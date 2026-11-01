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
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, errors.New("OTA URL must be HTTP(S)")
	}
	if e = enoughSpace(dir, 128<<20); e != nil {
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
		if e = enoughSpace(dir, uint64(resp.ContentLength)+(64<<20)); e != nil {
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
				if e = enoughSpace(dir, 64<<20); e != nil {
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
	return filepath.Clean(p), func() { _ = os.Remove(p) }, nil
}
func redactSource(source string) string {
	if strings.Contains(source, "://") {
		return "user-provided URL"
	}
	return "user-provided local OTA"
}
