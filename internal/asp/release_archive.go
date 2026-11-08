package asp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LoadRelease verifies a versioned archive once and keeps the verified
// payload snapshot for all downstream deployment and bundle operations.
func LoadRelease(filename, expectedPlatform string) (VerifiedRelease, error) {
	st, err := os.Lstat(filename)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxReleaseCompressed {
		return VerifiedRelease{}, errors.New("release must be a regular .tar.gz <=64MiB")
	}
	blob, err := os.ReadFile(filename)
	if err != nil {
		return VerifiedRelease{}, err
	}
	return ParseReleaseBytes(blob, filepath.Base(filename), expectedPlatform)
}

type tailValidator struct {
	Count   int64
	Nonzero bool
}

func (v *tailValidator) Write(p []byte) (int, error) {
	v.Count += int64(len(p))
	for _, b := range p {
		if b != 0 {
			v.Nonzero = true
		}
	}
	return len(p), nil
}

// ParseReleaseBytes rejects malformed framing, extra members, duplicate
// paths and content inconsistent with the single strict manifest.
func ParseReleaseBytes(raw []byte, filename, expectedPlatform string) (VerifiedRelease, error) {
	if len(raw) == 0 || len(raw) > maxReleaseCompressed {
		return VerifiedRelease{}, errors.New("invalid compressed release size")
	}
	if filename != path.Base(filename) || strings.Contains(filename, "\\") {
		return VerifiedRelease{}, errors.New("invalid release archive basename")
	}
	compressed := bytes.NewReader(raw)
	gr, err := gzip.NewReader(compressed)
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("release gzip: %w", err)
	}
	gr.Multistream(false)
	defer gr.Close()
	capped := io.LimitReader(gr, maxReleaseStream+1)
	tr := tar.NewReader(capped)
	payload := map[string][]byte{}
	directories := map[string]bool{}
	observed := map[string]bool{}
	var total int64
	count := 0
	var manifestFullName string
	for {
		h, er := tr.Next()
		if er == io.EOF {
			break
		}
		if er != nil {
			return VerifiedRelease{}, fmt.Errorf("release tar: %w", er)
		}
		count++
		if count > maxReleaseMembers {
			return VerifiedRelease{}, errors.New("release tar has too many members")
		}
		if len(h.PAXRecords) > 0 {
			return VerifiedRelease{}, errors.New("extended archive records are not permitted")
		}
		isDir := h.Typeflag == tar.TypeDir
		name := h.Name
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		if !validReleasePath(name, 224, 10) || observed[name] {
			return VerifiedRelease{}, fmt.Errorf("unsafe or duplicate tar member %q", h.Name)
		}
		observed[name] = true
		if isDir {
			if h.Size != 0 {
				return VerifiedRelease{}, fmt.Errorf("nonempty tar directory %q", name)
			}
			directories[name] = true
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return VerifiedRelease{}, fmt.Errorf("unsafe tar member type %q", h.Name)
		}
		if h.Mode&07000 != 0 || h.Size <= 0 || h.Size > maxReleaseFile {
			return VerifiedRelease{}, fmt.Errorf("invalid tar member metadata %q", name)
		}
		if strings.HasSuffix(name, "/assets/manifest.json") {
			if manifestFullName != "" {
				return VerifiedRelease{}, errors.New("multiple release manifests")
			}
			if h.Size > maxReleaseManifest {
				return VerifiedRelease{}, errors.New("oversized release manifest")
			}
			manifestFullName = name
		}
		total += h.Size
		if total > maxReleaseExpanded {
			return VerifiedRelease{}, errors.New("oversized release payload")
		}
		member, er := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if er != nil || int64(len(member)) != h.Size {
			return VerifiedRelease{}, fmt.Errorf("truncated release member %q", name)
		}
		payload[name] = member
	}
	// Consume the *whole* gzip stream, including its checksum, after tar EOF.
	// The remaining tar padding must be zero and within the expansion limit.
	tail := &tailValidator{}
	if _, err = io.Copy(tail, capped); err != nil {
		return VerifiedRelease{}, fmt.Errorf("release stream tail: %w", err)
	}
	if tail.Nonzero || total+tail.Count > maxReleaseStream {
		return VerifiedRelease{}, errors.New("nonzero trailing tar data or oversized release stream")
	}
	if compressed.Len() != 0 {
		return VerifiedRelease{}, errors.New("additional gzip member or trailing bytes")
	}
	if manifestFullName == "" {
		return VerifiedRelease{}, errors.New("missing release manifest")
	}
	manifest, err := parseReleaseManifest(payload[manifestFullName])
	if err != nil {
		return VerifiedRelease{}, fmt.Errorf("release manifest: %w", err)
	}
	root := "frankenasp-" + manifest.Platform
	prefix := root + "/"
	if manifestFullName != prefix+"assets/manifest.json" {
		return VerifiedRelease{}, errors.New("release manifest in unexpected root")
	}
	if expectedPlatform != "" && expectedPlatform != manifest.Platform {
		return VerifiedRelease{}, errors.New("release target platform mismatch")
	}
	if !strings.HasPrefix(filename, "frankenasp-v") || !strings.HasSuffix(filename, "-"+manifest.Platform+"-arm32.tar.gz") {
		return VerifiedRelease{}, errors.New("release filename and platform do not match")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(filename, "frankenasp-v"), "-"+manifest.Platform+"-arm32.tar.gz")
	if !releaseVersionPattern.MatchString(version) {
		return VerifiedRelease{}, errors.New("invalid release archive version")
	}
	files := make(map[string][]byte, len(payload))
	expectedDirs := map[string]bool{}
	for full, contents := range payload {
		if !strings.HasPrefix(full, prefix) {
			return VerifiedRelease{}, fmt.Errorf("unexpected release root for %q", full)
		}
		rel := strings.TrimPrefix(full, prefix)
		if !validReleasePath(rel, 160, 8) {
			return VerifiedRelease{}, fmt.Errorf("invalid release asset name %q", rel)
		}
		files[rel] = contents
		for p := path.Dir(full); p != "." && p != ""; p = path.Dir(p) {
			expectedDirs[p] = true
			if p == root {
				break
			}
		}
	}
	for dir := range directories {
		if !expectedDirs[dir] {
			return VerifiedRelease{}, fmt.Errorf("extraneous release directory %q", dir)
		}
	}
	if _, ok := files["assets/manifest.json"]; !ok {
		return VerifiedRelease{}, errors.New("missing release manifest payload")
	}
	return makeReleasePlan(filename, version, raw, files, manifest)
}
