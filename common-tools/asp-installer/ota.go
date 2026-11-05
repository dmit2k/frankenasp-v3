package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/andybalholm/brotli"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const blockSize = 4096
const maxCache = 96 << 20
const maxPasses = 64

type blockRange struct{ lo, hi uint64 }
type transferPlan struct {
	version           int
	blocks, newBlocks uint64
	newRanges         []blockRange
	sortedNew         []blockRange
}

func ranges(raw string, total uint64) ([]blockRange, error) {
	fields := strings.Split(raw, ",")
	if len(fields) < 3 {
		return nil, errors.New("short transfer ranges")
	}
	n, e := strconv.Atoi(fields[0])
	if e != nil || n != len(fields)-1 || n%2 != 0 || n > 131072 {
		return nil, errors.New("malformed transfer range count")
	}
	ret := make([]blockRange, 0, n/2)
	var end uint64
	for j := 1; j < len(fields); j += 2 {
		a, e := strconv.ParseUint(fields[j], 10, 64)
		if e != nil {
			return nil, e
		}
		b, e := strconv.ParseUint(fields[j+1], 10, 64)
		if e != nil {
			return nil, e
		}
		if a >= b || b > total || (j > 1 && a < end) {
			return nil, errors.New("overlapping/invalid transfer ranges")
		}
		ret = append(ret, blockRange{a, b})
		end = b
	}
	return ret, nil
}
func parseTransfer(data []byte) (transferPlan, error) {
	if len(data) > 1<<20 {
		return transferPlan{}, errors.New("oversized transfer list")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 5 {
		return transferPlan{}, errors.New("short transfer list")
	}
	version, e := strconv.Atoi(strings.TrimSpace(lines[0]))
	if e != nil || version != 3 && version != 4 {
		return transferPlan{}, errors.New("only full block v3/v4 OTA supported")
	}
	total, e := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 64)
	if e != nil || total == 0 || total > 1<<20 {
		return transferPlan{}, errors.New("invalid transfer block count")
	}
	stashEntries, e := strconv.Atoi(strings.TrimSpace(lines[2]))
	if e != nil {
		return transferPlan{}, e
	}
	stashBlocks, e := strconv.Atoi(strings.TrimSpace(lines[3]))
	if e != nil || stashBlocks != 0 || stashEntries != 0 {
		return transferPlan{}, errors.New("incremental/stash OTA unsupported")
	}
	p := transferPlan{version: version}
	var maxEnd uint64
	var seen []blockRange
	for _, line := range lines[4:] {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return p, errors.New("unsupported transfer command")
		}
		kind := parts[0]
		if kind != "new" && kind != "zero" && kind != "erase" {
			return p, fmt.Errorf("OTA transfer command %q needs base and is unsupported", kind)
		}
		rr, e := ranges(parts[1], 1<<20)
		if e != nil {
			return p, e
		}
		for _, r := range rr {
			if r.hi > maxEnd {
				maxEnd = r.hi
			}
			if kind == "new" {
				p.newBlocks += r.hi - r.lo
				p.newRanges = append(p.newRanges, r)
			}
			if kind != "erase" {
				seen = append(seen, r)
			}
		}
	}
	if p.newBlocks == 0 || p.newBlocks > total {
		return p, errors.New("new transfer block count mismatch")
	}
	sort.Slice(seen, func(i, j int) bool { return seen[i].lo < seen[j].lo })
	for i := 1; i < len(seen); i++ {
		if seen[i].lo < seen[i-1].hi {
			return p, errors.New("overlapping new/zero transfer ranges")
		}
	}
	var count uint64
	for _, r := range seen {
		count += r.hi - r.lo
	}
	if version == 4 && count != total {
		return p, errors.New("v4 new/zero block count mismatch")
	}
	if maxEnd == 0 || maxEnd > 1<<20 {
		return p, errors.New("image >4GiB or empty")
	}
	p.blocks = maxEnd
	p.sortedNew = append([]blockRange(nil), p.newRanges...)
	sort.Slice(p.sortedNew, func(i, j int) bool { return p.sortedNew[i].lo < p.sortedNew[j].lo })
	for i := 1; i < len(p.sortedNew); i++ {
		if p.sortedNew[i].lo < p.sortedNew[i-1].hi {
			return p, errors.New("overlapping new ranges")
		}
	}
	return p, nil
}
func (p transferPlan) hasNew(id uint64) bool {
	j := sort.Search(len(p.sortedNew), func(i int) bool { return p.sortedNew[i].hi > id })
	return j < len(p.sortedNew) && p.sortedNew[j].lo <= id
}

