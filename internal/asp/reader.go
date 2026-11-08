package asp

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var pathExpr = regexp.MustCompile(`^[a-zA-Z0-9_.@+/-]+$`)

func safePath(s string) bool {
	if !pathExpr.MatchString(s) || strings.HasPrefix(s, "/") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func sortStrings(x []string) { sort.Strings(x) }

type files interface {
	Read(string) ([]byte, error)
	Exists(string) (bool, error)
	Files(string) ([]string, error)
}
type zipTree struct {
	z       *zip.Reader
	prefix  string
	members map[string]*zip.File
}

func newZipTree(z *zip.Reader, prefix string) *zipTree {
	m := make(map[string]*zip.File)
	for _, f := range z.File {
		m[f.Name] = f
	}
	return &zipTree{z, prefix, m}
}
func (t *zipTree) name(p string) string { return t.prefix + "/" + p }
func (t *zipTree) Exists(p string) (bool, error) {
	if _, ok := t.members[t.name(p)]; ok {
		return true, nil
	}
	// ZIP archives may omit explicit directory records. Discover directories
	// from child entries so config collection works for both ZIP layouts.
	prefix := t.name(p) + "/"
	for name := range t.members {
		if strings.HasPrefix(name, prefix) {
			return true, nil
		}
	}
	return false, nil
}
func (t *zipTree) Read(p string) ([]byte, error) {
	if !safePath(p) {
		return nil, errors.New("unsafe path")
	}
	f, ok := t.members[t.name(p)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissing, p)
	}
	if f.UncompressedSize64 > 64<<20 || f.UncompressedSize64 == 0 {
		return nil, errors.New("file too large/empty")
	}
	r, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, 64<<20+1))
	if e != nil {
		return nil, e
	}
	if uint64(len(b)) != f.UncompressedSize64 {
		return nil, errors.New("ZIP size mismatch")
	}
	return b, nil
}

// Files traverses complete nested audio-algorithms resources (including ZIP
// implicit directories) with bounds independent of archive entry count.
func (t *zipTree) Files(p string) ([]string, error) {
	if !safePath(p) {
		return nil, errors.New("unsafe ZIP config root")
	}
	var out []string
	seen := map[string]bool{}
	dirs := map[string]bool{}
	prefix := t.name(p) + "/"
	entries := 0
	for _, f := range t.z.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		entries++
		if entries > 4096 {
			return nil, errors.New("ZIP audio directory exceeds entry bound")
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		isDir := f.FileInfo().IsDir()
		rel = strings.TrimSuffix(rel, "/")
		if !safePath(rel) {
			return nil, errors.New("unsafe ZIP audio entry")
		}
		if seen[rel] {
			return nil, errors.New("duplicate ZIP audio entry")
		}
		seen[rel] = true
		parts := strings.Split(rel, "/")
		if len(parts) > 8+map[bool]int{true: 0, false: 1}[isDir] || len(p)+1+len(rel) > 240 {
			return nil, errors.New("ZIP audio resource exceeds path depth/length")
		}
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
		if isDir {
			dirs[rel] = true
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, errors.New("unsafe ZIP audio symlink/special entry")
		}
		if len(dirs) > 1024 {
			return nil, errors.New("ZIP audio directory limit exceeded")
		}
		out = append(out, p+"/"+rel)
		if len(out) > 512 {
			return nil, errors.New("ZIP config file count exceeded")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("missing or empty ZIP audio directory")
	}
	if len(dirs) > 1024 {
		return nil, errors.New("ZIP audio directory limit exceeded")
	}
	sort.Strings(out)
	return out, nil
}
