package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Reject symlinked ancestors in a managed generation before inspecting its
// receipt or aliases. The generation is installer-owned and not concurrently
// modified by untrusted users; no separate descriptor traversal is needed.
func managedDirectory(root string, parts ...string) (string, error) {
	current := root
	for _, part := range append([]string{""}, parts...) {
		if part != "" {
			current = filepath.Join(current, part)
		}
		st, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("invalid managed directory %s", current)
		}
	}
	return current, nil
}

// Validate the saved generation inventory without reading calibration values.
// Old V3 receipts do not contain this inventory and remain compatible.
func validateInstalledMiccalAliases(root string) error {
	etc, err := managedDirectory(root, "etc")
	if err != nil {
		return fmt.Errorf("invalid managed receipt directory: %w", err)
	}
	receiptPath := filepath.Join(etc, "installer-receipt.json")
	st, err := os.Lstat(receiptPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 64<<10 {
		return errors.New("invalid installed calibration receipt file")
	}
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		return err
	}
	if len(receipt) > 64<<10 {
		return errors.New("oversized installed calibration receipt")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(receipt, &fields); err != nil {
		return fmt.Errorf("invalid installer receipt: %w", err)
	}
	raw, hasInventory := fields["miccal_aliases"]
	revision, marked := fields["miccal_inventory_version"]
	if marked {
		var version int
		if err := json.Unmarshal(revision, &version); err != nil || version != 1 {
			return errors.New("invalid miccal inventory revision")
		}
		if !hasInventory {
			return errors.New("missing saved miccal aliases")
		}
	}
	if !hasInventory {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("null saved miccal inventory")
	}
	var ids []int
	if err := json.Unmarshal(raw, &ids); err != nil {
		return fmt.Errorf("malformed saved miccal inventory: %w", err)
	}
	if len(ids) > 64 {
		return errors.New("too many saved miccal aliases")
	}
	expected := make(map[string]string, len(ids))
	previous := -1
	for _, id := range ids {
		if id < 0 || id > 255 || id <= previous {
			return errors.New("noncanonical saved miccal alias indices")
		}
		previous = id
		name := "miccal." + strconv.Itoa(id)
		expected[name] = "/proc/device-tree/idme/" + name + "/value"
	}
	idme, err := managedDirectory(root, "lib", "rootfs", "proc", "idme")
	if errors.Is(err, os.ErrNotExist) && len(expected) == 0 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invalid managed miccal directory: %w", err)
	}
	entries, err := os.ReadDir(idme)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return errors.New("missing or extra saved miccal aliases")
	}
	for _, entry := range entries {
		target, ok := expected[entry.Name()]
		if !ok || entry.Type()&os.ModeSymlink == 0 {
			return fmt.Errorf("unexpected saved miccal alias %q", entry.Name())
		}
		actual, err := os.Readlink(filepath.Join(idme, entry.Name()))
		if err != nil || actual != target {
			return fmt.Errorf("saved miccal alias %q has incorrect target", entry.Name())
		}
	}
	return nil
}
