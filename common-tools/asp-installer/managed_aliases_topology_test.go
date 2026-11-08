package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedAliasAncestorsAndLegacy(t *testing.T) {
	fixture := func(t *testing.T, ids []int) (string, string) {
		t.Helper()
		root := t.TempDir()
		external := t.TempDir()
		for _, dir := range []string{"etc", "lib/rootfs/proc/idme"} {
			if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		receipt, _ := json.Marshal(map[string]any{"miccal_inventory_version": 1, "miccal_aliases": ids})
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
		return root, external
	}
	for _, ids := range [][]int{{}, {0, 7}, {7, 200}} {
		t.Run("valid", func(t *testing.T) {
			root, _ := fixture(t, ids)
			if err := validateInstalledMiccalAliases(root); err != nil {
				t.Fatalf("valid saved alias topology rejected: %v", err)
			}
		})
	}
	t.Run("valid zero with absent idme", func(t *testing.T) {
		root, _ := fixture(t, []int{})
		if err := os.Remove(filepath.Join(root, "lib/rootfs/proc/idme")); err != nil {
			t.Fatal(err)
		}
		if err := validateInstalledMiccalAliases(root); err != nil {
			t.Fatal(err)
		}
	})
	for _, component := range []string{"lib", "lib/rootfs", "lib/rootfs/proc", "lib/rootfs/proc/idme", "etc"} {
		t.Run("redirect-"+component, func(t *testing.T) {
			root, external := fixture(t, []int{7})
			link := filepath.Join(root, filepath.FromSlash(component))
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(external, "target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			if component == "etc" {
				receipt, _ := json.Marshal(map[string]any{"miccal_inventory_version": 1, "miccal_aliases": []int{7}})
				if err := os.WriteFile(filepath.Join(target, "installer-receipt.json"), receipt, 0600); err != nil {
					t.Fatal(err)
				}
			} else if component == "lib/rootfs/proc/idme" {
				if err := os.Symlink("/proc/device-tree/idme/miccal.7/value", filepath.Join(target, "miccal.7")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if err := validateInstalledMiccalAliases(root); err == nil {
				t.Fatal("symlinked managed generation ancestor accepted")
			}
		})
	}
	t.Run("redirect leaf", func(t *testing.T) {
		root, _ := fixture(t, []int{7})
		p := filepath.Join(root, "lib/rootfs/proc/idme/miccal.7")
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/outside-rootfs/invalid", p); err != nil {
			t.Fatal(err)
		}
		if err := validateInstalledMiccalAliases(root); err == nil {
			t.Fatal("redirected leaf accepted")
		}
	})
	t.Run("legacy no inventory", func(t *testing.T) {
		root, _ := fixture(t, []int{7})
		if err := os.WriteFile(filepath.Join(root, "etc/installer-receipt.json"), []byte("{\"schema\":\"ASP1-INSTALLER-V3\"}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := validateInstalledMiccalAliases(root); err != nil {
			t.Fatalf("legacy receipt rejected: %v", err)
		}
	})
}
