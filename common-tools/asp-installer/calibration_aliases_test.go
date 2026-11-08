package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestMiccal(t *testing.T, root string, index int, value []byte) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("miccal.%d", index))
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "value"), value, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestMiccalInventoriesAndSparseIndices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ids   []int
		empty []int
	}{
		{"zero", nil, nil},
		{"two", []int{0, 1}, nil},
		{"four", []int{0, 1, 2, 3}, nil},
		{"six", []int{0, 1, 2, 3, 4, 5}, nil},
		{"sparse", []int{0, 7, 31, 255}, nil},
		{"empty skipped", []int{0, 1, 8}, []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, id := range tc.ids {
				b := []byte{0x22, 0x00}
				for _, empty := range tc.empty {
					if id == empty {
						b = nil
					}
				}
				writeTestMiccal(t, dir, id, b)
			}
			result, e := discoverMiccalAt(dir, uint32(os.Geteuid()))
			if e != nil {
				t.Fatal(e)
			}
			want := make([]int, 0)
			for _, id := range tc.ids {
				skip := false
				for _, empty := range tc.empty {
					if id == empty {
						skip = true
					}
				}
				if !skip {
					want = append(want, id)
				}
			}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("discovery mismatch: got %v want %v", result, want)
			}
			links := aliasTargetsForMiccal(result)
			if len(links) != len(result)+4 {
				t.Fatal("unexpected alias count")
			}
			if _, ok := links["idme/miccal.2"]; ok && tc.name == "two" {
				t.Fatal("fabricated miccal.2")
			}
		})
	}
}

func TestMiccalRejectsHostileEntries(t *testing.T) {
	cases := []struct {
		name string
		make func(*testing.T, string)
	}{
		{"index leading zero", func(t *testing.T, dir string) {
			if e := os.Mkdir(filepath.Join(dir, "miccal.01"), 0700); e != nil {
				t.Fatal(e)
			}
		}},
		{"index oversized", func(t *testing.T, dir string) {
			if e := os.Mkdir(filepath.Join(dir, "miccal.256"), 0700); e != nil {
				t.Fatal(e)
			}
		}},
		{"symlink directory", func(t *testing.T, dir string) {
			if e := os.Symlink(t.TempDir(), filepath.Join(dir, "miccal.1")); e != nil {
				t.Fatal(e)
			}
		}},
		{"symlink value", func(t *testing.T, dir string) {
			writeTestMiccal(t, dir, 1, []byte{1})
			if e := os.Remove(filepath.Join(dir, "miccal.1/value")); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink("/etc/passwd", filepath.Join(dir, "miccal.1/value")); e != nil {
				t.Fatal(e)
			}
		}},
		{"value directory", func(t *testing.T, dir string) {
			if e := os.Mkdir(filepath.Join(dir, "miccal.1"), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.Mkdir(filepath.Join(dir, "miccal.1/value"), 0700); e != nil {
				t.Fatal(e)
			}
		}},
		{"too many miccal", func(t *testing.T, dir string) {
			for i := 0; i < 65; i++ {
				writeTestMiccal(t, dir, i, []byte{1})
			}
		}},
		{"too many entries", func(t *testing.T, dir string) {
			for i := 0; i < 1025; i++ {
				if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f-%04d", i)), []byte(""), 0600); e != nil {
					t.Fatal(e)
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.make(t, dir)
			if _, e := discoverMiccalAt(dir, uint32(os.Geteuid())); e == nil {
				t.Fatal("malicious or excessive miccal entries accepted")
			}
		})
	}
}

func TestMiccalDoesNotExposeContents(t *testing.T) {
	dir := t.TempDir()
	contents := []byte("sensitive-private-factory-calibration-never-log")
	writeTestMiccal(t, dir, 99, contents)
	ids, e := discoverMiccalAt(dir, uint32(os.Geteuid()))
	if e != nil {
		t.Fatal(e)
	}
	if len(ids) != 1 || ids[0] != 99 {
		t.Fatal(ids)
	}
	for k, v := range aliasTargetsForMiccal(ids) {
		if strings.Contains(k, string(contents)) || strings.Contains(v, string(contents)) {
			t.Fatal("calibration leaked into aliases")
		}
	}
}
