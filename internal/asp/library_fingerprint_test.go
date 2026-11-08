package asp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryFingerprintIgnoresFirmwareResourcesAndInventoryOrder(t *testing.T) {
	base := []string{
		hashString("asp") + "  vendor/lib/libasp.so",
		hashString("linker") + "  system/bin/linker",
		hashString("libc") + "  system/lib/libc.so",
		hashString("vndk") + "  system/lib/vndk-28/libui.so",
		hashString("config") + "  system/etc/asp.conf",
		hashString("non-elf") + "  system/lib/example.so.config",
	}
	fingerprint := func(lines []string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := LibraryFingerprint(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 64 {
			t.Fatalf("want full SHA-256, got %q", got)
		}
		return got
	}
	original := fingerprint(base)
	changedMetadata := []string{
		hashString("different-build") + "  system/build.prop",
		hashString("different-vendor") + "  vendor/build.prop",
		hashString("other-public-libraries") + "  system/etc/public.libraries-AMAZON.txt",
		hashString("different-non-elf") + "  system/lib/example.so.config",
		base[3], base[2], base[1], base[0],
	}
	if got := fingerprint(changedMetadata); got != original {
		t.Fatalf("firmware resources or inventory order changed ASP library fingerprint: %s != %s", got, original)
	}
	changedLibrary := append([]string(nil), base...)
	changedLibrary[2] = hashString("different-libc") + "  system/lib/libc.so"
	if got := fingerprint(changedLibrary); got == original {
		t.Fatal("changed extracted dependency did not change fingerprint")
	}
}

func TestLibraryFingerprintRejectsMissingASPAndMalformedInventory(t *testing.T) {
	for _, content := range []string{
		hashString("linker") + "  system/bin/linker\n",
		hashString("asp") + "  ../vendor/lib/libasp.so\n",
		hashString("asp") + "  vendor/lib/libasp.so\n" + hashString("different") + "  vendor/lib/libasp.so\n",
		"garbled\n",
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := LibraryFingerprint(root); err == nil {
			t.Fatalf("accepted invalid inventory (%q): %s", content, got)
		}
	}
}
