package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type testRoot struct{ path string }

func (r testRoot) Read(name string) ([]byte, error) {
	if !safePath(name) {
		return nil, errors.New("unsafe path")
	}
	return os.ReadFile(filepath.Join(r.path, filepath.FromSlash(name)))
}
func (r testRoot) Exists(name string) (bool, error) {
	if !safePath(name) {
		return false, errors.New("unsafe path")
	}
	_, e := os.Stat(filepath.Join(r.path, filepath.FromSlash(name)))
	return e == nil, nil
}
func (r testRoot) Files(name string) ([]string, error) {
	if !safePath(name) {
		return nil, errors.New("unsafe path")
	}
	entries, e := os.ReadDir(filepath.Join(r.path, filepath.FromSlash(name)))
	if e != nil {
		return nil, e
	}
	var out []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		out = append(out, name+"/"+entry.Name())
	}
	sort.Strings(out)
	return out, nil
}
func TestPrivateFactoryClosure(t *testing.T) {
	for _, platform := range []string{"athena", "rook"} {
		t.Run(platform, func(t *testing.T) {
			root := os.Getenv("ASP_TEST_" + platform + "_ROOT")
			child := os.Getenv("ASP_TEST_" + platform + "_CHILD")
			if root == "" || child == "" {
				t.Skip("owner-provided private runtime/root and compiled V3 child required")
			}
			raw, e := os.ReadFile(child)
			if e != nil {
				t.Fatal(e)
			}
			sources := &otaSources{platform: platform, system: testRoot{filepath.Join(root, "system")}, vendor: testRoot{filepath.Join(root, "vendor")}}
			c := newCollector(sources)
			if e = c.closure(platform, raw); e != nil {
				t.Fatal(e)
			}
			if len(c.items) < 4 {
				t.Fatal("unreasonably small closure")
			}
			t.Log(fmt.Sprintf("%s: %d source files, %d dependency edges", platform, len(c.items), c.edgeCount))
		})
	}
}