type selective struct {
	ctx              context.Context
	file             *zip.File
	plan             transferPlan
	size             int64
	cache            map[uint64][]byte
	pending          map[uint64]bool
	passes           int
	partition        string
	meter            *progressMeter
	compressedBrotli bool
}

func (s *selective) ReadAt(dst []byte, off int64) (int, error) {
	if off < 0 || int64(len(dst)) > 64<<20 || off > s.size || int64(len(dst)) > s.size-off {
		return 0, io.ErrUnexpectedEOF
	}
	missing := false
	start, end := uint64(off)/blockSize, (uint64(off)+uint64(len(dst))+blockSize-1)/blockSize
	for id := start; id < end; id++ {
		if !s.plan.hasNew(id) {
			continue
		}
		if _, ok := s.cache[id]; !ok {
			s.pending[id] = true
			missing = true
		}
	}
	if missing {
		return 0, errBlocks
	}
	for i := range dst {
		id := uint64(off+int64(i)) / blockSize
		if b, ok := s.cache[id]; ok {
			dst[i] = b[(uint64(off)+uint64(i))%blockSize]
		} else {
			dst[i] = 0
		}
	}
	return len(dst), nil
}

type streamSelect struct {
	s          *selective
	rangeIndex int
	inRange    uint64
	total      uint64
}

func (w *streamSelect) Write(p []byte) (int, error) {
	if w.s.ctx != nil && w.s.ctx.Err() != nil {
		return 0, w.s.ctx.Err()
	}
	original := len(p)
	if w.total+uint64(len(p)) > w.s.plan.newBlocks*blockSize {
		return 0, errors.New("excess transfer data")
	}
	for len(p) > 0 {
		if w.rangeIndex >= len(w.s.plan.newRanges) {
			return 0, errors.New("transfer stream overrun")
		}
		r := w.s.plan.newRanges[w.rangeIndex]
		avail := (r.hi-r.lo)*blockSize - w.inRange
		take := min(uint64(len(p)), avail)
		piece := p[:int(take)]
		position := r.lo*blockSize + w.inRange
		for len(piece) > 0 {
			id := position / blockSize
			at := position % blockSize
			chunk := min(uint64(len(piece)), blockSize-at)
			if w.s.pending[id] || (w.s.passes == 0 && id < 2048) {
				b, ok := w.s.cache[id]
				if !ok {
					b = make([]byte, blockSize)
					w.s.cache[id] = b
				}
				copy(b[at:at+chunk], piece[:int(chunk)])
			}
			piece = piece[int(chunk):]
			position += chunk
		}
		p = p[int(take):]
		w.inRange += take
		w.total += take
		if w.inRange == (r.hi-r.lo)*blockSize {
			w.rangeIndex++
			w.inRange = 0
		}
	}
	return original, nil
}
func (s *selective) fill() error {
	if len(s.pending) == 0 {
		return nil
	}
	if s.passes >= maxPasses {
		return errors.New("selective extraction exceeded 64 passes")
	}
	if (len(s.cache)+len(s.pending))*blockSize > maxCache {
		return errors.New("selective block cache exceeded 96MiB")
	}
	r, e := s.file.Open()
	if e != nil {
		return e
	}
	defer r.Close()
	var reader io.Reader = r
	if s.compressedBrotli {
		reader = brotli.NewReader(r)
	}
	beforeBlocks := len(s.cache)
	w := &streamSelect{s: s}
	start := time.Now()
	buf := make([]byte, 256<<10)
	n, e := io.CopyBuffer(w, reader, buf)
	if e != nil {
		return fmt.Errorf("OTA decode: %w", e)
	}
	if n != int64(s.plan.newBlocks*blockSize) || w.rangeIndex != len(s.plan.newRanges) {
		return fmt.Errorf("truncated OTA transfer stream (%d bytes)", n)
	}
	s.passes++
	for id := range s.pending {
		delete(s.pending, id)
	}
	if s.meter != nil {
		overall := s.meter.NextScan()
		s.meter.Note(fmt.Sprintf("Scan %d: %s partition, pass %d — +%d blocks, %d retained (%.1f MiB), %s", overall, s.partition, s.passes, len(s.cache)-beforeBlocks, len(s.cache), float64(len(s.cache)*blockSize)/(1<<20), time.Since(start).Round(time.Second)))
	}
	return nil
}

