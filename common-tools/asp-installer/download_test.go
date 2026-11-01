package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
