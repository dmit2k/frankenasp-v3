package asp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dmit2k/frankenasp-v3/platforms"
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

// Factory extraction follows the discovered libasp.so and its DT_NEEDED
// edges. No helper or Echo codename participates in this process.
func (c *collector) closure() error {
	probes := []string{
		"vendor/lib/libasp.so", "system/lib/libasp.so",
		"system/bin/linker", "system/lib/ld-android.so",
		"system/build.prop", "vendor/build.prop",
		"system/etc/public.libraries.txt", "system/etc/public.libraries-AMAZON.txt",
		"vendor/etc/audio-algorithms", "system/etc/audio-algorithms",
	}
	existing, err := c.existMany(probes)
	if err != nil {
		return err
	}
	asp := ""
	if existing["vendor/lib/libasp.so"] {
		asp = "vendor/lib/libasp.so"
	}
	if asp == "" && existing["system/lib/libasp.so"] {
		asp = "system/lib/libasp.so"
	}
	if asp == "" {
		return errors.New("factory libasp.so not found in source library roots")
	}
	required := []string{asp}
	// The standalone system-lib layout needs only its loader and libasp
	// dependencies. Separate vendor-lib layouts additionally need linker
	// namespace metadata; this retains the existing V2 inventories.
	if existing["system/bin/linker"] {
		required = append(required, "system/bin/linker")
	}
	if strings.HasPrefix(asp, "vendor/") {
		for _, path := range probes[3:8] {
			if existing[path] {
				required = append(required, path)
			}
		}
	}
	// Collect the source's actual audio configuration files, without requiring
	// an arbitrary device-specific filename.
	for _, dir := range []string{"vendor/etc/audio-algorithms", "system/etc/audio-algorithms"} {
		if !existing[dir] {
			continue
		}
		paths, e := c.fileList(dir)
		if e != nil {
			return e
		}
		required = append(required, paths...)
	}
	if err := c.readMany(required); err != nil {
		return err
	}

	vndk := ""
	for _, prop := range []string{"vendor/build.prop", "system/build.prop"} {
		if version := sourceProperty(c.items[prop], "ro.vndk.version"); version != "" {
			vndk = version
			break
		}
	}
	// Linker namespace configuration matters for vendor-rooted libasp.
	// Standalone system-rooted libasp keeps its historical cache closure.
	if strings.HasPrefix(asp, "vendor/") {
		names := []string{"system/etc/ld.config.txt", "system/etc/ld.config.28.txt"}
		if vndk != "" && normalizedVersion(vndk) {
			names = append([]string{"system/etc/ld.config." + vndk + ".txt"}, names...)
		}
		present, err := c.existMany(names)
		if err != nil {
			return err
		}
		var configs []string
		for _, name := range names {
			if present[name] {
				configs = append(configs, name)
			}
		}
		if err := c.readMany(configs); err != nil {
			return err
		}
		for _, filename := range configs {
			if vndk == "" {
				if m := vndkDirectoryPattern.FindSubmatch(c.items[filename]); len(m) > 1 {
					vndk = string(m[1])
				}
			}
		}
	}
	if existing["system/bin/linker"] {
		if _, err := elfNeeded(c.items["system/bin/linker"], true, ""); err != nil {
			return fmt.Errorf("linker: %w", err)
		}
	}
	priorities := []string{"system/lib", "vendor/lib"}
	if strings.HasPrefix(asp, "vendor/") {
		priorities = []string{"vendor/lib"}
		if vndk != "" && normalizedVersion(vndk) {
			priorities = append(priorities, "system/lib/vndk-"+vndk, "system/lib/vndk-sp-"+vndk)
		}
		priorities = append(priorities, "system/lib")
	}
	queue := []runtimeFile{{asp, c.items[asp]}}
	visited := map[string]bool{}
	var bytesTotal uint64
	for len(queue) > 0 {
		var missingNames []string
		for _, node := range queue {
			if visited[node.path] {
				continue
			}
			visited[node.path] = true
			bytesTotal += uint64(len(node.data))
			libs, err := elfNeeded(node.data, true, "")
			if err != nil {
				return fmt.Errorf("%s: %w", node.path, err)
			}
			missingNames = append(missingNames, libs...)
		}
		queue = nil
		if len(visited) > 256 || bytesTotal > 128<<20 {
			return errors.New("ELF dependency closure exceeds 256 files/128MiB")
		}
		if len(missingNames) == 0 {
			break
		}
		var candidates []string
		for _, name := range missingNames {
			for _, dir := range priorities {
				candidates = append(candidates, dir+"/"+name)
			}
		}
		exists, err := c.existMany(candidates)
		if err != nil {
			return err
		}
		selected := map[string]string{}
		for _, name := range missingNames {
			if selected[name] != "" {
				continue
			}
			for _, dir := range priorities {
				path := dir + "/" + name
				if exists[path] {
					selected[name] = path
					break
				}
			}
			if selected[name] == "" {
				return fmt.Errorf("missing Bionic dependency %q", name)
			}
		}
		var paths []string
		for _, path := range selected {
			paths = append(paths, path)
		}
		if err := c.readMany(paths); err != nil {
			return err
		}
		for _, name := range missingNames {
			path := selected[name]
			queue = append(queue, runtimeFile{path, c.items[path]})
			c.edgeCount++
		}
	}
	return nil
}

var vndkDirectoryPattern = regexp.MustCompile(`vndk-([0-9]+)`)

func normalizedVersion(value string) bool {
	if value == "" || len(value) > 5 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validateHelperRuntime preserves the historical source-level compatibility API.
// All new V4 consumers supply a verified runtime library search contract.
func validateHelperRuntime(platform string, child []byte, items map[string][]byte) error {
	c, ok, e := platforms.LegacyContract(platform)
	if e != nil {
		return e
	}
	if !ok {
		return fmt.Errorf("no runtime contract for %q", platform)
	}
	return ValidateHelperRuntimeForPaths(child, items, c.Runtime.LibrarySearch)
}

// ValidateHelperRuntimeForPaths validates the selected helper against the
// actual factory cache and the launcher-declared ordered library lookup.
// This is bounded static ELF closure, not a full Android linker emulator.
func ValidateHelperRuntimeForPaths(child []byte, items map[string][]byte, librarySearch []string) error {
	libs, err := elfNeeded(child, false, "/system/bin/linker")
	if err != nil {
		return fmt.Errorf("helper ELF: %w", err)
	}
	if len(items["system/bin/linker"]) == 0 {
		return errors.New("helper loader /system/bin/linker absent from factory cache")
	}
	if _, e := elfNeeded(items["system/bin/linker"], true, ""); e != nil {
		return fmt.Errorf("helper loader /system/bin/linker invalid ELF: %w", e)
	}
	seen := map[string]bool{}
	queue := append([]string{}, libs...)
	if len(queue) > 256 {
		return errors.New("helper dependency closure exceeded")
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		if len(seen) > 256 {
			return errors.New("helper dependency closure exceeds 256")
		}
		if !safePath(name) || strings.Contains(name, "/") {
			return fmt.Errorf("invalid helper dependency %q", name)
		}
		chosen := ""
		for _, dir := range librarySearch {
			if len(items[dir+"/"+name]) > 0 {
				chosen = dir + "/" + name
				break
			}
		}
		if chosen == "" {
			return fmt.Errorf("helper requires %s, absent from declared library paths", name)
		}
		nested, e := elfNeeded(items[chosen], true, "")
		if e != nil {
			return fmt.Errorf("helper runtime dependency %s: %w", chosen, e)
		}
		queue = append(queue, nested...)
	}
	return nil
}
