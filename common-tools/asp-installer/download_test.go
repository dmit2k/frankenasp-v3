package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadLocalAndCleanup(t *testing.T) {
	payload := []byte("synthetic-OTA-fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer server.Close()
	p, cleanup, e := download(context.Background(), server.URL+"/update.bin", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(p)
	if e != nil || string(raw) != string(payload) {
		t.Fatalf("download %v %q", e, raw)
	}
	cleanup()
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Errorf("download not cleaned: %v", e)
	}
}
func TestDownloadRejectsTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	defer server.Close()
	dir := t.TempDir()
	_, _, e := download(context.Background(), server.URL, dir)
	if e == nil {
		t.Fatal("accepted truncated HTTP body")
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".asp-installer-ota-partial-*"))
	if len(files) > 0 {
		t.Fatal("partial OTA not cleaned")
	}
}
func TestChannelMap(t *testing.T) {
	for _, s := range []string{
		"ASP1-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,1\n",
		"ASP1-CHANNEL-MAP-V1\nmic=3,2,1,0\nref=1,0\n",
	} {
		if e := validateChannelMap([]byte(s)); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{
		"ASP1-CHANNEL-MAP-V1\nmic=0,0,2,3\nref=0,1\n",
		"ASP1-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,2\n",
		"ASP1-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,1",
		"ASP2-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,1\n",
	} {
		if validateChannelMap([]byte(s)) == nil {
			t.Fatal("accepted invalid channel map:", s)
		}
	}
}

func TestRetainedDownloadNameAndOptionalCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("firmware fixture")) }))
	defer server.Close()
	dir := t.TempDir()
	path, cleanup, err := download(context.Background(), server.URL+"/update-test.bin", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "asp-installer-ota-") || !strings.HasSuffix(path, "update-test.bin") {
		t.Fatalf("unusable retained filename: %s", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "firmware fixture" {
		t.Fatalf("retained archive inaccessible: %v", err)
	}
	// The CLI does not call cleanup by default; archive persists until owner deletes it.
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	cleanup() // Only called by the explicit delete-after-use option.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("explicit cleanup failed: %v", err)
	}
}
func TestSanitizeRetainedDownloadName(t *testing.T) {
	for _, name := range []string{"..", ".", ".hidden", ""} {
		if got := safeOTAName(name); got != "firmware.bin" {
			t.Errorf("unsafe name %q: %q", name, got)
		}
	}
	if got := safeOTAName("firmware 7080.bin"); got != "firmware_7080.bin" {
		t.Fatal(got)
	}
}
