package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRookAutoSelectsBundled(t *testing.T) {
	mode, _, e := chooseVendorMode("rook", "auto", map[string][]byte{})
	if e != nil || mode != "bundled" {
		t.Fatalf("rook auto rejected: %v, %s", e, mode)
	}
	_, _, e = chooseVendorMode("rook", "live", map[string][]byte{})
	if e == nil {
		t.Fatal("rook accepted unsupported live vendor")
	}
}
func TestAtomicExchangeAndRefusal(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "external-asp")
	candidate := filepath.Join(base, ".external-asp-stage")
	if e := os.Mkdir(first, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(candidate, 0700); e != nil {
		t.Fatal(e)
	}
	fixtureFile(t, first, "etc/version", []byte("old"))
	fixtureFile(t, candidate, "etc/version", []byte("new"))
	if e := unix.Renameat2(unix.AT_FDCWD, candidate, unix.AT_FDCWD, first, unix.RENAME_EXCHANGE); e != nil {
		t.Fatal(e)
	}
	newValue, _ := os.ReadFile(filepath.Join(first, "etc/version"))
	oldValue, _ := os.ReadFile(filepath.Join(candidate, "etc/version"))
	if string(newValue) != "new" || string(oldValue) != "old" {
		t.Fatalf("lost generation: new %q old %q", newValue, oldValue)
	}
	if e := unix.Renameat2(unix.AT_FDCWD, filepath.Join(base, "not-present"), unix.AT_FDCWD, first, unix.RENAME_EXCHANGE); e == nil {
		t.Fatal("unexpected exchange of missing stage")
	}
	again, _ := os.ReadFile(filepath.Join(first, "etc/version"))
	if string(again) != "new" {
		t.Fatalf("failed swap changed installed tree: %s", again)
	}
}
func TestProcessProofSelf(t *testing.T) {
	proof, e := processInfo(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	if proof.pid != os.Getpid() || proof.start == "" || proof.exe == (fileID{}) {
		t.Fatalf("invalid process proof: %+v", proof)
	}
}
