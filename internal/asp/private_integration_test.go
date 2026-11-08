package asp

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
			if root == "" {
				t.Skip("owner-provided private runtime root required")
			}
			sources := &otaSources{platform: platform, system: testRoot{filepath.Join(root, "system")}, vendor: testRoot{filepath.Join(root, "vendor")}}
			c := newCollector(sources)
			if e := c.closure(); e != nil {
				t.Fatal(e)
			}
			if len(c.items) < 4 {
				t.Fatal("unreasonably small closure")
			}
			t.Log(fmt.Sprintf("%s: %d source files, %d dependency edges", platform, len(c.items), c.edgeCount))
		})
	}
}

// This optional test uses owner-provided extracted factory files without
// requiring proprietary artifacts in the public repository.
func TestPrivateRealFactorySourceDedup(t *testing.T) {
	for _, platform := range []string{"athena", "rook"} {
		t.Run(platform, func(t *testing.T) {
			path := os.Getenv("ASP_TEST_" + platform + "_CACHE")
			if path == "" {
				t.Skip("private V2 factory cache not specified")
			}
			actual, err := readCache(path)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Join(t.TempDir(), "asp-installer-cache")
			if err := requireCacheDir(base); err != nil {
				t.Fatal(err)
			}
			ota := actual.Metadata
			ota.SourceSHA256 = hashString("simulated OTA input for " + platform)
			ota.URLSHA256 = ""
			ota.OTAFormat = "ZIP_FULL_BLOCK_v4"
			first, err := publishCache(base, ota, actual.Items, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			backup := ota
			backup.SourceSHA256 = hashString("simulated backup input for " + platform)
			backup.OTAFormat = "BACKUP_RAW_EXT4"
			backup.DependencyEdges++
			second, err := publishCache(base, backup, actual.Items, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !second.Hit || second.Path != first.Path {
				t.Fatalf("duplicates across source formats: %s vs %s", first.Path, second.Path)
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(first.Path) {
				t.Fatalf("unexpected factory cache layout: %+v", entries)
			}
			t.Logf("%s: deduplicated %d real factory files across OTA/backup identities", platform, len(actual.Items))
		})
	}
}
