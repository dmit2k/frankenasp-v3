package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformReleaseContractStrictIdentityAndRequiredResources(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"installer", "launcher", "child", "LICENSE"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	contract := filepath.Join(dir, "contract.json")
	params := []string{
		"--platform", "9_test-model", "--version", "3.6",
		"--launcher", filepath.Join(dir, "launcher"),
		"--child", filepath.Join(dir, "child"),
		"--installer", filepath.Join(dir, "installer"),
		"--license", filepath.Join(dir, "LICENSE"),
		"--out-dir", dir,
		"--contract", contract,
	}
	write := func(contents string) {
		t.Helper()
		if err := os.WriteFile(contract, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"platform":"wrong-model","runtime":{"vendor_modes":["bundled"],"library_search":["system/lib"]}}`)
	if err := run(params); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("foreign platform contract accepted: %v", err)
	}
	write(`{"platform":"9_test-model","platform":"9_test-model","runtime":{"vendor_modes":["bundled"],"library_search":["system/lib"]}}`)
	if err := run(params); err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("duplicate contract field accepted: %v", err)
	}
	write(`{"platform":"9_test-model","runtime":{"vendor_modes":["bundled"],"library_search":["system/lib"]},"required_data":[{"path":"assets/resource","install_to":"lib/rootfs/factory/resource"}]}`)
	if err := run(params); err == nil || !strings.Contains(err.Error(), "missing required platform release data") {
		t.Fatalf("required platform-owned resource ignored: %v", err)
	}
}
