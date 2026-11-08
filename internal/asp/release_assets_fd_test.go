package asp

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPinnedAssetReadNoFDEscapeOnNestedFailureOrSuccess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deep", "resource"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("process FD inventory unavailable")
	}
	before := len(entries)
	for i := 0; i < 24; i++ {
		if b, err := pinnedAssetRead(fd, "deep/resource", 1024); err != nil || string(b) != "safe" {
			t.Fatalf("valid nested asset not read: %v", err)
		}
		for _, missing := range []string{"deep/missing", "missing/child", "deep/resource/invalid"} {
			if _, err := pinnedAssetRead(fd, missing, 1024); err == nil {
				t.Fatalf("malformed/missing asset accepted: %q", missing)
			}
		}
	}
	afterFiles, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFiles) > before+1 {
		t.Fatalf("open descriptors leaked: before=%d after=%d", before, len(afterFiles))
	}
}