type otaSources struct {
	system, vendor files
	selective      []*selective
	kind           string
	platform       string
	firmware       string
}
type prefixed struct {
	parent files
	prefix string
}

func (p prefixed) Read(s string) ([]byte, error) { return p.parent.Read(p.prefix + "/" + s) }
func (p prefixed) Exists(s string) (bool, error) { return p.parent.Exists(p.prefix + "/" + s) }
func (p prefixed) Files(s string) ([]string, error) {
	f, e := p.parent.Files(p.prefix + "/" + s)
	if e != nil {
		return nil, e
	}
	for i := range f {
		f[i] = strings.TrimPrefix(f[i], p.prefix+"/")
	}
	return f, nil
}

// Decide whether the system partition is mounted at / or /system using the
// actual filesystem. Both conventions exist in otherwise compatible ext4 OTAs.
func configureSystemRoot(src *otaSources) error {
	img, ok := src.system.(*image)
	if !ok {
		return nil
	}
	var root, nested bool
	err := src.resolve(func() error {
		var e error
		nested, e = img.Exists("system/bin/linker")
		if e != nil {
			return e
		}
		if nested {
			return nil
		} // /bin can be a symlink on nested-root images.
		root, e = img.Exists("bin/linker")
		if e != nil {
			return e
		}
		if root {
			return nil
		}
		nested, e = img.Exists("system/build.prop")
		if e != nil {
			return e
		}
		if nested {
			return nil
		}
		root, e = img.Exists("build.prop")
		return e
	})
	if err != nil {
		return err
	}
	if nested {
		img.prefix = "system"
	}
	if !root && !nested {
		return errors.New("system filesystem layout not recognized")
	}
	return nil
}

