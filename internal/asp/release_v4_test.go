package asp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticARMExecutable(interpreter bool) []byte {
	b := make([]byte, 256)
	copy(b[:4], "\x7fELF")
	b[4], b[5], b[6] = 1, 1, 1
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], 40)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint32(b[28:], 52)
	binary.LittleEndian.PutUint32(b[36:], 0x05000000)
	binary.LittleEndian.PutUint16(b[42:], 32)
	count := uint16(1)
	if interpreter {
		count = 2
	}
	binary.LittleEndian.PutUint16(b[44:], count)
	ph := b[52:]
	binary.LittleEndian.PutUint32(ph[0:], 1) // PT_LOAD
	binary.LittleEndian.PutUint32(ph[4:], 0)
	binary.LittleEndian.PutUint32(ph[8:], 0)
	binary.LittleEndian.PutUint32(ph[16:], uint32(len(b)))
	if interpreter {
		prefix := "/system/bin/linker\x00"
		copy(b[180:], prefix)
		ph = b[84:]
		binary.LittleEndian.PutUint32(ph[0:], 3) // PT_INTERP
		binary.LittleEndian.PutUint32(ph[4:], 180)
		binary.LittleEndian.PutUint32(ph[16:], uint32(len(prefix)))
	}
	return b
}

func syntheticPackage(platform string) V4Package {
	return V4Package{
		Platform: platform, Version: "3.6.test",
		Runtime:   ReleaseRuntime{VendorModes: []string{"bundled"}, LibrarySearch: []string{"system/lib", "vendor/lib"}},
		Installer: syntheticARMExecutable(false), License: []byte("public test license"),
		Launcher: syntheticARMExecutable(false), Child: syntheticARMExecutable(true),
	}
}

func TestUnknownPlatformV4AndOpaqueData(t *testing.T) {
	p := syntheticPackage("9_test-model")
	p.Data = []ReleaseResource{{Path: "assets/channel-map", InstallTo: "lib/rootfs/factory/channel-map", Bytes: []byte("opaque geometry—not an Athena map")}}
	v, e := PackV4(p)
	if e != nil {
		t.Fatal(e)
	}
	if v.Schema != ReleaseSchemaV4 || v.Platform != p.Platform || len(v.Installed) != 3 {
		t.Fatalf("unexpected V4 %+v", v)
	}
	if !bytes.Equal(v.Files["assets/channel-map"], p.Data[0].Bytes) {
		t.Fatal("opaque resource changed")
	}
	again, e := ParseReleaseBytes(v.Original, v.Name, p.Platform)
	if e != nil || len(again.Installed) != 3 {
		t.Fatalf("reparse: %v", e)
	}
	if _, e := ParseReleaseBytes(v.Original, v.Name, "rook"); e == nil {
		t.Fatal("different platform accepted")
	}
}

func TestV4PlatformOwnedRequirements(t *testing.T) {
	for _, platform := range []string{"athena", "rook"} {
		p := syntheticPackage(platform)
		// Self-describing releases are not constrained by historical V3 policy.
		p.Runtime.VendorModes = []string{"bundled", "live"}
		asset, err := PackV4(p)
		if err != nil {
			t.Fatalf("%s modern capability: %v", platform, err)
		}
		if len(asset.Installed) != 2 || len(asset.Runtime.VendorModes) != 2 {
			t.Fatalf("%s release did not honor declared inventory/runtime", platform)
		}
		dir := filepath.Join(t.TempDir(), "assets")
		for name, contents := range asset.Files {
			if !strings.HasPrefix(name, "assets/") {
				continue
			}
			filename := filepath.Join(dir, strings.TrimPrefix(name, "assets/"))
			if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filename, contents, 0600); err != nil {
				t.Fatal(err)
			}
		}
		runtimePlan, err := LoadAssets(dir)
		if err != nil {
			t.Fatalf("%s modern assets rejected due to legacy policy: %v", platform, err)
		}
		if len(runtimePlan.Installed) != 2 || !runtimePlan.AllowsVendor("live") {
			t.Fatalf("%s modern runtime capabilities or inventory changed: %+v", platform, runtimePlan)
		}
	}
}

func TestV4ManifestSchemaAndPathDefenses(t *testing.T) {
	cases := []struct {
		name   string
		change func(*V4Package)
	}{
		{"uppercase codename", func(p *V4Package) { p.Platform = "9_TEST" }},
		{"no bundled", func(p *V4Package) { p.Runtime.VendorModes = []string{"live"} }},
		{"duplicate library search", func(p *V4Package) { p.Runtime.LibrarySearch = []string{"vendor/lib", "vendor/lib"} }},
		{"unsafe library search", func(p *V4Package) { p.Runtime.LibrarySearch = []string{"system/lib/../../../tmp"} }},
		{"absolute data destination", func(p *V4Package) {
			p.Data = []ReleaseResource{{Path: "assets/foo", InstallTo: "/proc/foo", Bytes: []byte("X")}}
		}},
		{"data destination collision", func(p *V4Package) {
			p.Data = []ReleaseResource{{Path: "assets/foo", InstallTo: "lib/rootfs/factory/asp1-child", Bytes: []byte("X")}}
		}},
		{"source path collision", func(p *V4Package) {
			p.Data = []ReleaseResource{
				{Path: "assets/data", InstallTo: "lib/rootfs/factory/data", Bytes: []byte("data")},
				{Path: "assets/data/child", InstallTo: "lib/rootfs/factory/data2", Bytes: []byte("child")},
			}
		}},
		{"parent escape", func(p *V4Package) {
			p.Data = []ReleaseResource{{Path: "assets/../foo", InstallTo: "lib/rootfs/factory/foo", Bytes: []byte("X")}}
		}},
		{"zero asset", func(p *V4Package) {
			p.Data = []ReleaseResource{{Path: "assets/foo", InstallTo: "lib/rootfs/factory/foo", Bytes: nil}}
		}},
		{"empty license", func(p *V4Package) { p.License = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := syntheticPackage("9_test-model")
			tc.change(&p)
			if _, e := PackV4(p); e == nil {
				t.Fatal("invalid package accepted")
			}
		})
	}
}

