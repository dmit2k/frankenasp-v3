package asp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPayloadFallbackFullPartitionDigest(t *testing.T) {
	data := append(bytes.Repeat([]byte{'A'}, 4096), bytes.Repeat([]byte{'B'}, 4096)...)
	sum := sha256.Sum256(data)
	part := payloadPartition{name: "system", size: uint64(len(data)), hash: sum[:],
		operations: []payloadOperation{{kind: 0, dataLength: 4096, extents: []payloadExtent{{0, 1}}},
			{kind: 0, dataOffset: 4096, dataLength: 4096, extents: []payloadExtent{{1, 1}}}}}
	reader, err := newPayloadImage(bytes.NewReader(data), 0, uint64(len(data)), part)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 12)
	if _, err = reader.ReadAt(b, 4096); err != nil || !bytes.Equal(b, bytes.Repeat([]byte{'B'}, 12)) {
		t.Fatalf("correct fallback: %v", err)
	}
	data[5000] ^= 1
	bad, err := newPayloadImage(bytes.NewReader(data), 0, uint64(len(data)), part)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.ReadAt(b, 0); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("corruption outside requested range was accepted: %v", err)
	}
	part.hash = nil
	if _, err = newPayloadImage(bytes.NewReader(data), 0, uint64(len(data)), part); err == nil {
		t.Fatal("accepted hashless partition without per-operation hashes")
	}
}

