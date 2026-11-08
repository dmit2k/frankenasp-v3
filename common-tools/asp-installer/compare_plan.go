package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
)

// compareInstalledWithPlan compares exactly the bytes used to prepare a staged
// runtime; no release asset is reopened after validation.
func compareInstalledWithPlan(plan asp.AssetPlan, items map[string][]byte, mode, installed string, miccal []int) (compareResult, error) {
	result := compareResult{Changed: []difference{}, Mode: mode}
	if st, e := os.Lstat(installed); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("external-asp directory not installed or unsafe")
	}
	expected := map[string][]byte{}
	for p, b := range items {
		if mode == "live" && strings.HasPrefix(p, "vendor/") {
			continue
		}
		expected["lib/rootfs/"+p] = b
	}
	for _, f := range plan.Installed {
		b, ok := plan.Files[f.Path]
		if !ok {
			return result, errors.New("missing verified release resource")
		}
		expected[f.InstallTo] = b
	}
	expected["etc/launcher.profile"] = []byte(profileForMode(mode))
	for name, b := range expected {
		result.check(name, b, installed)
	}
	expectedLinks := map[string]string{}
	for name, target := range aliasTargetsForMiccal(miccal) {
		p := "lib/rootfs/proc/" + name
		expectedLinks[p] = target
		result.Compared++
		actual, e := os.Readlink(filepath.Join(installed, filepath.FromSlash(p)))
		if e != nil || actual != target {
			result.Changed = append(result.Changed, difference{p, "invalid-or-missing-symlink"})
		}
	}
	// Receipts from the new installer record only calibrated alias indices.
	// Old recognized generations have no inventory field and retain legacy
	// status/rollback compatibility; they are never retroactively rewritten.
	receiptPath := filepath.Join(installed, "etc/installer-receipt.json")
	if st, e := os.Lstat(receiptPath); e == nil {
		result.Compared++
		if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 64<<10 {
			result.Changed = append(result.Changed, difference{"etc/installer-receipt.json", "invalid-receipt"})
		} else if b, e := os.ReadFile(receiptPath); e != nil {
			result.Changed = append(result.Changed, difference{"etc/installer-receipt.json", "unreadable-receipt"})
		} else {
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(b, &fields); e != nil {
				result.Changed = append(result.Changed, difference{"etc/installer-receipt.json", "malformed-receipt"})
			} else if inventory, ok := fields["miccal_aliases"]; ok {
				var recorded []int
				if bytes.Equal(bytes.TrimSpace(inventory), []byte("null")) {
					result.Changed = append(result.Changed, difference{"etc/installer-receipt.json", "invalid-calibration-alias-inventory"})
				} else if e = json.Unmarshal(inventory, &recorded); e != nil || len(recorded) > 64 || !reflect.DeepEqual(recorded, miccal) {
					result.Changed = append(result.Changed, difference{"etc/installer-receipt.json", "calibration-alias-inventory-mismatch"})
				}
			}
		}
	} else if !os.IsNotExist(e) {
		return result, fmt.Errorf("read installer receipt: %w", e)
	}
	root := filepath.Join(installed, "lib/rootfs")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.Type().IsRegular() && d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, e := filepath.Rel(installed, p)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		if d.Type().IsRegular() {
			if _, ok := expected[name]; !ok {
				result.Changed = append(result.Changed, difference{name, "extra-installed"})
			}
		} else if d.Type()&os.ModeSymlink != 0 {
			if _, ok := expectedLinks[name]; !ok {
				result.Changed = append(result.Changed, difference{name, "extra-symlink"})
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	sort.Slice(result.Changed, func(i, j int) bool { return result.Changed[i].Path < result.Changed[j].Path })
	result.Matching = len(result.Changed) == 0
	return result, nil
}
