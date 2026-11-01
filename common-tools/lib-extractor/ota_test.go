package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestTransferV3Rook(t *testing.T) {
	p, e := parseTransfer([]byte("3\n10\n0\n0\nerase 2,0,12\nnew 4,0,2,6,10\n"))
	if e != nil {
		t.Fatal(e)
	}
	if p.version != 3 || p.blocks != 12 || p.newBlocks != 6 {
		t.Fatalf("wrong v3 plan: %+v", p)
	}
	if p.hasNew(4) || !p.hasNew(1) || !p.hasNew(8) {
		t.Fatal("invalid v3 mapping")
	}
}
func TestTransferV4(t *testing.T) {
	p, e := parseTransfer([]byte("4\n12\n0\n0\nnew 4,0,3,5,8\nzero 4,3,5,8,12\n"))
	if e != nil {
		t.Fatal(e)
	}
	if p.newBlocks != 6 || p.blocks != 12 || !p.hasNew(6) || p.hasNew(4) {
		t.Fatal("invalid v4 mapping")
	}
}
func TestTransferRejectsDependentCommands(t *testing.T) {
	for _, bad := range []string{
		"4\n5\n0\n0\nmove 2,0,5\n",
		"3\n5\n1\n0\nnew 2,0,5\n",
		"4\n5\n0\n0\nnew 2,0,5\nzero 2,2,5\n",
		"4\n5\n0\n0\nnew 2,0,6\n",
	} {
		if _, e := parseTransfer([]byte(bad)); e == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
func TestSelectiveBlockCache(t *testing.T) {
	s := &selective{plan: transferPlan{newRanges: []blockRange{{1, 3}}, sortedNew: []blockRange{{1, 3}}, newBlocks: 2, blocks: 4}, size: 4 * blockSize, cache: map[uint64][]byte{}, pending: map[uint64]bool{}}
	b := make([]byte, blockSize)
	n, e := s.ReadAt(b, 0)
	if e != nil || n != blockSize || !bytes.Equal(b, make([]byte, blockSize)) {
		t.Fatalf("zero block: %d %v", n, e)
	}
	if _, e = s.ReadAt(b, blockSize); !errors.Is(e, errBlocks) {
		t.Fatalf("missing block: %v", e)
	}
	w := &streamSelect{s: s}
	data := append(bytes.Repeat([]byte{3}, blockSize), bytes.Repeat([]byte{4}, blockSize)...)
	if n, e := w.Write(data); e != nil || n != len(data) {
		t.Fatalf("stream %d %v", n, e)
	}
	s.pending = map[uint64]bool{}
	if _, e = s.ReadAt(b, blockSize); e != nil || !bytes.Equal(b, bytes.Repeat([]byte{3}, blockSize)) {
		t.Fatalf("retained block: %v", e)
	}
}
func TestRejectBadPath(t *testing.T) {
	for _, s := range []string{"/etc/passwd", "../libasp.so", "a//b", "a/./b", "a/../b", ""} {
		if safePath(s) {
			t.Fatal("unsafe path accepted:", s)
		}
	}
}
