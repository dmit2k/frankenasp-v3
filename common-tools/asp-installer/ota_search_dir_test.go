package main

import (
	"strings"
	"testing"
)

func TestOTASearchDirectoryNeverDefaultsToSharedTECHO5Parent(t *testing.T) {
	tests := []struct {
		name, cache, chosen, legacy, expected string
		fails                                 bool
	}{
		{name: "default", cache: "/data/techo5-linux/asp-installer-cache", expected: "/data/techo5-linux/asp-installer-cache/ota-images"},
		{name: "custom cache root", cache: "/mnt/cache/private", expected: "/mnt/cache/private/ota-images"},
		{name: "explicit search path", cache: "/data/techo5-linux/asp-installer-cache", chosen: "/mnt/stored-otas", expected: "/mnt/stored-otas"},
		{name: "legacy alias", cache: "/data/techo5-linux/asp-installer-cache", legacy: "/mnt/old-otas", expected: "/mnt/old-otas"},
		{name: "both options", cache: "/cache", chosen: "/tmp/a", legacy: "/tmp/b", fails: true},
		{name: "relative", cache: "/cache", chosen: "relative", fails: true},
		{name: "unsafe slash", cache: "/cache", chosen: "/", fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := resolveOTASearchDir(tc.cache, tc.chosen, tc.legacy)
			if (err != nil) != tc.fails {
				t.Fatalf("expected failure %v, got %q, %v", tc.fails, actual, err)
			}
			if !tc.fails && actual != tc.expected {
				t.Fatalf("expected %s, got %s", tc.expected, actual)
			}
			if !tc.fails && strings.HasSuffix(actual, "/techo5-linux") {
				t.Fatalf("shared TECHO5 directory used by default: %s", actual)
			}
		})
	}
}
