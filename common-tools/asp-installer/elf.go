package main

import (
	"errors"
	"fmt"
	"strings"
)

type phdr struct{ typ, off, vaddr, filesz uint64 }

func elfNeeded(data []byte, shared bool, interp string) ([]string, error) {
	if len(data) < 52 || string(data[:4]) != "\x7fELF" || data[4] != 1 || data[5] != 1 || data[6] != 1 {
		return nil, errors.New("not ELF32 little endian")
	}
	if le16(data, 18) != 40 || le32(data, 20) != 1 || le32(data, 36)&0xff000000 != 0x05000000 {
		return nil, errors.New("not ARM EABI5")
	}
	if shared && le16(data, 16) != 3 {
		return nil, errors.New("expected shared ELF")
	}
	if le16(data, 16) != 2 && le16(data, 16) != 3 {
		return nil, errors.New("invalid ELF type")
	}
	count, phsize, at := le16(data, 44), le16(data, 42), le32(data, 28)
	if count == 0 || count > 128 || phsize != 32 || at+count*32 > uint64(len(data)) {
		return nil, errors.New("invalid ELF program headers")
	}
	var load, dy *phdr
	var ptInterp []string
	for i := uint64(0); i < count; i++ {
		n := int(at + i*32)
		p := phdr{le32(data, n), le32(data, n+4), le32(data, n+8), le32(data, n+16)}
		if p.off+p.filesz > uint64(len(data)) {
			return nil, errors.New("ELF program header outside file")
		}
		switch p.typ {
		case 1:
			if load == nil {
				cp := p
				load = &cp
			}
		case 2:
			cp := p
			dy = &cp
		case 3:
			ptInterp = append(ptInterp, string(data[p.off:p.off+p.filesz]))
		}
	}
	if load == nil {
		return nil, errors.New("ELF has no load segment")
	}
	if interp != "" {
		if len(ptInterp) != 1 || ptInterp[0] != interp+"\x00" {
			return nil, errors.New("wrong helper ELF interpreter")
		}
	} else if len(ptInterp) > 0 && (!shared || len(ptInterp) != 1 || ptInterp[0] != "/system/bin/linker\x00") {
		return nil, errors.New("unexpected ELF interpreter")
	}
	if dy == nil {
		return nil, nil
	}
	if dy.filesz%8 != 0 {
		return nil, errors.New("unaligned ELF dynamic section")
	}
	needed := []uint64{}
	var strVA, strsz uint64
	for at := dy.off; at < dy.off+dy.filesz; at += 8 {
		tag, v := le32(data, int(at)), le32(data, int(at+4))
		switch tag {
		case 1:
			needed = append(needed, v)
		case 5:
			strVA = v
		case 10:
			strsz = v
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	if strsz == 0 || strsz > 64<<20 {
		return nil, errors.New("invalid ELF dynamic string table")
	}
	var base uint64
	found := false
	for i := uint64(0); i < count; i++ {
		n := int(at + i*32)
		if le32(data, n) != 1 {
			continue
		}
		vaddr, filesz, off := le32(data, n+8), le32(data, n+16), le32(data, n+4)
		if strVA >= vaddr && strVA < vaddr+filesz {
			base = off + strVA - vaddr
			found = true
			break
		}
	}
	if !found || base+strsz > uint64(len(data)) {
		return nil, errors.New("unmapped ELF string table")
	}
	out := []string{}
	for _, n := range needed {
		if n >= strsz {
			return nil, errors.New("invalid DT_NEEDED offset")
		}
		s := data[base+n : base+strsz]
		end := -1
		for i, c := range s {
			if c == 0 {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unterminated DT_NEEDED")
		}
		name := string(s[:end])
		if !safePath(name) || strings.Contains(name, "/") {
			return nil, errors.New("unsafe DT_NEEDED")
		}
		out = append(out, name)
	}
	return out, nil
}

type runtimeFile struct {
	path string
	data []byte
}
type collector struct {
	src       *otaSources
	items     map[string][]byte
	edgeCount int
}

func newCollector(src *otaSources) *collector {
	return &collector{src: src, items: map[string][]byte{}}
}
func (c *collector) reader(p string) (files, string, error) {
	parts := strings.SplitN(p, "/", 2)
	if len(parts) != 2 || !safePath(p) {
		return nil, "", errors.New("unsafe runtime path")
	}
	switch parts[0] {
	case "system":
		return c.src.system, parts[1], nil
	case "vendor":
		return c.src.vendor, parts[1], nil
	}
	return nil, "", errors.New("unsupported runtime source prefix")
}
func (c *collector) readMany(paths []string) error {
	failures := map[string]error{}
	pending := map[string]bool{}
	for _, p := range paths {
		if _, ok := c.items[p]; !ok {
			pending[p] = true
		}
	}
	return c.src.resolve(func() error {
		missing := false
		for p := range pending {
			reader, sub, e := c.reader(p)
			if e != nil {
				return e
			}
			raw, e := reader.Read(sub)
			if errors.Is(e, errBlocks) {
				missing = true
				continue
			}
			if e != nil {
				failures[p] = e
				delete(pending, p)
				continue
			}
			if len(raw) > 64<<20 {
				return errors.New("oversize runtime file")
			}
			c.items[p] = raw
			var total int
			for _, contents := range c.items {
				total += len(contents)
			}
			if total > 128<<20 {
				return errors.New("runtime files exceed 128MiB")
			}
			delete(pending, p)
		}
		if missing {
			return errBlocks
		}
		if len(failures) > 0 {
			for p, e := range failures {
				return fmt.Errorf("%s: %w", p, e)
			}
		}
		return nil
	})
}
func (c *collector) fileList(dir string) ([]string, error) {
	var result []string
	err := c.src.resolve(func() error {
		r, s, e := c.reader(dir)
		if e != nil {
			return e
		}
		result, e = r.Files(s)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(result))
	prefix := strings.SplitN(dir, "/", 2)[0] + "/"
	for _, f := range result {
		out = append(out, prefix+f)
	}
	return out, nil
}
func (c *collector) existMany(paths []string) (map[string]bool, error) {
	result := map[string]bool{}
	pending := map[string]bool{}
	for _, p := range paths {
		pending[p] = true
	}
	err := c.src.resolve(func() error {
		hasMissing := false
		for p := range pending {
			r, sub, e := c.reader(p)
			if e != nil {
				return e
			}
			yes, e := r.Exists(sub)
			if errors.Is(e, errBlocks) {
				hasMissing = true
				continue
			}
			if e != nil {
				return e
			}
			result[p] = yes
			delete(pending, p)
		}
		if hasMissing {
			return errBlocks
		}
		return nil
	})
	return result, err
}
func (c *collector) closure(platform string) error {
	configFiles, e := c.fileList("vendor/etc/audio-algorithms")
	if e != nil {
		return e
	}
	required := []string{"system/bin/linker"}
	if platform == "athena" {
		required = append(required, "vendor/lib/libasp.so", "vendor/build.prop", "system/lib/ld-android.so", "system/build.prop", "system/etc/ld.config.28.txt", "system/etc/public.libraries.txt", "system/etc/public.libraries-AMAZON.txt")
	} else {
		required = append(required, "system/lib/libasp.so")
	}
	required = append(required, configFiles...)
	if err := c.readMany(required); err != nil {
		return err
	}
	for _, name := range []string{"AFE.cfg"} {
		if len(c.items["vendor/etc/audio-algorithms/"+name]) == 0 {
			return errors.New("required AFE.cfg missing")
		}
	}
	if platform == "athena" && len(c.items["vendor/etc/audio-algorithms/asp.cfg"]) == 0 {
		return errors.New("required asp.cfg missing")
	}
	if platform == "rook" && len(c.items["vendor/etc/audio-algorithms/coefs_FBF.cfg"]) == 0 {
		return errors.New("required coefs_FBF.cfg missing")
	}
	if _, e = elfNeeded(c.items["system/bin/linker"], true, ""); e != nil {
		return fmt.Errorf("linker: %w", e)
	}
	aspirant := "system/lib/libasp.so"
	priorities := []string{"system/lib", "vendor/lib"}
	if platform == "athena" {
		aspirant = "vendor/lib/libasp.so"
		priorities = []string{"vendor/lib", "system/lib/vndk-28", "system/lib/vndk-sp-28", "system/lib"}
	}
	queue := []runtimeFile{{aspirant, c.items[aspirant]}}
	visited := map[string]bool{}
	var bytesTotal uint64
	for len(queue) > 0 {
		missingNames := []string{}
		for _, node := range queue {
			if visited[node.path] {
				continue
			}
			visited[node.path] = true
			bytesTotal += uint64(len(node.data))
			libs, e := elfNeeded(node.data, true, "")
			if e != nil {
				return fmt.Errorf("%s: %w", node.path, e)
			}
			for _, name := range libs {
				missingNames = append(missingNames, name)
			}
		}
		queue = nil
		if len(visited) > 256 || bytesTotal > 128<<20 {
			return errors.New("ELF dependency closure exceeds 256 files/128MiB")
		}
		if len(missingNames) == 0 {
			break
		}
		candidates := []string{}
		for _, name := range missingNames {
			for _, dir := range priorities {
				candidates = append(candidates, dir+"/"+name)
			}
		}
		exists, e := c.existMany(candidates)
		if e != nil {
			return e
		}
		selected := map[string]string{}
		for _, name := range missingNames {
			if _, ok := selected[name]; ok {
				continue
			}
			for _, dir := range priorities {
				p := dir + "/" + name
				if exists[p] {
					selected[name] = p
					break
				}
			}
			if selected[name] == "" {
				return fmt.Errorf("missing Bionic dependency %q", name)
			}
		}
		all := []string{}
		for _, p := range selected {
			all = append(all, p)
		}
		if e := c.readMany(all); e != nil {
			return e
		}
		for _, name := range missingNames {
			p := selected[name]
			queue = append(queue, runtimeFile{p, c.items[p]})
			c.edgeCount++
		}
	}
	return nil
}

// validateHelperRuntime checks the selected helper against an already extracted
// factory runtime. It never expands the factory dependency closure and is not
// part of extraction or cache identity.
func validateHelperRuntime(platform string, child []byte, items map[string][]byte) error {
	libs, err := elfNeeded(child, false, "/system/bin/linker")
	if err != nil {
		return fmt.Errorf("helper ELF: %w", err)
	}
	priorities := []string{"system/lib", "vendor/lib"}
	if platform == "athena" {
		priorities = []string{"vendor/lib", "system/lib/vndk-28", "system/lib/vndk-sp-28", "system/lib"}
	}
	if len(items["system/bin/linker"]) == 0 {
		return errors.New("helper loader /system/bin/linker absent from factory cache")
	}
	for _, name := range libs {
		found := false
		for _, dir := range priorities {
			if len(items[dir+"/"+name]) != 0 {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("helper requires %s, absent from factory cache", name)
		}
	}
	return nil
}
