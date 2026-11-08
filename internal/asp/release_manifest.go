package asp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/dmit2k/frankenasp-v3/platforms"
)

const (
	ReleaseSchemaV3      = "ASP1-ASSETS-V3"
	ReleaseSchemaV4      = "ASP1-ASSETS-V4"
	maxReleaseCompressed = 64 << 20
	maxReleaseExpanded   = 64 << 20
	maxReleaseStream     = 66 << 20
	maxReleaseFile       = 32 << 20
	maxReleaseManifest   = 64 << 10
	maxReleaseData       = 1 << 20
	maxReleaseMembers    = 256
	maxReleaseFiles      = 64
)

var releaseVersionPattern = regexp.MustCompile(`^[0-9][A-Za-z0-9._-]{0,63}$`)
var wirePlatformPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,47}$`)
var releasePathPattern = regexp.MustCompile(`^[A-Za-z0-9_.@+/-]+$`)

type ReleaseRuntime struct {
	VendorModes   []string `json:"vendor_modes"`
	LibrarySearch []string `json:"library_search"`
}
type ReleaseFile struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	InstallTo string `json:"install_to,omitempty"`
}
type ReleaseManifest struct {
	Schema      string         `json:"schema"`
	Platform    string         `json:"platform"`
	Version     string         `json:"version"`
	CacheSchema string         `json:"cache_schema"`
	Arch        string         `json:"arch"`
	Runtime     ReleaseRuntime `json:"runtime"`
	Files       []ReleaseFile  `json:"files"`
}

// VerifiedRelease contains the source archive bytes and payloads validated as
// one immutable-by-convention snapshot. Callers must never reopen a member
// after comparing its SHA-256 digest.
type VerifiedRelease struct {
	Name        string
	Version     string
	Platform    string
	CacheSchema string
	Schema      string
	Original    []byte
	Files       map[string][]byte
	Manifest    ReleaseManifest
	Runtime     ReleaseRuntime
	Installed   []ReleaseFile
}

func validReleasePath(p string, maxBytes, maxParts int) bool {
	if len(p) == 0 || len(p) > maxBytes || strings.HasPrefix(p, "/") || !releasePathPattern.MatchString(p) {
		return false
	}
	parts := strings.Split(p, "/")
	if len(parts) > maxParts {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func verifyUniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return errors.New("JSON nesting depth exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				t, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := t.(string)
				if !ok {
					return errors.New("non-string JSON key")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			t, e := d.Token()
			if e != nil {
				return e
			}
			if t != json.Delim('}') {
				return errors.New("invalid JSON object ending")
			}
		case '[':
			for d.More() {
				if e := walk(depth + 1); e != nil {
					return e
				}
			}
			t, e := d.Token()
			if e != nil {
				return e
			}
			if t != json.Delim(']') {
				return errors.New("invalid JSON array ending")
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

// DecodeStrictJSON applies the same bounded unknown-field/duplicate-key
// policy to platform-owned packaging contracts and release manifests.
func DecodeStrictJSON(raw []byte, dst any) error { return strictJSON(raw, dst) }

func strictJSON(raw []byte, dst any) error {
	if len(raw) == 0 || len(raw) > maxReleaseManifest {
		return errors.New("invalid release manifest length")
	}
	if err := verifyUniqueJSON(raw); err != nil {
		return err
	}
	return decodeJSONFields(raw, dst)
}

func decodeJSONFields(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		if err == nil {
			return errors.New("trailing release manifest value")
		}
		return err
	}
	return nil
}

func parseReleaseManifest(raw []byte) (ReleaseManifest, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if len(raw) == 0 || len(raw) > maxReleaseManifest {
		return ReleaseManifest{}, errors.New("invalid release manifest length")
	}
	if err := verifyUniqueJSON(raw); err != nil {
		return ReleaseManifest{}, err
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return ReleaseManifest{}, err
	}
	var m ReleaseManifest
	switch header.Schema {
	case ReleaseSchemaV3:
		var v3 struct {
			Schema      string `json:"schema"`
			Platform    string `json:"platform"`
			CacheSchema string `json:"cache_schema"`
		}
		if err := decodeJSONFields(raw, &v3); err != nil {
			return m, err
		}
		m = ReleaseManifest{Schema: v3.Schema, Platform: v3.Platform, CacheSchema: v3.CacheSchema}
	case ReleaseSchemaV4:
		if err := decodeJSONFields(raw, &m); err != nil {
			return m, err
		}
	default:
		return m, fmt.Errorf("unknown release schema %q", header.Schema)
	}
	if !wirePlatformPattern.MatchString(m.Platform) {
		return m, errors.New("invalid canonical release platform")
	}
	if m.CacheSchema != cacheSchema {
		return m, fmt.Errorf("incompatible release cache schema %q", m.CacheSchema)
	}
	if m.Schema == ReleaseSchemaV4 {
		if !releaseVersionPattern.MatchString(m.Version) || m.Arch != "arm32" {
			return m, errors.New("invalid V4 release version or architecture")
		}
		if err := validateRuntime(m.Runtime); err != nil {
			return m, err
		}
		if err := validateInventory(m.Files); err != nil {
			return m, err
		}
	}
	return m, nil
}

func validateRuntime(r ReleaseRuntime) error {
	if len(r.VendorModes) == 0 || len(r.VendorModes) > 2 {
		return errors.New("invalid vendor mode capability list")
	}
	found := map[string]bool{}
	for _, mode := range r.VendorModes {
		if mode != "bundled" && mode != "live" || found[mode] {
			return errors.New("invalid or repeated vendor mode")
		}
		found[mode] = true
	}
	if !found["bundled"] {
		return errors.New("bundled vendor capability is mandatory")
	}
	if len(r.LibrarySearch) == 0 || len(r.LibrarySearch) > 16 {
		return errors.New("invalid runtime library search list")
	}
	seen := map[string]bool{}
	for _, dir := range r.LibrarySearch {
		if !validReleasePath(dir, 160, 4) || !(dir == "system/lib" || dir == "vendor/lib" || strings.HasPrefix(dir, "system/lib/") || strings.HasPrefix(dir, "vendor/lib/")) || seen[dir] {
			return fmt.Errorf("invalid or duplicated runtime library path %q", dir)
		}
		seen[dir] = true
	}
	return nil
}

func validateInventory(entries []ReleaseFile) error {
	if len(entries) < 4 || len(entries) > maxReleaseFiles {
		return errors.New("invalid release inventory length")
	}
	roleNames := map[string]string{
		"installer": "asp-installer", "license": "LICENSE",
		"launcher": "assets/external-asp", "child": "assets/asp1-child",
	}
	roleDests := map[string]string{
		"launcher": "bin/external-asp", "child": "lib/rootfs/factory/asp1-child",
	}
	seenRole := map[string]bool{}
	seenPath := map[string]bool{}
	var destinations []string
	for _, f := range entries {
		if !validReleasePath(f.Path, 160, 8) || f.Path == "assets/manifest.json" || seenPath[f.Path] {
			return fmt.Errorf("invalid or duplicated release file path %q", f.Path)
		}
		seenPath[f.Path] = true
		if f.Size <= 0 || f.Size > maxReleaseFile {
			return fmt.Errorf("invalid release file size for %q", f.Path)
		}
		if f.Role == "data" {
			if f.Size > maxReleaseData || !strings.HasPrefix(f.Path, "assets/") || !strings.HasPrefix(f.InstallTo, "lib/rootfs/factory/") || f.InstallTo == "lib/rootfs/factory/asp1-child" {
				return fmt.Errorf("invalid data asset %q", f.Path)
			}
		} else {
			expected, ok := roleNames[f.Role]
			if !ok || f.Path != expected || seenRole[f.Role] {
				return fmt.Errorf("invalid release role for %q", f.Path)
			}
			seenRole[f.Role] = true
			if f.InstallTo != roleDests[f.Role] {
				return fmt.Errorf("wrong destination for %q", f.Path)
			}
		}
		if f.InstallTo != "" {
			if !validReleasePath(f.InstallTo, 160, 8) {
				return fmt.Errorf("unsafe installation target %q", f.InstallTo)
			}
			if f.InstallTo == "lib/rootfs/factory" || f.InstallTo == "lib/rootfs/factory/." {
				return errors.New("data may not replace factory root")
			}
			destinations = append(destinations, f.InstallTo)
		}
		if len(f.SHA256) != 64 || strings.ToLower(f.SHA256) != f.SHA256 {
			return fmt.Errorf("noncanonical release hash for %q", f.Path)
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return fmt.Errorf("invalid release hash for %q", f.Path)
		}
	}
	for role := range roleNames {
		if !seenRole[role] {
			return fmt.Errorf("missing required release role %q", role)
		}
	}
	// A source file cannot also be a directory containing another payload.
	// This is independent of the separate installation destination checks.
	paths := make([]string, 0, len(seenPath))
	for p := range seenPath {
		paths = append(paths, p)
	}
	// Testing only adjacent sorted entries fails for a, a-, a/b.
	// Check every ancestor against the complete file set.
	paths = append(paths, "assets/manifest.json")
	if hasReleasePathCollision(paths) {
		return errors.New("release source file/directory collision")
	}
	if hasReleasePathCollision(destinations) {
		return errors.New("release installation destination collision")
	}
	return nil
}

// runtimeReleasePlan normalizes both wire schemas into one declared runtime
// inventory. Only historical V3 reads implicit platform-owned policy.
type runtimeReleasePlan struct {
	assets           []ReleaseFile
	runtime          ReleaseRuntime
	legacyFiles      []string
	legacyChannelMap bool
}

func normalizeRuntimeRelease(m ReleaseManifest) (runtimeReleasePlan, error) {
	if m.Schema == ReleaseSchemaV4 {
		p := runtimeReleasePlan{runtime: m.Runtime}
		for _, f := range m.Files {
			if f.Role == "launcher" || f.Role == "child" || f.Role == "data" {
				p.assets = append(p.assets, f)
			}
		}
		return p, nil
	}
	contract, ok, err := platforms.LegacyContract(m.Platform)
	if err != nil {
		return runtimeReleasePlan{}, err
	}
	if !ok {
		return runtimeReleasePlan{}, errors.New("unknown V3 platform has no self-describing release contract")
	}
	p := runtimeReleasePlan{
		runtime:          ReleaseRuntime{VendorModes: contract.Runtime.VendorModes, LibrarySearch: contract.Runtime.LibrarySearch},
		legacyFiles:      contract.LegacyV3Files,
		legacyChannelMap: contract.LegacyV3ValidateChannelMap,
		assets: []ReleaseFile{
			{Path: "assets/external-asp", Role: "launcher", InstallTo: "bin/external-asp"},
			{Path: "assets/asp1-child", Role: "child", InstallTo: "lib/rootfs/factory/asp1-child"},
		},
	}
	for _, r := range contract.RequiredData {
		p.assets = append(p.assets, ReleaseFile{Path: r.Path, Role: "data", InstallTo: r.InstallTo})
	}
	return p, nil
}

func validateReleaseContents(m ReleaseManifest, files map[string][]byte, version string) ([]ReleaseFile, ReleaseRuntime, error) {
	normalized, err := normalizeRuntimeRelease(m)
	if err != nil {
		return nil, ReleaseRuntime{}, err
	}
	entries, runtime := []ReleaseFile{}, normalized.runtime
	if m.Schema == ReleaseSchemaV3 {
		expected := map[string]bool{}
		for _, name := range normalized.legacyFiles {
			expected[name] = true
		}
		if len(files) != len(expected) {
			return nil, runtime, errors.New("legacy release file-set differs")
		}
		for name, raw := range files {
			if !expected[name] || len(raw) == 0 {
				return nil, runtime, fmt.Errorf("invalid legacy release member %q", name)
			}
		}
		if normalized.legacyChannelMap {
			if err := platforms.ValidateChannelMap(files["assets/channel-map"]); err != nil {
				return nil, runtime, err
			}
		}
		entries = append(entries, ReleaseFile{Path: "asp-installer", Role: "installer"}, ReleaseFile{Path: "LICENSE", Role: "license"})
		entries = append(entries, normalized.assets...)
	} else {
		if version != m.Version {
			return nil, runtime, errors.New("V4 version mismatch")
		}
		if len(files) != len(m.Files)+1 {
			return nil, runtime, errors.New("V4 release file inventory incomplete or has extras")
		}
		declared := map[string]bool{}
		for _, f := range m.Files {
			raw, ok := files[f.Path]
			if !ok || int64(len(raw)) != f.Size {
				return nil, runtime, fmt.Errorf("V4 asset missing or size differs: %s", f.Path)
			}
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != f.SHA256 {
				return nil, runtime, fmt.Errorf("V4 asset digest mismatch: %s", f.Path)
			}
			declared[f.Path] = true
		}
		for name := range files {
			if name != "assets/manifest.json" && !declared[name] {
				return nil, runtime, fmt.Errorf("undeclared release member %q", name)
			}
		}
		entries = append(entries, m.Files...)
	}
	if _, err := elfNeeded(files["asp-installer"], false, ""); err != nil {
		return nil, runtime, fmt.Errorf("invalid embedded installer ELF: %w", err)
	}
	if _, err := elfNeeded(files["assets/asp1-child"], false, "/system/bin/linker"); err != nil {
		return nil, runtime, fmt.Errorf("invalid helper ELF: %w", err)
	}
	if _, err := elfNeeded(files["assets/external-asp"], false, ""); err != nil {
		return nil, runtime, fmt.Errorf("invalid launcher ELF: %w", err)
	}
	return entries, runtime, nil
}

func makeReleasePlan(name, version string, raw []byte, files map[string][]byte, manifest ReleaseManifest) (VerifiedRelease, error) {
	entries, runtime, err := validateReleaseContents(manifest, files, version)
	if err != nil {
		return VerifiedRelease{}, err
	}
	installed := make([]ReleaseFile, 0, len(entries))
	for _, f := range entries {
		if f.InstallTo != "" {
			installed = append(installed, f)
		}
	}
	sort.Slice(installed, func(i, j int) bool { return installed[i].InstallTo < installed[j].InstallTo })
	return VerifiedRelease{
		Name: name, Version: version, Platform: manifest.Platform, CacheSchema: manifest.CacheSchema,
		Schema: manifest.Schema, Original: raw, Files: files, Manifest: manifest, Runtime: runtime, Installed: installed,
	}, nil
}

// hasReleasePathCollision also detects duplicated destination names.
func hasReleasePathCollision(paths []string) bool {
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if seen[p] {
			return true
		}
		seen[p] = true
	}
	for _, p := range paths {
		for i := 0; i < len(p); i++ {
			if p[i] == '/' && seen[p[:i]] {
				return true
			}
		}
	}
	return false
}