func openOTA(path, platform string) (*otaSources, *os.File, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, e
	}
	closeOnErr := func(err error) (*otaSources, *os.File, error) { _ = f.Close(); return nil, nil, err }
	st, e := f.Stat()
	if e != nil {
		return closeOnErr(e)
	}
	if st.Size() <= 0 || st.Size() > 2<<30 {
		return closeOnErr(errors.New("OTA size outside 2GiB limit"))
	}
	z, e := zip.NewReader(f, st.Size())
	if e != nil {
		return closeOnErr(e)
	}
	if len(z.File) > 100000 {
		return closeOnErr(errors.New("too many OTA ZIP entries"))
	}
	var logicalBytes uint64
	members := map[string]*zip.File{}
	for _, member := range z.File {
		logicalBytes += member.UncompressedSize64
		if logicalBytes > 8<<30 {
			return closeOnErr(errors.New("OTA ZIP uncompressed inventory exceeds 8GiB"))
		}
		name := strings.TrimSuffix(member.Name, "/")
		if !safePath(name) {
			return closeOnErr(errors.New("unsafe OTA ZIP path"))
		}
		if _, found := members[member.Name]; found {
			return closeOnErr(errors.New("duplicate OTA ZIP member"))
		}
		if member.Flags&1 != 0 || member.Mode()&os.ModeSymlink != 0 {
			return closeOnErr(errors.New("unsupported OTA encrypted/symlink entry"))
		}
		members[member.Name] = member
	}
	var mb []byte
	if meta := members["META-INF/com/android/metadata"]; meta != nil {
		mb, e = readZip(meta, 65536)
		if e != nil {
			return closeOnErr(e)
		}
	}
	if bytes.Contains(mb, []byte("pre-build=")) || members["payload.bin"] != nil {
		return closeOnErr(errors.New("incremental/payload OTA unsupported"))
	}
	detected, e := sourceCodename(metadataValue(mb, "post-device"), metadataValue(mb, "post-build"))
	if e != nil {
		return closeOnErr(e)
	}
	platform, e = resolveCodename(platform, detected)
	if e != nil {
		return closeOnErr(e)
	}
	out := &otaSources{platform: platform, firmware: firmwareLabel(metadataValue(mb, "post-build"), metadataValue(mb, "post-build-incremental"))}
	if members["system.transfer.list"] == nil {
		out.kind = "ZIP_FILE_TREE"
		out.system = newZipTree(z, "system")
		separateVendor := false
		for name := range members {
			if strings.HasPrefix(name, "vendor/") {
				separateVendor = true
				break
			}
		}
		if separateVendor {
			out.vendor = newZipTree(z, "vendor")
		} else {
			out.vendor = prefixed{parent: out.system, prefix: "vendor"}
		}
		return out, f, nil
	}
	parts := []string{"system"}
	if members["vendor.transfer.list"] != nil {
		parts = append(parts, "vendor")
	}
	for _, part := range parts {
		raw, err := readZip(members[part+".transfer.list"], 1<<20)
		if err != nil {
			return closeOnErr(err)
		}
		plan, err := parseTransfer(raw)
		if err != nil {
			return closeOnErr(fmt.Errorf("%s: %w", part, err))
		}
		if pd := members[part+".patch.dat"]; pd != nil && pd.UncompressedSize64 != 0 {
			return closeOnErr(errors.New("incremental nonempty patch.dat"))
		}
		member := members[part+".new.dat.br"]
		isBr := true
		if member == nil {
			member = members[part+".new.dat"]
			isBr = false
		}
		if member == nil {
			return closeOnErr(fmt.Errorf("missing %s new.dat", part))
		}
		reader := &selective{file: member, partition: part, plan: plan, size: int64(plan.blocks * blockSize),
			cache: map[uint64][]byte{}, pending: map[uint64]bool{}, compressedBrotli: isBr}
		out.selective = append(out.selective, reader)
		img := &image{src: reader, size: reader.size}
		if part == "system" {
			out.system = img
		} else {
			out.vendor = img
		}
	}
	if e := configureSystemRoot(out); e != nil {
		return closeOnErr(e)
	}
	if out.vendor == nil {
		out.vendor = prefixed{parent: out.system, prefix: "vendor"}
	}
	out.kind = fmt.Sprintf("ZIP_FULL_BLOCK_v%d", out.selective[0].plan.version)
	return out, f, nil
}
func readZip(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("ZIP member too large")
	}
	r, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit || uint64(len(b)) != f.UncompressedSize64 {
		return nil, errors.New("ZIP read length mismatch")
	}
	return b, nil
}
func (s *otaSources) resolve(action func() error) error {
	for i := 0; i < maxPasses; i++ {
		err := action()
		if err == nil {
			return nil
		}
		if !errors.Is(err, errBlocks) {
			return err
		}
		filled := false
		for _, src := range s.selective {
			if len(src.pending) > 0 {
				if e := src.fill(); e != nil {
					return e
				}
				filled = true
			}
		}
		if !filled {
			return errors.New("no requested blocks for incomplete source read")
		}
	}
	return errors.New("selective resolution exceeds 64 iterations")
}
func metadataValue(raw []byte, key string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}

var _ = bufio.NewReader
