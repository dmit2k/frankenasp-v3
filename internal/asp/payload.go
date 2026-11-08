package asp

import (
	"bytes"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/ulikunitz/xz"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	payloadBlockSize   = 4096
	payloadMaxManifest = 8 << 20
	payloadMaxOpData   = 32 << 20
	payloadMaxOpOutput = 32 << 20
	payloadCacheLimit  = 48 << 20
)

type payloadExtent struct{ start, count uint64 }
type payloadOperation struct {
	kind, dataOffset, dataLength uint64
	extents                      []payloadExtent
	rawHash                      []byte
}
type payloadPartition struct {
	name       string
	size       uint64
	hash       []byte
	old        bool
	operations []payloadOperation
}
type payloadSegment struct {
	start, end, offset uint64
	operation          int
}
type payloadImage struct {
	source                io.ReaderAt
	dataStart, dataLimit  uint64
	size                  uint64
	operations            []payloadOperation
	segments              []payloadSegment
	mu                    sync.Mutex
	decoded               map[int][]byte
	lru                   []int
	cacheBytes            int
	partitionHash         []byte
	needsFullVerification bool
	verified              bool
	integrityFailure      error
}

// Protobuf wire reader: only the fields relevant to source-independent
// partition extraction are interpreted; unknown protobuf fields are skipped.
func payloadWire(raw []byte, visit func(int, uint64, []byte) error) error {
	for len(raw) > 0 {
		number, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return errors.New("invalid payload protobuf tag")
		}
		raw = raw[n:]
		var value uint64
		var bytesValue []byte
		switch typ {
		case protowire.VarintType:
			var k int
			value, k = protowire.ConsumeVarint(raw)
			if k < 0 {
				return errors.New("invalid payload protobuf varint")
			}
			raw = raw[k:]
		case protowire.BytesType:
			var k int
			bytesValue, k = protowire.ConsumeBytes(raw)
			if k < 0 {
				return errors.New("invalid payload protobuf length")
			}
			raw = raw[k:]
		default:
			k := protowire.ConsumeFieldValue(number, typ, raw)
			if k < 0 {
				return errors.New("invalid payload protobuf field")
			}
			raw = raw[k:]
			continue
		}
		if err := visit(int(number), value, bytesValue); err != nil {
			return err
		}
	}
	return nil
}
func payloadParseExtent(raw []byte) (payloadExtent, error) {
	var e payloadExtent
	err := payloadWire(raw, func(n int, v uint64, _ []byte) error {
		switch n {
		case 1:
			e.start = v
		case 2:
			e.count = v
		}
		return nil
	})
	if err != nil {
		return e, err
	}
	if e.count == 0 || e.start > 1<<25 || e.count > 1<<25-e.start {
		return e, errors.New("invalid payload extent")
	}
	return e, nil
}
func payloadParseOperation(raw []byte) (payloadOperation, error) {
	var op payloadOperation
	var kindSeen bool
	err := payloadWire(raw, func(n int, v uint64, b []byte) error {
		switch n {
		case 1:
			op.kind = v
			kindSeen = true
		case 2:
			op.dataOffset = v
		case 3:
			op.dataLength = v
		case 4:
			return errors.New("incremental payload operation uses source extents")
		case 6:
			e, err := payloadParseExtent(b)
			if err != nil {
				return err
			}
			op.extents = append(op.extents, e)
		case 8:
			op.rawHash = append([]byte(nil), b...)
		}
		return nil
	})
	if err != nil {
		return op, err
	}
	if !kindSeen || (op.kind != 0 && op.kind != 1 && op.kind != 8 && op.kind != 6 && op.kind != 7) {
		return op, fmt.Errorf("unsupported incremental/unknown payload operation %d", op.kind)
	}
	if len(op.extents) == 0 || len(op.extents) > 4096 {
		return op, errors.New("invalid payload extent count")
	}
	var blocks uint64
	for _, e := range op.extents {
		blocks += e.count
		if blocks > payloadMaxOpOutput/payloadBlockSize {
			return op, errors.New("payload operation exceeds decoded limit")
		}
	}
	if op.kind == 6 || op.kind == 7 {
		if op.dataLength != 0 {
			return op, errors.New("zero/discard payload operation carries data")
		}
	} else if op.dataLength == 0 || op.dataLength > payloadMaxOpData {
		return op, errors.New("payload compressed operation exceeds bounds")
	}
	if len(op.rawHash) != 0 && len(op.rawHash) != sha256.Size {
		return op, errors.New("bad payload operation SHA-256")
	}
	return op, nil
}
func payloadParsePartition(raw []byte) (payloadPartition, error) {
	var p payloadPartition
	err := payloadWire(raw, func(n int, v uint64, b []byte) error {
		switch n {
		case 1:
			p.name = string(b)
		case 6:
			p.old = true
		case 7:
			return payloadWire(b, func(n int, v uint64, b []byte) error {
				switch n {
				case 1:
					p.size = v
				case 2:
					p.hash = append([]byte(nil), b...)
				}
				return nil
			})
		case 8:
			if len(p.operations) >= 32768 {
				return errors.New("payload operation count exceeded")
			}
			op, err := payloadParseOperation(b)
			if err != nil {
				return err
			}
			p.operations = append(p.operations, op)
		}
		return nil
	})
	if err != nil {
		return p, err
	}
	if !safePath(p.name) || bytes.ContainsRune([]byte(p.name), '/') || p.size == 0 || p.size > 4<<30 ||
		p.size%payloadBlockSize != 0 || len(p.operations) == 0 || p.old {
		return p, errors.New("invalid/incremental payload partition")
	}
	if len(p.hash) != 0 && len(p.hash) != sha256.Size {
		return p, errors.New("invalid payload partition SHA-256")
	}
	return p, nil
}
func payloadParseManifest(raw []byte) (map[string]payloadPartition, error) {
	var blockSize uint64 = payloadBlockSize
	var minor uint64
	parts := map[string]payloadPartition{}
	err := payloadWire(raw, func(n int, v uint64, b []byte) error {
		switch n {
		case 3:
			blockSize = v
		case 12:
			minor = v
		case 13:
			p, err := payloadParsePartition(b)
			if err != nil {
				return err
			}
			if _, ok := parts[p.name]; ok {
				return errors.New("duplicate payload partition")
			}
			if len(parts) >= 64 {
				return errors.New("too many payload partitions")
			}
			parts[p.name] = p
		case 16:
			if v != 0 {
				return errors.New("partial payload cannot reconstruct full image")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if blockSize != payloadBlockSize || minor != 0 {
		return nil, errors.New("unsupported block size or incremental payload")
	}
	if _, ok := parts["system"]; !ok {
		return nil, errors.New("payload has no system partition")
	}
	return parts, nil
}
func newPayloadImage(source io.ReaderAt, dataStart, dataLimit uint64, p payloadPartition) (*payloadImage, error) {
	reader := &payloadImage{source: source, dataStart: dataStart, dataLimit: dataLimit, size: p.size, operations: p.operations, decoded: map[int][]byte{}, partitionHash: append([]byte(nil), p.hash...)}
	for i, op := range p.operations {
		if len(op.rawHash) == 0 && op.kind != 6 && op.kind != 7 {
			reader.needsFullVerification = true
		}
		if op.kind != 6 && op.kind != 7 && (op.dataOffset > dataLimit || op.dataLength > dataLimit-op.dataOffset) {
			return nil, errors.New("payload operation data outside container")
		}
		var cursor uint64
		for _, e := range op.extents {
			start := e.start * payloadBlockSize
			end := start + e.count*payloadBlockSize
			if end > p.size || end <= start {
				return nil, errors.New("payload operation outside partition")
			}
			reader.segments = append(reader.segments, payloadSegment{start: start, end: end, offset: cursor, operation: i})
			cursor += end - start
		}
	}
	sort.Slice(reader.segments, func(i, j int) bool { return reader.segments[i].start < reader.segments[j].start })
	var end uint64
	for _, s := range reader.segments {
		if s.start != end {
			return nil, fmt.Errorf("payload full partition coverage gap/overlap at %d", end)
		}
		end = s.end
	}
	if end != p.size {
		return nil, errors.New("incomplete payload full partition")
	}
	if reader.needsFullVerification && len(reader.partitionHash) != sha256.Size {
		return nil, errors.New("missing partition digest for payload without operation hashes")
	}
	return reader, nil
}
func (p *payloadImage) decode(index int) ([]byte, error) {
	if cached, ok := p.decoded[index]; ok {
		for j, id := range p.lru {
			if id == index {
				p.lru = append(p.lru[:j], p.lru[j+1:]...)
				break
			}
		}
		p.lru = append(p.lru, index)
		return cached, nil
	}
	op := p.operations[index]
	var size uint64
	for _, e := range op.extents {
		size += e.count * payloadBlockSize
	}
	if size == 0 || size > payloadMaxOpOutput {
		return nil, errors.New("invalid decoded payload size")
	}
	var data []byte
	if op.kind == 6 || op.kind == 7 {
		data = make([]byte, int(size))
	} else {
		raw := make([]byte, int(op.dataLength))
		n, err := p.source.ReadAt(raw, int64(p.dataStart+op.dataOffset))
		if err != nil || n != len(raw) {
			return nil, fmt.Errorf("read payload operation: %w", err)
		}
		if len(op.rawHash) != 0 && !payloadHashEqual(op.rawHash, raw) {
			return nil, errors.New("payload operation SHA-256 mismatch")
		}
		var stream io.Reader = bytes.NewReader(raw)
		switch op.kind {
		case 1:
			stream = bzip2.NewReader(stream)
		case 8:
			xzr, err := xz.NewReader(stream)
			if err != nil {
				return nil, fmt.Errorf("XZ payload operation: %w", err)
			}
			stream = xzr
		}
		out, err := io.ReadAll(io.LimitReader(stream, int64(size)+1))
		if err != nil {
			return nil, fmt.Errorf("decode payload operation: %w", err)
		}
		if uint64(len(out)) != size {
			return nil, fmt.Errorf("payload decoded %d bytes expected %d", len(out), size)
		}
		data = out
	}
	for len(p.lru) > 0 && p.cacheBytes+len(data) > payloadCacheLimit {
		victim := p.lru[0]
		p.lru = p.lru[1:]
		p.cacheBytes -= len(p.decoded[victim])
		delete(p.decoded, victim)
	}
	p.decoded[index] = data
	p.cacheBytes += len(data)
	p.lru = append(p.lru, index)
	return data, nil
}

// verifyWholePartition is required only when operation hashes are absent.
// Hash logical extents in partition order without materializing an image.
// Caller holds p.mu or calls before exposing the image to another reader.
func (p *payloadImage) verifyWholePartition() error {
	if p.verified || !p.needsFullVerification {
		return nil
	}
	if len(p.partitionHash) != sha256.Size {
		return errors.New("payload partition digest unavailable")
	}
	if p.integrityFailure != nil {
		return p.integrityFailure
	}
	h := sha256.New()
	for _, seg := range p.segments {
		decoded, err := p.decode(seg.operation)
		if err != nil {
			return err
		}
		length := seg.end - seg.start
		if seg.offset > uint64(len(decoded)) || length > uint64(len(decoded))-seg.offset {
			return errors.New("invalid payload decoded extent")
		}
		_, _ = h.Write(decoded[seg.offset : seg.offset+length])
	}
	if !bytes.Equal(h.Sum(nil), p.partitionHash) {
		p.integrityFailure = errors.New("payload whole-partition SHA-256 mismatch")
		return p.integrityFailure
	}
	p.verified = true
	return nil
}

func (p *payloadImage) ReadAt(dst []byte, at int64) (int, error) {
	if at < 0 || uint64(at) > p.size {
		return 0, io.EOF
	}
	if len(dst) == 0 {
		return 0, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.integrityFailure != nil {
		return 0, p.integrityFailure
	}
	if err := p.verifyWholePartition(); err != nil {
		return 0, err
	}
	copied := 0
	for copied < len(dst) && uint64(at) < p.size {
		pos := uint64(at)
		j := sort.Search(len(p.segments), func(i int) bool { return p.segments[i].end > pos })
		if j >= len(p.segments) || p.segments[j].start > pos {
			return copied, errors.New("payload image segment gap")
		}
		s := p.segments[j]
		data, err := p.decode(s.operation)
		if err != nil {
			return copied, err
		}
		rel := s.offset + pos - s.start
		count := min(uint64(len(dst)-copied), s.end-pos)
		copy(dst[copied:copied+int(count)], data[rel:rel+count])
		copied += int(count)
		at += int64(count)
	}
	if copied != len(dst) {
		return copied, io.EOF
	}
	return copied, nil
}
func openPayloadSource(f *os.File, containerStart, containerSize uint64, requested, meta, format string) (*otaSources, error) {
	if containerSize < 24 {
		return nil, errors.New("short payload")
	}
	hdr := make([]byte, 24)
	if _, err := f.ReadAt(hdr, int64(containerStart)); err != nil {
		return nil, err
	}
	if string(hdr[:4]) != "CrAU" {
		return nil, errors.New("invalid payload magic")
	}
	version := binary.BigEndian.Uint64(hdr[4:12])
	if version != 2 {
		return nil, errors.New("unsupported CrAU major version (only major 2 manifest is implemented)")
	}
	size := binary.BigEndian.Uint64(hdr[12:20])
	tail := uint64(20)
	var signature uint64
	if version == 2 {
		tail = 24
		signature = uint64(binary.BigEndian.Uint32(hdr[20:24]))
	}
	if size == 0 || size > payloadMaxManifest || tail+size+signature > containerSize {
		return nil, errors.New("invalid payload manifest bounds")
	}
	manifest := make([]byte, int(size))
	if _, err := f.ReadAt(manifest, int64(containerStart+tail)); err != nil {
		return nil, err
	}
	partitions, err := payloadParseManifest(manifest)
	if err != nil {
		return nil, err
	}
	platform, err := sourceCodename(metadataValue([]byte(meta), "post-device"), metadataValue([]byte(meta), "post-build"))
	if err != nil {
		return nil, err
	}
	platform, err = resolveCodename(requested, platform)
	if err != nil {
		return nil, err
	}
	dataStart := containerStart + tail + size + signature
	dataLimit := containerSize - (tail + size + signature)
	systemReader, err := newPayloadImage(f, dataStart, dataLimit, partitions["system"])
	if err != nil {
		return nil, err
	}
	out := &otaSources{kind: format, platform: platform, firmware: firmwareLabel(metadataValue([]byte(meta), "post-build"), metadataValue([]byte(meta), "post-build-incremental")), system: &image{src: systemReader, size: int64(systemReader.size)}}
	if partition, ok := partitions["vendor"]; ok {
		vendorReader, err := newPayloadImage(f, dataStart, dataLimit, partition)
		if err != nil {
			return nil, err
		}
		out.vendor = &image{src: vendorReader, size: int64(vendorReader.size)}
	}
	// Validate full partition digest only for hashless operations; do not
	// publish an unchecked image if discovery happens to skip its corrupt area.
	if err := systemReader.verifyWholePartition(); err != nil {
		return nil, err
	}
	if v, ok := out.vendor.(*image); ok {
		if rd, ok := v.src.(*payloadImage); ok {
			if err := rd.verifyWholePartition(); err != nil {
				return nil, err
			}
		}
	}
	if err := configureSystemRoot(out); err != nil {
		return nil, err
	}
	if out.vendor == nil {
		out.vendor = prefixed{parent: out.system, prefix: "vendor"}
	}
	return out, nil
}

func payloadHashEqual(want, raw []byte) bool {
	sum := sha256.Sum256(raw)
	return bytes.Equal(want, sum[:])
}