func testZIPPayload(t *testing.T, blob []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	member, err := writer.CreateHeader(&zip.FileHeader{Name: "payload.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.Write(blob); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestPayloadCRCAndPartitionDigestBeforeCachePublish(t *testing.T) {
	src := t.TempDir()
	files := map[string][]byte{
		"build.prop":                     []byte("ro.product.device=9_review\n"),
		"bin/linker":                     runtimeELF(t, true, false),
		"lib/libasp.so":                  runtimeELF(t, true, false),
		"etc/audio-algorithms/model.bin": []byte("SYNTHETIC-RESOURCE-VALID-BYTES"),
	}
	for name, data := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	img := newExt4ImageFromTree(t, src)
	base, err := os.ReadFile(img.src.(*os.File).Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(base) > 32<<20 {
		t.Fatalf("fixture too large: %d", len(base))
	}
	hash := sha256.Sum256(base)
	info := append(pbVar(1, uint64(len(base))), pbBytes(2, hash[:])...)
	op := append(pbVar(1, 0), pbVar(3, uint64(len(base)))...)
	op = append(op, pbBytes(6, payloadTestExtent(0, uint64(len(base)/4096)))...)
	part := append(pbBytes(1, []byte("system")), pbBytes(7, info)...)
	part = append(part, pbBytes(8, op)...)
	manifest := append(pbVar(3, 4096), pbBytes(13, part)...)
	header := make([]byte, 24)
	copy(header, "CrAU")
	binary.BigEndian.PutUint64(header[4:], 2)
	binary.BigEndian.PutUint64(header[12:], uint64(len(manifest)))
	blob := append(append(header, manifest...), base...)
	corrupted := bytes.Clone(blob)
	where := bytes.Index(corrupted, files["etc/audio-algorithms/model.bin"])
	if where < 0 {
		t.Fatal("synthetic marker not found")
	}
	corrupted[where+3] ^= 0x20
	origZIP := testZIPPayload(t, blob)
	zipCRCbad := bytes.Clone(origZIP)
	crcAt := bytes.Index(zipCRCbad, files["etc/audio-algorithms/model.bin"])
	if crcAt < 0 {
		t.Fatal("ZIP marker not found")
	}
	zipCRCbad[crcAt+3] ^= 0x20
	for _, tc := range []struct {
		name      string
		data      []byte
		wantError bool
	}{
		{"valid-no-operation-hashes", origZIP, false},
		{"corrupt-ZIP-CRC", zipCRCbad, true},
		{"raw-payload-bad-partition-hash", corrupted, true},
		{"valid-raw-no-operation-hashes", blob, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.bin")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(t.TempDir(), "cache")
			if err := requireCacheDir(cache); err != nil {
				t.Fatal(err)
			}
			meter := newMeter()
			meter.Out = io.Discard
			defer meter.Close()
			result, _, _, err := loadExtracted(context.Background(), cache, "9_review", path, "", "", "", "inspect", false, false, false, meter, PreflightGenericDesktop)
			if tc.wantError {
				if err == nil {
					t.Fatal("corrupted payload accepted and published")
				}
				entries, readErr := os.ReadDir(cache)
				if readErr != nil {
					t.Fatal(readErr)
				}
				for _, entry := range entries {
					if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
						t.Fatalf("partial cache published after error: %s", entry.Name())
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("valid hashless payload rejected: %v", err)
			}
			if len(result.Items) < 3 {
				t.Fatalf("incomplete factory inventory: %d", len(result.Items))
			}
			if !bytes.Equal(result.Items["system/etc/audio-algorithms/model.bin"], files["etc/audio-algorithms/model.bin"]) {
				t.Fatal("factory asset was not preserved")
			}
		})
	}
}

func TestReleaseJSONTypedDecodingAndDuplicateKeys(t *testing.T) {
	// JSON member spelling is not a canonical wire policy. Typed decoding
	// still rejects duplicates, unknown members and invalid field types.
	for _, raw := range []string{
		`{"schema":"ASP1-ASSETS-V3","schema":"ASP1-ASSETS-V4","platform":"rook","cache_schema":"ASP1-EXTRACTED-CACHE-V2"}`,
		`{"schema":"ASP1-ASSETS-V3","platform":"rook","cache_schema":"ASP1-EXTRACTED-CACHE-V2","unexpected":true}`,
		`{"schema":7,"platform":"rook","cache_schema":"ASP1-EXTRACTED-CACHE-V2"}`,
	} {
		if _, err := parseReleaseManifest([]byte(raw)); err == nil {
			t.Errorf("accepted malformed or ambiguous JSON %s", raw)
		}
	}
	if m, err := parseReleaseManifest([]byte(`{"Schema":"ASP1-ASSETS-V3","Platform":"rook","Cache_Schema":"ASP1-EXTRACTED-CACHE-V2"}`)); err != nil || m.Platform != "rook" {
		t.Fatalf("legitimate JSON case variant rejected: %+v %v", m, err)
	}
}

func TestReleaseInterleavedPrefixCollisions(t *testing.T) {
	for _, prefix := range []string{"source", "destination"} {
		t.Run(prefix, func(t *testing.T) {
			p := syntheticPackage("9_test")
			for i, suffix := range []string{"a", "a-", "a/b"} {
				source := fmt.Sprintf("assets/resource%d", i)
				dest := fmt.Sprintf("lib/rootfs/factory/r%d", i)
				if prefix == "source" {
					source = "assets/" + suffix
				} else {
					dest = "lib/rootfs/factory/" + suffix
				}
				p.Data = append(p.Data, ReleaseResource{Path: source, InstallTo: dest, Bytes: []byte{byte(i + 1)}})
			}
			if _, err := PackV4(p); err == nil {
				t.Fatal("interleaved file/directory ancestor collision accepted")
			}
		})
	}
}

func TestRejectUnimplementedCrAUMajorOne(t *testing.T) {
	data := make([]byte, 24)
	copy(data, "CrAU")
	binary.BigEndian.PutUint64(data[4:], 1)
	filename := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, handle, err := openOTA(filename, "")
	if handle != nil {
		handle.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "major") {
		t.Fatalf("major one was not explicitly rejected: %v", err)
	}
}

func TestZIPAndExt4DepthBoundaryAndBeyond(t *testing.T) {
	for _, depth := range []int{8, 9} {
		t.Run(fmt.Sprintf("nested-dirs-%d", depth), func(t *testing.T) {
			parts := make([]string, 0, depth)
			for i := 0; i < depth; i++ {
				parts = append(parts, fmt.Sprintf("n%d", i))
			}
			rel := "etc/audio-algorithms/" + strings.Join(parts, "/") + "/model.cfg"
			tree := t.TempDir()
			full := filepath.Join(tree, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("synthetic config"), 0600); err != nil {
				t.Fatal(err)
			}
			image := newExt4ImageFromTree(t, tree)
			_, extErr := image.Files("etc/audio-algorithms")
			zipTree := syntheticZipTree(t, map[string]struct {
				Payload []byte
				Mode    os.FileMode
			}{
				"vendor/" + rel: {Payload: []byte("synthetic config")},
			})
			_, zipErr := zipTree.Files("etc/audio-algorithms")
			wantAccepted := depth <= 8
			if (extErr == nil) != wantAccepted || (zipErr == nil) != wantAccepted {
				t.Fatalf("different ZIP/ext4 depth boundary: %d, ext4=%v, ZIP=%v", depth, extErr, zipErr)
			}
		})
	}
}
