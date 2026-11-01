package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func enoughSpace(dir string, required uint64) error {
	var s unix.Statfs_t
	if e := unix.Statfs(dir, &s); e != nil {
		return e
	}
	free := s.Bavail * uint64(s.Bsize)
	if free < required {
		return fmt.Errorf("insufficient space: %d MiB free, require %d MiB", free>>20, required>>20)
	}
	return nil
}
func download(ctx context.Context, raw, dir string) (string, func(), error) {
	return downloadWithReserve(ctx, raw, dir, 0)
}

// extraReserve protects staged runtime and backup capacity on the same filesystem.
// OTA data must never consume that space while downloading.
func downloadWithReserve(ctx context.Context, raw, dir string, extraReserve uint64) (string, func(), error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, errors.New("OTA URL must be HTTP(S)")
	}
	initial, e := sumSpace(128<<20, extraReserve)
	if e != nil {
		return "", nil, e
	}
	if e = enoughSpace(dir, initial); e != nil {
		return "", nil, e
	}
	file, e := os.CreateTemp(dir, ".asp-installer-ota-partial-")
	if e != nil {
		return "", nil, e
	}
	p := file.Name()
	cleanup := func() { file.Close(); os.Remove(p) }
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, Proxy: nil, MaxIdleConns: 1, DisableCompression: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 90 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many OTA redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("non-HTTP OTA redirect")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		cleanup()
		return "", nil, e
	}
	resp, e := client.Do(req)
	if e != nil {
		cleanup()
		return "", nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		cleanup()
		return "", nil, fmt.Errorf("OTA HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > 2<<30 {
		cleanup()
		return "", nil, errors.New("OTA exceeds 2GiB")
	}
	if resp.ContentLength > 0 {
		need, err := sumSpace(uint64(resp.ContentLength), 64<<20, extraReserve)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if e = enoughSpace(dir, need); e != nil {
			cleanup()
			return "", nil, e
		}
	}
	var n int64
	buf := make([]byte, 256<<10)
	for {
		count, e := resp.Body.Read(buf)
		if count > 0 {
			n += int64(count)
			if n > 2<<30 {
				cleanup()
				return "", nil, errors.New("OTA exceeded 2GiB")
			}
			if n%(16<<20) < int64(count) {
				need, err := sumSpace(64<<20, extraReserve)
				if err != nil {
					cleanup()
					return "", nil, err
				}
				if e = enoughSpace(dir, need); e != nil {
					cleanup()
					return "", nil, e
				}
			}
			if _, w := file.Write(buf[:count]); w != nil {
				cleanup()
				return "", nil, w
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			cleanup()
			return "", nil, e
		}
	}
	if n == 0 || (resp.ContentLength >= 0 && resp.ContentLength != n) {
		cleanup()
		return "", nil, errors.New("OTA download truncated")
	}
	if e = file.Sync(); e != nil {
		cleanup()
		return "", nil, e
	}
	if e = file.Close(); e != nil {
		cleanup()
		return "", nil, e
	}
	// Complete downloads are retained by default, with an owner-visible safe filename.
	// A random suffix prevents replacing existing OTA archives of the same release.
	name := safeOTAName(path.Base(u.Path))
	final := filepath.Join(dir, "asp-installer-ota-"+nonce()+"-"+name)
	if e = unix.Renameat2(unix.AT_FDCWD, p, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); e != nil {
		cleanup()
		return "", nil, fmt.Errorf("retain downloaded OTA: %w", e)
	}
	return filepath.Clean(final), func() { _ = os.Remove(final) }, nil
}
func safeOTAName(original string) string {
	if len(original) > 100 {
		original = original[len(original)-100:]
	}
	var b strings.Builder
	for _, r := range original {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	result := b.String()
	if result == "" || result == "." || result == ".." || strings.HasPrefix(result, ".") {
		return "firmware.bin"
	}
	return result
}
func redactSource(source string) string {
	if strings.Contains(source, "://") {
		return "user-provided URL"
	}
	return "user-provided local OTA"
}
