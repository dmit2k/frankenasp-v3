package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

func TestVendorCapabilitiesAndReadOnlyEvidence(t *testing.T) {
	root := t.TempDir()
	source := map[string][]byte{
		"vendor/lib/libasp.so": []byte("synthetic vendor bytes"),
		"system/lib/libc.so":   []byte("system should remain cache-backed"),
	}
	vendorFile := filepath.Join(root, "lib", "libasp.so")
	if err := os.MkdirAll(filepath.Dir(vendorFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendorFile, source["vendor/lib/libasp.so"], 0600); err != nil {
		t.Fatal(err)
	}
	bundled := asp.ReleaseRuntime{VendorModes: []string{"bundled"}}
	live := asp.ReleaseRuntime{VendorModes: []string{"bundled", "live"}}
	readonly := func() error { return nil }
	writable := func() error { return errors.New("live vendor writable") }
	check := func(runtime asp.ReleaseRuntime, mode string, files map[string][]byte, readOnly func() error, wantMode string, wantErr bool) {
		t.Helper()
		got, _, err := chooseVendorModeForRoot(runtime, mode, files, root, readOnly)
		if (err != nil) != wantErr || (err == nil && got != wantMode) {
			t.Fatalf("vendor mode expected %s err=%v; got %q err=%v", wantMode, wantErr, got, err)
		}
	}
	check(bundled, "auto", source, readonly, "bundled", false)
	check(bundled, "live", source, readonly, "", true)
	check(live, "auto", source, readonly, "live", false)
	check(live, "live", source, readonly, "live", false)
	check(live, "bundled", source, readonly, "bundled", false)
	check(live, "auto", source, writable, "bundled", false)
	check(live, "live", source, writable, "", true)
	check(live, "auto", map[string][]byte{"system/lib/libc.so": []byte("system-only")}, readonly, "bundled", false)
	check(live, "live", map[string][]byte{"system/lib/libc.so": []byte("system-only")}, readonly, "", true)
	if err := os.WriteFile(vendorFile, []byte("wrong vendor"), 0600); err != nil {
		t.Fatal(err)
	}
	check(live, "auto", source, readonly, "bundled", false)
	check(live, "live", source, readonly, "", true)
	if err := os.Remove(vendorFile); err != nil {
		t.Fatal(err)
	}
	check(live, "auto", source, readonly, "bundled", false)
	check(live, "live", source, readonly, "", true)
}
