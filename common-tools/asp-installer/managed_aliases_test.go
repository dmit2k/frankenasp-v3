package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedMiccalInventoryValidatesOwnedGenerationWithoutHostDiscovery(t *testing.T) {
	fixture := func(t *testing.T, ids []int) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "etc"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "lib/rootfs/proc/idme"), 0700); err != nil {
			t.Fatal(err)
		}
		receipt, _ := json.Marshal(map[string]any{"schema": "ASP1-INSTALLER-V3", "miccal_inventory_version": 1, "miccal_aliases": ids})
		if err := os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), receipt, 0600); err != nil {
			t.Fatal(err)
		}
		for name, target := range aliasTargetsForMiccal(ids) {
			if len(name) < 6 || name[:6] != "idme/m" {
				continue
			}
			if err := os.Symlink(target, filepath.Join(root, "lib/rootfs/proc", filepath.FromSlash(name))); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	t.Run("persisted not current host", func(t *testing.T) {
		root := fixture(t, []int{0, 7})
		if err := validateInstalledMiccalAliases(root); err != nil {
			t.Fatal(err)
		}
		// Invariants must not depend on a new discovery from mutable host /sys.
	})
	for _, tc := range []struct {
		name   string
		modify func(t *testing.T, root string)
	}{
		{"missing", func(t *testing.T, root string) { os.Remove(filepath.Join(root, "lib/rootfs/proc/idme/miccal.7")) }},
		{"redirected", func(t *testing.T, root string) {
			p := filepath.Join(root, "lib/rootfs/proc/idme/miccal.7")
			os.Remove(p)
			os.Symlink("/untrusted", p)
		}},
		{"extra", func(t *testing.T, root string) {
			os.Symlink("/untrusted", filepath.Join(root, "lib/rootfs/proc/idme/miccal.9"))
		}},
		{"not symlink", func(t *testing.T, root string) {
			p := filepath.Join(root, "lib/rootfs/proc/idme/miccal.7")
			os.Remove(p)
			os.WriteFile(p, []byte("bad"), 0600)
		}},
		{"bad indices", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"miccal_inventory_version\":1,\"miccal_aliases\":[999,-1,7,7]}"), 0600)
		}},
		{"duplicates", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"miccal_inventory_version\":1,\"miccal_aliases\":[0,7,7]}"), 0600)
		}},
		{"unsorted", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"miccal_inventory_version\":1,\"miccal_aliases\":[7,0]}"), 0600)
		}},
		{"null", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"miccal_inventory_version\":1,\"miccal_aliases\":null}"), 0600)
		}},
		{"missing marked", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"miccal_inventory_version\":1}"), 0600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, []int{0, 7})
			tc.modify(t, root)
			if err := validateInstalledMiccalAliases(root); err == nil {
				t.Fatal("invalid saved miccal generation accepted")
			}
		})
	}
	t.Run("legacy unmarked receipt", func(t *testing.T) {
		root := fixture(t, []int{0, 7})
		if err := os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"schema\":\"ASP1-INSTALLER-V3\"}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := validateInstalledMiccalAliases(root); err != nil {
			t.Fatalf("legacy unsupported: %v", err)
		}
	})
}
