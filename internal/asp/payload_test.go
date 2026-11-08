package asp

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/ulikunitz/xz"
	"google.golang.org/protobuf/encoding/protowire"
)

func pbVar(n protowire.Number, v uint64) []byte {
	b := protowire.AppendTag(nil, n, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}
func pbBytes(n protowire.Number, v []byte) []byte {
	b := protowire.AppendTag(nil, n, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}
func payloadTestExtent(block, count uint64) []byte {
	e := pbVar(1, block)
	return append(e, pbVar(2, count)...)
}
func payloadTestOp(t uint64, offset uint64, compressed []byte, start uint64) []byte {
	op := pbVar(1, t)
	op = append(op, pbVar(2, offset)...)
	op = append(op, pbVar(3, uint64(len(compressed)))...)
	op = append(op, pbBytes(6, payloadTestExtent(start, 1))...)
	h := sha256.Sum256(compressed)
	return append(op, pbBytes(8, h[:])...)
}
func TestFullCrAUPayloadReader(t *testing.T) {
	first := bytes.Repeat([]byte{0x56}, payloadBlockSize)
	second := bytes.Repeat([]byte{0x99}, payloadBlockSize)
	var compressed bytes.Buffer
	writer, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(second); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := append(append([]byte(nil), first...), compressed.Bytes()...)
	info := pbVar(1, 2*payloadBlockSize)
	fullHash := sha256.Sum256(append(append([]byte(nil), first...), second...))
	info = append(info, pbBytes(2, fullHash[:])...)
	part := pbBytes(1, []byte("system"))
	part = append(part, pbBytes(7, info)...)
	part = append(part, pbBytes(8, payloadTestOp(0, 0, first, 0))...)
	part = append(part, pbBytes(8, payloadTestOp(8, uint64(len(first)), compressed.Bytes(), 1))...)
	manifest := append(pbVar(3, payloadBlockSize), pbBytes(13, part)...)
	partitions, err := payloadParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	src, err := newPayloadImage(bytes.NewReader(raw), 0, uint64(len(raw)), partitions["system"])
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 2*payloadBlockSize)
	if n, err := src.ReadAt(got, 0); err != nil || n != len(got) {
		t.Fatalf("ReadAt %d %v", n, err)
	}
	if !bytes.Equal(got[:payloadBlockSize], first) || !bytes.Equal(got[payloadBlockSize:], second) {
		t.Fatal("decoded payload differs")
	}
	if n, err := src.ReadAt(got[1:1+payloadBlockSize], payloadBlockSize-1); err != nil || n != payloadBlockSize {
		t.Fatalf("cross-boundary read %d %v", n, err)
	}
	if got[1] != first[len(first)-1] || got[payloadBlockSize] != second[0] {
		t.Fatal("cross-boundary bytes differ")
	}
	if n, err := src.ReadAt(got, 2*payloadBlockSize); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("EOF %d %v", n, err)
	}
	// A wrong SHA-256 must be rejected even if the compressed XZ is otherwise valid.
	broken := partitions["system"]
	broken.operations[0].rawHash = bytes.Repeat([]byte{0xff}, 32)
	bad, err := newPayloadImage(bytes.NewReader(raw), 0, uint64(len(raw)), broken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.ReadAt(make([]byte, 1), 0); err == nil || !bytes.Contains([]byte(err.Error()), []byte("SHA-256")) {
		t.Fatalf("hash mismatch: %v", err)
	}
}
func TestPayloadRejectsIncompleteOrIncremental(t *testing.T) {
	data := bytes.Repeat([]byte{0x0e}, payloadBlockSize)
	opRaw := payloadTestOp(0, 0, data, 1)
	p := payloadPartition{name: "system", size: 2 * payloadBlockSize, operations: []payloadOperation{}}
	op, err := payloadParseOperation(opRaw)
	if err != nil {
		t.Fatal(err)
	}
	p.operations = []payloadOperation{op}
	if _, err := newPayloadImage(bytes.NewReader(data), 0, uint64(len(data)), p); err == nil {
		t.Fatal("accepted partial extent coverage")
	}
	invalidOp := pbVar(1, 4) // SOURCE_COPY requires the previous partition.
	invalidOp = append(invalidOp, pbBytes(6, payloadTestExtent(0, 1))...)
	if _, err := payloadParseOperation(invalidOp); err == nil {
		t.Fatal("accepted incremental op")
	}
	part := pbBytes(1, []byte("system"))
	part = append(part, pbVar(6, 1)...)
	part = append(part, pbBytes(7, pbVar(1, payloadBlockSize))...)
	part = append(part, pbBytes(8, payloadTestOp(0, 0, data, 0))...)
	badManifest := append(pbVar(3, payloadBlockSize), pbBytes(13, part)...)
	if _, err := payloadParseManifest(badManifest); err == nil {
		t.Fatal("accepted an incremental partition")
	}
	partial := append(pbVar(16, 1), badManifest...)
	if _, err := payloadParseManifest(partial); err == nil {
		t.Fatal("accepted partial update")
	}
}

func TestPuffinOTAMetadataExactSelection(t *testing.T) {
	raw, err := sourceCodename("donut_puffin", "Amazon/donut_puffin/donut_puffin:6.0/NS6575/8349N:user")
	if err != nil || raw != "donut_puffin" {
		t.Fatalf("raw source %q %v", raw, err)
	}
	for _, requested := range []string{"", "donut_puffin"} {
		got, err := resolveCodename(requested, raw)
		if err != nil || got != "donut_puffin" {
			t.Fatalf("canonical %q => %q %v", requested, got, err)
		}
	}
	if _, err := resolveCodename("donut", raw); err == nil {
		t.Fatal("short packaging identity must not replace OTA device identity")
	}
	if _, err := resolveCodename("rook", raw); err == nil || !bytes.Contains([]byte(err.Error()), []byte("donut_puffin")) {
		t.Fatalf("identity mismatch: %v", err)
	}
	if got := firmwareLabel("Amazon/donut_puffin/donut_puffin:6.0/NS6575/8349N:user", "0013323246980"); got != "13323246980" {
		t.Fatalf("numeric build %q", got)
	}
	if got := firmwareLabel("", "0036274153604"); got != "build-0036274153604" {
		t.Fatalf("legacy build %q", got)
	}
}

func TestLiteralUnknownPuffinIdentity(t *testing.T) {
	got, err := resolveCodename("9_review_puffin", "9_review_puffin")
	if err != nil || got != "9_review_puffin" {
		t.Fatalf("exact identity renamed: %q %v", got, err)
	}
	got, err = resolveCodename("donut_puffin", "donut_puffin")
	if err != nil || got != "donut_puffin" {
		t.Fatalf("literal identity renamed: %q %v", got, err)
	}
}
