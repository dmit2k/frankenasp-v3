package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errBlocks = errors.New("selective image blocks pending")
var errMissing = errors.New("source path missing")

type image struct {
	src                                                      io.ReaderAt
	size                                                     int64
	prefix                                                   string
	initialized                                              bool
	bs, blocks, first, bpg, ipg, inodes, inodeSize, descSize uint64
}

func le16(b []byte, o int) uint64 { return uint64(binary.LittleEndian.Uint16(b[o:])) }
func le32(b []byte, o int) uint64 { return uint64(binary.LittleEndian.Uint32(b[o:])) }
func invalid(ok bool, msg string) error {
	if !ok {
		return fmt.Errorf("invalid or unsupported source: %s", msg)
	}
	return nil
}
func (fs *image) read(off, n uint64) ([]byte, error) {
	if n > 64<<20 || off > uint64(fs.size) || n > uint64(fs.size)-off {
		return nil, fmt.Errorf("ext4 read out of bounds: %d+%d", off, n)
	}
	b := make([]byte, int(n))
	_, err := fs.src.ReadAt(b, int64(off))
	if err != nil {
		return nil, err
	}
	return b, nil
}
func (fs *image) init() error {
	if fs.initialized {
		return nil
	}
	s, e := fs.read(1024, 1024)
	if e != nil {
		return e
	}
	if e = invalid(le16(s, 56) == 0xef53, "ext4 superblock magic"); e != nil {
		return e
	}
	log := le32(s, 24)
	if e = invalid(log <= 6, "block size"); e != nil {
		return e
	}
	bs := uint64(1024) << log
	compat, incompat, ro := le32(s, 92), le32(s, 96), le32(s, 100)
	_ = compat
	if e = invalid(incompat & ^uint64(0x2c2) == 0 && ro & ^uint64(0x47f) == 0, "unsupported ext4 flags"); e != nil {
		return e
	}
	blocks := le32(s, 4)
	if incompat&0x80 != 0 {
		blocks |= le32(s, 336) << 32
	}
	first, bpg, ipg, inodes := le32(s, 20), le32(s, 32), le32(s, 40), le32(s, 0)
	isize, desc := uint64(128), uint64(32)
	if le32(s, 76) != 0 {
		isize = le16(s, 88)
	}
	if incompat&0x80 != 0 {
		desc = le16(s, 254)
	}
	groups := (blocks - first + bpg - 1) / max(1, bpg)
	if e = invalid(blocks > first && blocks*bs <= uint64(fs.size) && blocks*bs <= 4<<30 && first == map[bool]uint64{true: 1, false: 0}[bs == 1024] && bpg > 0 && bpg <= bs*8 && ipg > 0 && ipg <= bs*8 && isize >= 128 && isize <= bs && isize%4 == 0 && desc >= 32 && desc <= bs && desc%8 == 0 && groups > 0 && groups <= 131072 && inodes > 0 && inodes <= groups*ipg, "ext4 geometry"); e != nil {
		return e
	}
	fs.bs, fs.blocks, fs.first, fs.bpg, fs.ipg, fs.inodes, fs.inodeSize, fs.descSize = bs, blocks, first, bpg, ipg, inodes, isize, desc
	fs.initialized = true
	return nil
}
func (fs *image) block(n uint64) ([]byte, error) {
	if n == 0 || n >= fs.blocks {
		return nil, fmt.Errorf("invalid ext4 block %d", n)
	}
	return fs.read(n*fs.bs, fs.bs)
}

type inode struct {
	mode  uint64
	size  uint64
	flags uint64
	body  []byte
}

func (fs *image) inode(n uint64) (inode, error) {
	if e := fs.init(); e != nil {
		return inode{}, e
	}
	if n == 0 || n > fs.inodes {
		return inode{}, fmt.Errorf("invalid inode %d", n)
	}
	group, slot := (n-1)/fs.ipg, (n-1)%fs.ipg
	gd, e := fs.read((fs.first+1)*fs.bs+group*fs.descSize, fs.descSize)
	if e != nil {
		return inode{}, e
	}
	table := le32(gd, 8)
	if fs.descSize >= 64 {
		table |= le32(gd, 40) << 32
	}
	off := table*fs.bs + slot*fs.inodeSize
	if table == 0 || off+fs.inodeSize > fs.blocks*fs.bs {
		return inode{}, errors.New("invalid ext4 inode table")
	}
	raw, e := fs.read(off, fs.inodeSize)
	if e != nil {
		return inode{}, e
	}
	mode := le16(raw, 0)
	size := le32(raw, 4) | le32(raw, 108)<<32
	flags := le32(raw, 32)
	if le32(raw, 20) != 0 || le16(raw, 26) == 0 || flags&(0x4|0x800|0x1000|0x80000|0x100000|0x10000000) != flags&0x80000 {
		return inode{}, errors.New("unsupported/deleted ext4 inode")
	}
	return inode{mode, size, flags, raw[40:100]}, nil
}

type extent struct {
	logical, length, physical uint64
	unwritten                 bool
}

