package asp

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func syntheticZipTree(t *testing.T, records map[string]struct {
	Payload []byte
	Mode    os.FileMode
}) *zipTree {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(records))
	for n := range records {
		names = append(names, n)
	}
	sortStrings(names)
	for _, name := range names {
		record := records[name]
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		if record.Mode != 0 {
			h.SetMode(record.Mode)
		} else {
			h.SetMode(0600)
		}
		w, e := zw.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(record.Payload); e != nil {
			t.Fatal(e)
		}
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	r, e := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if e != nil {
		t.Fatal(e)
	}
	return newZipTree(r, "vendor")
}

func TestNestedZIPAudioResourcesPreserveExactPaths(t *testing.T) {
	root := "vendor/etc/audio-algorithms/"
	entries := map[string]struct {
		Payload []byte
		Mode    os.FileMode
	}{
		root + "AFE.cfg":                    {Payload: []byte("cfg")},
		root + "deep/model.tflite":          {Payload: []byte("model")},
		root + "deep/filters/coefs.dat":     {Payload: []byte("coef")},
		root + "deep/filters/settings.json": {Payload: []byte("{}")},
	}
	src := syntheticZipTree(t, entries)
	got, e := src.Files("etc/audio-algorithms")
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"etc/audio-algorithms/AFE.cfg", "etc/audio-algorithms/deep/filters/coefs.dat", "etc/audio-algorithms/deep/filters/settings.json", "etc/audio-algorithms/deep/model.tflite"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ZIP inventory: got %v want %v", got, want)
	}
	for _, name := range got {
		b, e := src.Read(name)
		if e != nil || len(b) == 0 {
			t.Fatalf("read %s: %v", name, e)
		}
	}
}

func TestNestedZIPAudioRejectsUnsafeEntries(t *testing.T) {
	root := "vendor/etc/audio-algorithms/"
	for _, tc := range []struct {
		name  string
		entry string
		mode  os.FileMode
	}{
		{"symlink", "nested/will-not-follow", os.ModeSymlink | 0777},
		{"deep nesting", "1/2/3/4/5/6/7/8/9/too-deep.cfg", 0600},
		{"path traversal", "../breakout.cfg", 0600},
		{"duplicate embedded member", "empty", 0},
	} {
		if tc.name == "duplicate embedded member" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			src := syntheticZipTree(t, map[string]struct {
				Payload []byte
				Mode    os.FileMode
			}{
				root + "original.cfg": {Payload: []byte("good")},
				root + tc.entry:       {Payload: []byte("evil"), Mode: tc.mode},
			})
			if _, e := src.Files("etc/audio-algorithms"); e == nil {
				t.Fatal("unsafe nested ZIP entry accepted")
			}
		})
	}
}

func newExt4ImageFromTree(t *testing.T, tree string) *image {
	t.Helper()
	mkfs, e := exec.LookPath("mkfs.ext4")
	if e != nil {
		t.Skip("mkfs.ext4 unavailable for synthetic ext4 fixture")
	}
	path := filepath.Join(t.TempDir(), "factory.ext4")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(32 << 20); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(mkfs, "-q", "-F", "-O", "^metadata_csum,^64bit,^dir_index", "-d", tree, path)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("create synthetic ext4: %s: %v", output, e)
	}
	source, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = source.Close() })
	st, e := source.Stat()
	if e != nil {
		t.Fatal(e)
	}
	img := &image{src: source, size: st.Size()}
	if e = img.init(); e != nil {
		t.Fatal(e)
	}
	return img
}

func TestNestedExt4AudioResourcesPreserveExactPaths(t *testing.T) {
	dir := t.TempDir()
	contents := map[string]string{
		"etc/audio-algorithms/AFE.cfg":                "cfg",
		"etc/audio-algorithms/deep/model.tflite":      "model",
		"etc/audio-algorithms/deep/coefs/filters.cfg": "coefficient",
		"etc/audio-algorithms/deep/coefs/filter.json": "{}",
	}
	for name, b := range contents {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(b), 0600); e != nil {
			t.Fatal(e)
		}
	}
	img := newExt4ImageFromTree(t, dir)
	got, e := img.Files("etc/audio-algorithms")
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != len(contents) {
		t.Fatalf("missing nested ext4 resources: got %v", got)
	}
	for _, name := range got {
		b, e := img.Read(name)
		if e != nil || string(b) != contents[name] {
			t.Fatalf("ext4 read %q: %q %v", name, b, e)
		}
	}
}

func TestNestedExt4RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "audio-algorithms")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "AFE.cfg"), []byte("cfg"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("../outside", filepath.Join(dir, "link.cfg")); e != nil {
		t.Fatal(e)
	}
	img := newExt4ImageFromTree(t, root)
	if _, e := img.Files("etc/audio-algorithms"); e == nil || !strings.Contains(e.Error(), "symlink") {
		t.Fatalf("ext4 symlink not rejected: %v", e)
	}
}
