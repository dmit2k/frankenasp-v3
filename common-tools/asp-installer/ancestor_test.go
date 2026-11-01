package main

import (
	"os"
	"testing"
)

func TestAndroidDataAncestorTrustBoundary(t *testing.T) {
	tests := []struct {
		name, path string
		mode       os.FileMode
		uid, gid   uint32
		allowed    bool
	}{
		{"spot android data", "/data", os.ModeDir | 0771, 1000, 1000, true},
		{"root owned android data", "/data", os.ModeDir | 0777, 0, 0, true},
		{"root owned parent", "/data/techo5-linux", os.ModeDir | 0777, 0, 0, true},
		{"unrelated system-owned path", "/data/techo5-linux", os.ModeDir | 0771, 1000, 1000, false},
		{"other system-owned ancestor", "/mnt/data", os.ModeDir | 0771, 1000, 1000, false},
		{"non-system uid", "/data", os.ModeDir | 0771, 1001, 1000, false},
		{"non-system gid", "/data", os.ModeDir | 0771, 1000, 1001, false},
		{"world writable system directory", "/data", os.ModeDir | 0777, 1000, 1000, false},
		{"system sticky data", "/data", os.ModeDir | os.ModeSticky | 0771, 1000, 1000, false},
		{"system group write extra", "/data", os.ModeDir | 0775, 1000, 1000, false},
		{"system other mode", "/data", os.ModeDir | 0711, 1000, 1000, false},
		{"symlink system-owned", "/data", os.ModeSymlink | 0771, 1000, 1000, false},
		{"regular file root-owned", "/data", 0644, 0, 0, false},
		{"unknown privileged group", "/data", os.ModeDir | 0771, 2000, 1000, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := trustedDeploymentAncestor(tc.path, tc.mode, tc.uid, tc.gid)
			if actual != tc.allowed {
				t.Fatalf("trustedDeploymentAncestor(%q, %s, %d, %d) = %t; expected %t", tc.path, tc.mode, tc.uid, tc.gid, actual, tc.allowed)
			}
		})
	}
}