func (fs *image) extents(n inode) ([]extent, error) {
	if n.size > 64<<20 || n.flags&0x80000 == 0 {
		return nil, errors.New("unsupported ext4 inode extents/size")
	}
	out := []extent{}
	visited := map[uint64]bool{}
	var walk func([]byte, int) error
	walk = func(data []byte, expected int) error {
		if len(data) < 12 {
			return errors.New("bad extent header")
		}
		magic, cnt, cap, depth := le16(data, 0), le16(data, 2), le16(data, 4), int(le16(data, 6))
		if magic != 0xf30a || depth > 5 || depth != expected || cap == 0 || cap > uint64((len(data)-12)/12) || cnt > cap {
			return errors.New("invalid ext4 extent tree")
		}
		var last uint64
		for k := 0; k < int(cnt); k++ {
			at := 12 + k*12
			a, b, c := le32(data, at), le32(data, at+4), le32(data, at+8)
			if k > 0 && a <= last {
				return errors.New("unsorted ext4 extents")
			}
			last = a
			if depth > 0 {
				p := b | (c&0xffff)<<32
				if c>>16 != 0 || visited[p] || len(visited) >= 4096 {
					return errors.New("invalid extent index")
				}
				visited[p] = true
				child, e := fs.block(p)
				if e != nil {
					return e
				}
				if e = walk(child, depth-1); e != nil {
					return e
				}
			} else {
				length := b & 0xffff
				unwritten := length > 32768
				if unwritten {
					length -= 32768
				}
				p := c | (b>>16)<<32
				if length == 0 || p == 0 || p+length > fs.blocks {
					return errors.New("invalid extent address")
				}
				out = append(out, extent{a, length, p, unwritten})
			}
		}
		return nil
	}
	depth := int(le16(n.body, 6))
	if e := walk(n.body, depth); e != nil {
		return nil, e
	}
	var end uint64
	for _, x := range out {
		if x.logical < end || x.logical*fs.bs >= max(n.size, 1) {
			return nil, errors.New("overlapping extents")
		}
		end = x.logical + x.length
	}
	if len(out) > 16384 {
		return nil, errors.New("too many extents")
	}
	return out, nil
}
func (fs *image) data(n inode) ([]byte, error) {
	ext, e := fs.extents(n)
	if e != nil {
		return nil, e
	}
	out := make([]byte, int(n.size))
	pending := false
	for _, x := range ext {
		if x.unwritten {
			continue
		}
		start := x.logical * fs.bs
		count := min(x.length*fs.bs, n.size-start)
		data, e := fs.read(x.physical*fs.bs, count)
		if errors.Is(e, errBlocks) {
			pending = true
			continue
		}
		if e != nil {
			return nil, e
		}
		copy(out[int(start):], data)
	}
	if pending {
		return nil, errBlocks
	}
	return out, nil
}
func (fs *image) entries(n inode) (map[string]uint64, error) {
	if n.mode&0xf000 != 0x4000 {
		return nil, errors.New("not an ext4 directory")
	}
	data, e := fs.data(n)
	if e != nil {
		return nil, e
	}
	r := map[string]uint64{}
	for base := 0; base < len(data); base += int(fs.bs) {
		for at, end := base, min(base+int(fs.bs), len(data)); at < end; {
			if at+8 > end {
				return nil, errors.New("short directory entry")
			}
			ino, sz, nlen := le32(data, at), int(le16(data, at+4)), int(data[at+6])
			if sz < 8 || sz%4 != 0 || at+sz > end || nlen > sz-8 {
				return nil, errors.New("bad ext4 directory")
			}
			if ino != 0 {
				if nlen == 0 || nlen > 255 || ino > fs.inodes {
					return nil, errors.New("invalid directory inode")
				}
				name := string(data[at+8 : at+8+nlen])
				if strings.ContainsAny(name, "/\x00") {
					return nil, errors.New("invalid directory name")
				}
				if _, ok := r[name]; ok {
					return nil, errors.New("duplicate directory entry")
				}
				r[name] = ino
			}
			at += sz
		}
	}
	return r, nil
}
func (fs *image) lookup(path string) (inode, error) {
	if e := fs.init(); e != nil {
		return inode{}, e
	}
	if !safePath(path) {
		return inode{}, errors.New("unsafe ext4 source path")
	}
	parts := strings.Split(path, "/")
	if fs.prefix != "" {
		parts = append([]string{fs.prefix}, parts...)
	}
	num := uint64(2)
	for _, p := range parts {
		n, e := fs.inode(num)
		if e != nil {
			return inode{}, e
		}
		items, e := fs.entries(n)
		if e != nil {
			return inode{}, e
		}
		var ok bool
		num, ok = items[p]
		if !ok {
			return inode{}, fmt.Errorf("%w: %s", errMissing, path)
		}
	}
	return fs.inode(num)
}
func (fs *image) Read(path string) ([]byte, error) {
	n, e := fs.lookup(path)
	if e != nil {
		return nil, e
	}
	if n.mode&0xf000 != 0x8000 || n.size == 0 {
		return nil, errors.New("file missing or non-regular")
	}
	return fs.data(n)
}
func (fs *image) Exists(path string) (bool, error) {
	_, e := fs.lookup(path)
	if errors.Is(e, errMissing) {
		return false, nil
	}
	return e == nil, e
}
func (fs *image) Files(path string) ([]string, error) {
	n, e := fs.lookup(path)
	if e != nil {
		return nil, e
	}
	items, e := fs.entries(n)
	if e != nil {
		return nil, e
	}
	out := []string{}
	pending := false
	for key, id := range items {
		if key == "." || key == ".." {
			continue
		}
		if !safePath(key) {
			return nil, errors.New("unsafe directory member")
		}
		n, e := fs.inode(id)
		if errors.Is(e, errBlocks) {
			pending = true
			continue
		}
		if e != nil {
			return nil, e
		}
		if n.mode&0xf000 != 0x8000 {
			return nil, errors.New("unsupported non-file vendor config")
		}
		out = append(out, path+"/"+key)
	}
	if pending {
		return nil, errBlocks
	}
	if len(out) == 0 || len(out) > 512 {
		return nil, errors.New("empty/oversized config directory")
	}
	sortStrings(out)
	return out, nil
}