// rebuildTestArchive retains selected tar headers and allows intentional
// malformed payloads without including proprietary or real device artifacts.
func rebuildTestArchive(t *testing.T, orig []byte, transform func(*tar.Header, []byte) (*tar.Header, []byte, bool), extra func(*tar.Writer) error) []byte {
	t.Helper()
	gr, e := gzip.NewReader(bytes.NewReader(orig))
	if e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(gr)
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for {
		h, e := tr.Next()
		if e != nil {
			break
		}
		b := make([]byte, h.Size)
		if h.Size > 0 {
			if _, e := io.ReadFull(tr, b); e != nil {
				t.Fatal(e)
			}
		}
		nh, nb, ok := transform(h, b)
		if !ok {
			continue
		}
		nh.Size = int64(len(nb))
		if e := tw.WriteHeader(nh); e != nil {
			t.Fatal(e)
		}
		if _, e := tw.Write(nb); e != nil {
			t.Fatal(e)
		}
	}
	if extra != nil {
		if e := extra(tw); e != nil {
			t.Fatal(e)
		}
	}
	if e := tw.Close(); e != nil {
		t.Fatal(e)
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}

func TestV4ArchiveNegativeCases(t *testing.T) {
	p := syntheticPackage("9_test-model")
	p.Data = []ReleaseResource{{Path: "assets/notes.dat", InstallTo: "lib/rootfs/factory/notes.dat", Bytes: []byte("opaque")}}
	good, e := PackV4(p)
	if e != nil {
		t.Fatal(e)
	}
	identity := func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) { return h, b, true }
	cases := []struct {
		name   string
		mutate func(*tar.Header, []byte) (*tar.Header, []byte, bool)
		extra  func(*tar.Writer) error
	}{
		{"duplicate key", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "manifest.json") {
				var m ReleaseManifest
				if e := json.Unmarshal(b, &m); e != nil {
					t.Fatal(e)
				}
				b = append([]byte(`{"schema":"ASP1-ASSETS-V4","schema":"ASP1-ASSETS-V4",`), b[1:]...)
			}
			return h, b, true
		}, nil},
		{"undeclared file", identity, func(w *tar.Writer) error {
			b := []byte("X")
			h := &tar.Header{Name: "frankenasp-9_test-model/assets/extra", Size: 1, Mode: 0600}
			if e := w.WriteHeader(h); e != nil {
				return e
			}
			_, e := w.Write(b)
			return e
		}},
		{"duplicate member", identity, func(w *tar.Writer) error {
			b := []byte("X")
			h := &tar.Header{Name: "frankenasp-9_test-model/LICENSE", Size: 1, Mode: 0600}
			if e := w.WriteHeader(h); e != nil {
				return e
			}
			_, e := w.Write(b)
			return e
		}},
		{"symlink", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "assets/notes.dat") {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/etc/passwd"
				return h, nil, true
			}
			return h, b, true
		}, nil},
		{"missing asset", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "assets/notes.dat") {
				return nil, nil, false
			}
			return h, b, true
		}, nil},
		{"bad hash", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "assets/notes.dat") {
				b = []byte("changed")
			}
			return h, b, true
		}, nil},
		{"wrong root", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "assets/notes.dat") {
				h.Name = "frankenasp-other/assets/notes.dat"
			}
			return h, b, true
		}, nil},
		{"path traversal", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "assets/notes.dat") {
				h.Name = "../assets/notes.dat"
			}
			return h, b, true
		}, nil},
		{"unknown manifest key", func(h *tar.Header, b []byte) (*tar.Header, []byte, bool) {
			if strings.HasSuffix(h.Name, "manifest.json") {
				b = append([]byte(`{"unknown":123,`), b[1:]...)
			}
			return h, b, true
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := rebuildTestArchive(t, good.Original, tc.mutate, tc.extra)
			if _, e := ParseReleaseBytes(archive, good.Name, p.Platform); e == nil {
				t.Fatal("malformed archive passed")
			}
		})
	}
	double := append(bytes.Clone(good.Original), good.Original...)
	if _, e := ParseReleaseBytes(double, good.Name, p.Platform); e == nil {
		t.Fatal("second gzip member accepted")
	}
	broken := bytes.Clone(good.Original)
	broken[len(broken)-5] ^= 0xff
	if _, e := ParseReleaseBytes(broken, good.Name, p.Platform); e == nil {
		t.Fatal("bad gzip checksum accepted")
	}
}
