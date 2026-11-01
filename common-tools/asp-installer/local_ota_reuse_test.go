package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadedLocalOTAReusedWithoutHTTP(t *testing.T) {
	base, cached := fixtureCache(t)
	dir := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "network unexpectedly contacted", http.StatusTeapot)
	}))
	defer server.Close()
	sourceURL := server.URL + "/update-test.bin"
	local := filepath.Join(dir, "asp-installer-ota-012345abcdef-update-test.bin")
	if e := os.WriteFile(local, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	var log bytes.Buffer
	meter := &progressMeter{out: &log, done: make(chan struct{})}
	got, downloaded, cleanup, e := loadExtracted(context.Background(), base, "athena", "", sourceURL, "", dir, "inspect", []byte("fake helper"), false, false, false, meter)
	meter.Close()
	if e != nil || !got.Hit || got.Path != cached.Path || downloaded != "" || cleanup != nil {
		t.Fatalf("failed local cache reuse: hit=%t err=%v", got.Hit, e)
	}
	if requests.Load() != 0 {
		t.Fatalf("unexpected %d HTTP requests", requests.Load())
	}
	if _, e = os.Stat(local); e != nil {
		t.Fatal("saved OTA was removed")
	}
	if !strings.Contains(log.String(), "Reusing saved local OTA") {
		t.Fatal(log.String())
	}
}

func TestLocalOTAWithExactRemoteBasenameReused(t *testing.T) {
	base, _ := fixtureCache(t)
	dir := t.TempDir()
	local := filepath.Join(dir, "factory-update.bin")
	if e := os.WriteFile(local, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	result, _, e := cachedLocalOTAForURL(context.Background(), base, dir, "athena", hashString("fake helper"), "https://example.invalid/factory-update.bin", meter)
	meter.Close()
	if e != nil || !result.Hit {
		t.Fatalf("explicit local filename ignored: %v", e)
	}
}

func TestDifferentLocalOTANeverMasqueradesAsURLCache(t *testing.T) {
	base, _ := fixtureCache(t)
	dir := t.TempDir()
	local := filepath.Join(dir, "asp-installer-ota-abcdef123456-update-test.bin")
	if e := os.WriteFile(local, []byte("wrong firmware bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "expected fallback", http.StatusBadGateway)
	}))
	defer server.Close()
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	_, _, _, e := loadExtracted(context.Background(), base, "athena", "", server.URL+"/update-test.bin", "", dir, "inspect", []byte("fake helper"), false, false, false, meter)
	meter.Close()
	if e == nil || !strings.Contains(e.Error(), "OTA HTTP status 502") {
		t.Fatalf("wrong local file did not fall back to network: %v", e)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one fallback GET, got %d", calls.Load())
	}
	if b, e := os.ReadFile(local); e != nil || string(b) != "wrong firmware bytes" {
		t.Fatal("download modified existing local OTA")
	}
}

func TestBadLocalOTACandidatesIgnored(t *testing.T) {
	base, _ := fixtureCache(t)
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.bin")
	if e := os.WriteFile(outside, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "asp-installer-ota-012345abcdef-update-test.bin")
	if e := os.Symlink(outside, link); e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(dir, "asp-installer-ota-111111111111-update-test.bin")
	if e := os.WriteFile(name, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(name, filepath.Join(dir, "other-link.bin")); e != nil {
		t.Fatal(e)
	}
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	_, _, e := cachedLocalOTAForURL(context.Background(), base, dir, "athena", hashString("fake helper"), "http://example.invalid/update-test.bin", meter)
	meter.Close()
	if !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("unsafe candidate accepted: %v", e)
	}
}
func TestUnrelatedLocalOTANamesAreNotConsidered(t *testing.T) {
	for _, tc := range []struct {
		name string
		yes  bool
	}{
		{"asp-installer-ota-a1b2c3d4e5f6-update.bin", true},
		{"update.bin", true},
		{"asp-installer-ota-a1b2c3d4e5f6-other.bin", false},
		{"asp-installer-ota-abcdef-update.bin", false},
		{"update.bin.old", false},
		{"asp-installer-ota-xxxxxxxxxxxx-update.bin", false},
	} {
		if got := localOTACandidateName(tc.name, "update.bin"); got != tc.yes {
			t.Errorf("%s expected=%v got=%v", tc.name, tc.yes, got)
		}
	}
}
func TestLocalOTACacheIdentityIncludesHelper(t *testing.T) {
	base, _ := fixtureCache(t)
	dir := t.TempDir()
	name := filepath.Join(dir, "update-test.bin")
	if e := os.WriteFile(name, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	_, _, e := cachedLocalOTAForURL(context.Background(), base, dir, "athena", hashString("other helper"), "https://example.invalid/update-test.bin", meter)
	meter.Close()
	if !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("incorrect helper accepted: %v", e)
	}
}

func TestRefreshCacheForcesNetworkEvenWithLocalAndCache(t *testing.T) {
	base, _ := fixtureCache(t)
	dir := t.TempDir()
	name := filepath.Join(dir, "update-test.bin")
	if e := os.WriteFile(name, []byte("fake OTA contents"), 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "deliberate refresh request", http.StatusBadGateway)
	}))
	defer server.Close()
	meter := &progressMeter{out: &bytes.Buffer{}, done: make(chan struct{})}
	_, _, _, e := loadExtracted(context.Background(), base, "athena", "", server.URL+"/update-test.bin", "", dir, "inspect", []byte("fake helper"), false, true, false, meter)
	meter.Close()
	if e == nil || !strings.Contains(e.Error(), "OTA HTTP status 502") {
		t.Fatalf("refresh did not perform network request: %v", e)
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh GET count=%d", calls.Load())
	}
}
