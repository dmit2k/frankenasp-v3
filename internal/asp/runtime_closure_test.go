package asp

import (
	"encoding/binary"
	"strings"
	"testing"
)

// runtimeELF builds a minimal synthetic ARM32/EABI5 ELF with true DT_NEEDED,
// PT_DYNAMIC and mapped DT_STRTAB entries. It contains no proprietary bytes.
func runtimeELF(t *testing.T, shared, interpreter bool, needed ...string) []byte {
	t.Helper()
	b := make([]byte, 1024)
	copy(b[:4], "\x7fELF")
	b[4], b[5], b[6] = 1, 1, 1
	order := binary.LittleEndian
	typ := uint16(2)
	if shared {
		typ = 3
	}
	order.PutUint16(b[16:], typ)
	order.PutUint16(b[18:], 40)
	order.PutUint32(b[20:], 1)
	order.PutUint32(b[28:], 52)
	order.PutUint32(b[36:], 0x05000000)
	order.PutUint16(b[42:], 32)
	count := uint16(2)
	if interpreter {
		count++
	}
	order.PutUint16(b[44:], count)
	order.PutUint32(b[52:], 1)
	order.PutUint32(b[52+4:], 0)
	order.PutUint32(b[52+8:], 0)
	order.PutUint32(b[52+16:], uint32(len(b)))
	next := 84
	if interpreter {
		target := "/system/bin/linker\x00"
		copy(b[180:], target)
		order.PutUint32(b[next:], 3)
		order.PutUint32(b[next+4:], 180)
		order.PutUint32(b[next+16:], uint32(len(target)))
		next += 32
	}
	dy := 256
	names := []byte{0}
	var positions []uint32
	for _, n := range needed {
		if strings.Contains(n, "/") {
			t.Fatal("invalid fixture SONAME")
		}
		positions = append(positions, uint32(len(names)))
		names = append(names, []byte(n)...)
		names = append(names, 0)
	}
	copy(b[512:], names)
	current := dy
	add := func(tag, value uint32) {
		order.PutUint32(b[current:], tag)
		order.PutUint32(b[current+4:], value)
		current += 8
	}
	for _, position := range positions {
		add(1, position)
	}
	add(5, 512)                 // DT_STRTAB
	add(10, uint32(len(names))) // DT_STRSZ
	add(0, 0)
	order.PutUint32(b[next:], 2)
	order.PutUint32(b[next+4:], uint32(dy))
	order.PutUint32(b[next+16:], uint32(current-dy))
	return b
}

func TestOrderedTransitiveHelperDependencyClosure(t *testing.T) {
	helper := runtimeELF(t, false, true, "libroot.so")
	rootSystem := runtimeELF(t, true, false, "libbranch.so")
	rootVendor := runtimeELF(t, true, false, "libmissing.so")
	branch := runtimeELF(t, true, false)
	items := map[string][]byte{
		"system/bin/linker":       runtimeELF(t, true, false),
		"system/lib/libroot.so":   rootSystem,
		"system/lib/libbranch.so": branch,
		"vendor/lib/libroot.so":   rootVendor,
	}
	if err := ValidateHelperRuntimeForPaths(helper, items, []string{"system/lib", "vendor/lib"}); err != nil {
		t.Fatalf("ordered system-first closure rejected: %v", err)
	}
	if err := ValidateHelperRuntimeForPaths(helper, items, []string{"vendor/lib", "system/lib"}); err == nil {
		t.Fatal("wrong prioritized root accepted without transitive libmissing")
	}
	delete(items, "system/lib/libbranch.so")
	if err := ValidateHelperRuntimeForPaths(helper, items, []string{"system/lib", "vendor/lib"}); err == nil {
		t.Fatal("missing transitive dependency accepted")
	}
	items["system/lib/libbranch.so"] = []byte("not an ELF")
	if err := ValidateHelperRuntimeForPaths(helper, items, []string{"system/lib", "vendor/lib"}); err == nil {
		t.Fatal("malformed transitive shared ELF accepted")
	}
}

func TestHelperDependencyCheckDoesNotMutateFactoryInventory(t *testing.T) {
	helper := runtimeELF(t, false, true, "libroot.so")
	files := map[string][]byte{
		"system/bin/linker":     runtimeELF(t, true, false),
		"system/lib/libroot.so": runtimeELF(t, true, false),
	}
	n := len(files)
	if err := ValidateHelperRuntimeForPaths(helper, files, []string{"system/lib"}); err != nil {
		t.Fatal(err)
	}
	if len(files) != n {
		t.Fatal("helper dependencies altered factory content identity")
	}
	if _, ok := files["assets/asp1-child"]; ok {
		t.Fatal("helper was added to factory cache")
	}
}
