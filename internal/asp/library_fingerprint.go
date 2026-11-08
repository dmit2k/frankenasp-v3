package asp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LibraryFingerprint hashes the recorded content digests of extracted ASP ELF
// libraries and the Bionic linker. Call only after ReadCache has verified the
// entire V2 inventory; this informational hash does not verify cache contents
// or imply runtime/configuration equivalence.
func LibraryFingerprint(cachePath string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(cachePath, "SHA256SUMS"))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return "", errors.New("invalid SHA256SUMS")
	}
	selected := make(map[string]string)
	foundASP := false
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " || !hexDigest.MatchString(line[:64]) || !safePath(line[66:]) {
			return "", errors.New("invalid SHA256SUMS entry")
		}
		path := line[66:]
		if !aspLibraryPath(path) {
			continue
		}
		if _, exists := selected[path]; exists {
			return "", fmt.Errorf("duplicate ASP library path %s", path)
		}
		selected[path] = line[:64]
		if strings.HasSuffix(path, "/libasp.so") {
			foundASP = true
		}
	}
	if !foundASP {
		return "", errors.New("cache has no extracted libasp.so")
	}
	paths := make([]string, 0, len(selected))
	for path := range selected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		// NUL separates the safe relative path from the 64-byte hex digest.
		// Newline terminates each pair; the encoding is unambiguous.
		_, _ = h.Write([]byte(path + "\x00" + selected[path] + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var sharedObjectName = regexp.MustCompile(`\.so(\.[0-9]+)*$`)

func aspLibraryPath(path string) bool {
	if path == "system/bin/linker" || path == "system/bin/linker64" {
		return true
	}
	for _, root := range []string{"system/lib/", "system/lib64/", "vendor/lib/", "vendor/lib64/"} {
		if strings.HasPrefix(path, root) && sharedObjectName.MatchString(filepath.Base(path)) {
			return true
		}
	}
	return false
}
