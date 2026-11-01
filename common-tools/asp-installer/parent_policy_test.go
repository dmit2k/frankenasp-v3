package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The installer must not assert any ownership or permission policy for the
// TECHO5/Android parent. These are writable test-only directories; only the
// installer-owned lock object gets validated.
func TestInstallerLockIgnoresParentDirectoryPolicy(t *testing.T) {
	for _, mode := range []os.FileMode{0700, 0755, 0770, 0777, 01777} {
		t.Run(mode.String(), func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, mode); err != nil {
				t.Fatal(err)
			}
			called := false
			if err := withInstallerLockAt(parent, func() error { called = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("lock callback not executed")
			}
			// The installer is forbidden from correcting TECHO5-managed permissions.
			st, err := os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != mode.Perm() || st.Mode()&os.ModeSticky != mode&os.ModeSticky {
				t.Fatalf("parent mode changed: %s != %s", st.Mode(), mode)
			}
		})
	}
}
func TestInstallerLockIgnoresForeignParentOwnershipWhenRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can create a foreign-owned test directory")
	}
	parent := t.TempDir()
	if err := os.Chown(parent, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	if err := withInstallerLockAt(parent, func() error { return nil }); err != nil {
		t.Fatalf("foreign-owned parent was rejected: %v", err)
	}
	st, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0777 {
		t.Fatal("installer modified parent mode")
	}
}
func TestInstallerLockValidatesOnlyOwnedLockFile(t *testing.T) {
	for _, kind := range []string{"symlink", "world-writable", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			path := filepath.Join(parent, ".asp-installer-v3.lock")
			switch kind {
			case "symlink":
				if err := os.Symlink("/dev/null", path); err != nil {
					t.Fatal(err)
				}
			case "world-writable":
				if err := os.WriteFile(path, []byte(""), 0666); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				other := filepath.Join(parent, "other")
				if err := os.WriteFile(other, []byte(""), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(other, path); err != nil {
					t.Fatal(err)
				}
			}
			invoked := false
			err := withInstallerLockAt(parent, func() error { invoked = true; return nil })
			if err == nil || invoked {
				t.Fatal("untrusted installer-owned lock was accepted")
			}
			if !strings.Contains(err.Error(), "untrusted installer lock file") && !errors.Is(err, os.ErrPermission) {
				// A rejected O_NOFOLLOW symlink may return ELOOP, which is acceptable.
				if kind != "symlink" {
					t.Fatalf("unexpected lock rejection: %v", err)
				}
			}
		})
	}
}
