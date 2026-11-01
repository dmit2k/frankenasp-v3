package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadPreventsStartingWhenRuntimeSpaceUnavailable(t *testing.T) {
	dir := t.TempDir()
	snap, e := diskAt(dir)
	if e != nil {
		t.Fatal(e)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(200)
	}))
	defer server.Close()
	_, _, e = downloadWithReserve(context.Background(), server.URL, dir, snap.Available)
	if e == nil || !strings.Contains(e.Error(), "insufficient space") {
		t.Fatalf("download started without required spare space: %v", e)
	}
	if requests.Load() != 0 {
		t.Fatal("OTA HTTP GET was made despite failed initial disk preflight")
	}
}
func TestDownloadChecksContentLengthBeforeBody(t *testing.T) {
	dir := t.TempDir()
	snap, e := diskAt(dir)
	if e != nil {
		t.Fatal(e)
	}
	if snap.Available < 300<<20 {
		t.Skip("test needs >300 MiB temporary free space")
	}
	reserve := snap.Available - (200 << 20)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(180<<20))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	_, _, e = downloadWithReserve(context.Background(), server.URL, dir, reserve)
	if e == nil || !strings.Contains(e.Error(), "insufficient space") {
		t.Fatalf("Content-Length budget did not reject download: %v", e)
	}
	if requests.Load() != 1 {
		t.Fatal("HTTP headers were not evaluated")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".asp-installer-ota-partial-") {
			t.Fatalf("left temporary OTA: %s", filepath.Join(dir, entry.Name()))
		}
	}
}
