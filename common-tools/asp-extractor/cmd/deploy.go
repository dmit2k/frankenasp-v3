package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmit2k/frankenasp-v3/internal/asp"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const echoRoot = "/data/techo5-linux"
const echoInstaller = echoRoot + "/asp-installer-v3"
const echoCache = echoRoot + "/asp-installer-cache"

type remote struct{ client *ssh.Client }

func makeSSH(o options) (*remote, error) {
	user, host, ok := strings.Cut(o.host, "@")
	if !ok || user != "root" || host == "" || strings.ContainsAny(host, " /@\x00\n\r") {
		return nil, errors.New("--host must be root@hostname or root@IP")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	kh := o.knownHosts
	if kh == "" {
		kh = filepath.Join(home, ".ssh", "known_hosts")
	}
	callback, err := knownhosts.New(kh)
	if err != nil {
		return nil, fmt.Errorf("SSH host-key verification (%s): %w", kh, err)
	}
	key := o.identity
	if key == "" {
		for _, n := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			candidate := filepath.Join(home, ".ssh", n)
			if _, err := os.Stat(candidate); err == nil {
				key = candidate
				break
			}
		}
	}
	if key == "" {
		return nil, errors.New("no SSH private key found; specify --identity")
	}
	secret, err := os.ReadFile(key)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(secret)
	if err != nil {
		return nil, fmt.Errorf("cannot parse SSH private key %s: %w", key, err)
	}
	config := &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: callback, Timeout: 12 * time.Second,
		// Prefer modern Ed25519 host keys; Echo known_hosts may pin only Ed25519
		// while its Dropbear server also advertises an unrecorded RSA key.
		HostKeyAlgorithms: []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"},
	}
	addr := net.JoinHostPort(host, fmt.Sprint(o.port))
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}
	return &remote{client: conn}, nil
}
func (r *remote) close() { _ = r.client.Close() }
func (r *remote) exec(script string) (string, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput("sh -c " + shQuote("set -eu; "+script))
	if err != nil {
		return "", fmt.Errorf("remote command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func remoteSafePath(p string) error {
	if path.Clean(p) != p || !strings.HasPrefix(p, echoRoot+"/") || p == echoRoot || strings.ContainsAny(p, "\x00\n\r") {
		return fmt.Errorf("remote path outside permitted root: %q", p)
	}
	return nil
}
func (r *remote) put(file string, contents []byte, mode string) error {
	if err := remoteSafePath(file); err != nil {
		return err
	}
	folder := path.Dir(file)
	if _, err := r.exec("umask 077; mkdir -p " + shQuote(folder) + "; [ ! -L " + shQuote(folder) + " ]"); err != nil {
		return err
	}
	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(contents)
	output, err := session.CombinedOutput("sh -c " + shQuote("set -eu; umask 077; cat > "+shQuote(file)+"; chmod "+mode+" "+shQuote(file)))
	if err != nil {
		return fmt.Errorf("SSH upload %s: %w: %s", file, err, string(output))
	}
	return r.verifyFile(file, contents)
}
func (r *remote) verifyFile(file string, expected []byte) error {
	if err := remoteSafePath(file); err != nil {
		return err
	}
	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err = session.Start("sh -c " + shQuote("set -eu; [ -f "+shQuote(file)+" ]; [ ! -L "+shQuote(file)+" ]; cat "+shQuote(file))); err != nil {
		return err
	}
	h := sha256.New()
	count, readErr := io.Copy(h, io.LimitReader(stdout, int64(len(expected))+1))
	waitErr := session.Wait()
	expectedHash := sha256.Sum256(expected)
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return fmt.Errorf("remote verification %s: %w: %s", file, waitErr, stderr.String())
	}
	if count != int64(len(expected)) || !bytes.Equal(h.Sum(nil), expectedHash[:]) {
		return fmt.Errorf("remote SHA-256 mismatch for %s", file)
	}
	return nil
}
func (r *remote) exists(file string) (bool, error) {
	if err := remoteSafePath(file); err != nil {
		return false, err
	}
	result, err := r.exec("if [ -L " + shQuote(file) + " ]; then echo LINK; elif [ -e " + shQuote(file) + " ]; then echo YES; else echo NO; fi")
	if err != nil {
		return false, err
	}
	switch result {
	case "YES":
		return true, nil
	case "NO":
		return false, nil
	case "LINK":
		return false, fmt.Errorf("refusing symlink on Echo: %s", file)
	}
	return false, fmt.Errorf("unexpected SSH result %q", result)
}
func newStage(parent, prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return parent + "/." + prefix + "-" + hex.EncodeToString(b), nil
}
func (r *remote) makeStage(root, stage string) error {
	if root != echoCache || path.Dir(stage) != root {
		return errors.New("invalid deployment staging root")
	}
	if err := remoteSafePath(stage); err != nil {
		return err
	}
	_, err := r.exec("umask 077; [ -d " + shQuote(echoRoot) + " ]; [ ! -L " + shQuote(echoRoot) + " ]; " +
		"if [ ! -e " + shQuote(root) + " ]; then mkdir " + shQuote(root) + "; fi; " +
		"[ -d " + shQuote(root) + " ]; [ ! -L " + shQuote(root) + " ]; chmod 700 " + shQuote(root) + "; " +
		"[ ! -e " + shQuote(stage) + " ]; [ ! -L " + shQuote(stage) + " ]; mkdir " + shQuote(stage) + "; chmod 700 " + shQuote(stage))
	return err
}
func (r *remote) cleanStage(stage string) {
	if err := remoteSafePath(stage); err == nil {
		_, _ = r.exec("if [ -d " + shQuote(stage) + " ] && [ ! -L " + shQuote(stage) + " ]; then rm -rf " + shQuote(stage) + "; fi")
	}
}
func transferTree(r *remote, stage string, files map[string][]byte, modeFn func(string) string) error {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if !asp.SafePath(name) {
			return fmt.Errorf("unsafe transfer member %q", name)
		}
		if err := r.put(stage+"/"+name, files[name], modeFn(name)); err != nil {
			return err
		}
	}
	return nil
}
func (r *remote) getSmallFile(file string, maxBytes int64) ([]byte, error) {
	if err := remoteSafePath(file); err != nil {
		return nil, err
	}
	session, err := r.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err = session.Start("sh -c " + shQuote("set -eu; [ -f "+shQuote(file)+" ]; [ ! -L "+shQuote(file)+" ]; cat "+shQuote(file))); err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes+1))
	if int64(len(raw)) > maxBytes {
		session.Close()
		return nil, fmt.Errorf("oversized remote file %s", file)
	}
	waitErr := session.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("remote read %s: %w: %s", file, waitErr, stderr.String())
	}
	return raw, nil
}
func compatibleCacheMetadata(a, b asp.CacheMetadata) bool {
	// Archive retrieval path, URL and creation timestamp are not factory content.
	return a.Schema == b.Schema && a.Platform == b.Platform && a.Firmware == b.Firmware &&
		a.FireOS == b.FireOS && a.SourceSHA256 == b.SourceSHA256 &&
		a.OTAFormat == b.OTAFormat && a.Files == b.Files && a.Bytes == b.Bytes && a.DependencyEdges == b.DependencyEdges
}
func deployCache(r *remote, cache cacheResult) error {
	files, err := asp.CacheFiles(cache)
	if err != nil {
		return err
	}
	basename := filepath.Base(cache.Path)
	if !asp.SafePath(basename) || strings.Contains(basename, "/") {
		return errors.New("invalid destination cache name")
	}
	dest := echoCache + "/" + basename
	already, err := r.exists(dest)
	if err != nil {
		return err
	}
	if already {
		// Existing firmware caches must not be rewritten. Creation timestamps
		// legitimately differ; compare metadata identity and every file hash.
		raw, er := r.getSmallFile(dest+"/manifest.json", 16<<10)
		if er != nil {
			return fmt.Errorf("existing remote cache preserved: %w", er)
		}
		var remoteMeta asp.CacheMetadata
		if er = json.Unmarshal(raw, &remoteMeta); er != nil {
			return fmt.Errorf("remote manifest invalid: %w", er)
		}
		if !compatibleCacheMetadata(remoteMeta, cache.Metadata) {
			return errors.New("existing remote cache identity differs; preserved unchanged")
		}
		for name, data := range files {
			if name == "manifest.json" {
				continue
			}
			if err = r.verifyFile(dest+"/"+name, data); err != nil {
				return fmt.Errorf("existing remote cache differs; preserved: %w", err)
			}
		}
		fmt.Println("REMOTE CACHE already identical:", dest)
		return nil
	}
	stage, err := newStage(echoCache, "asp-extractor-cache")
	if err != nil {
		return err
	}
	if err = r.makeStage(echoCache, stage); err != nil {
		return err
	}
	defer r.cleanStage(stage)
	if err = transferTree(r, stage, files, func(string) string { return "600" }); err != nil {
		return err
	}
	if _, err = r.exec("[ ! -e " + shQuote(dest) + " ]; [ ! -L " + shQuote(dest) + " ]; mv " + shQuote(stage) + " " + shQuote(dest)); err != nil {
		return err
	}
	fmt.Println("REMOTE CACHE:", dest)
	return nil
}
func deployInstaller(r *remote, release releaseArchive) error {
	stage, err := newStage(echoCache, "asp-extractor-installer")
	if err != nil {
		return err
	}
	if err = r.makeStage(echoCache, stage); err != nil {
		return err
	}
	defer r.cleanStage(stage)
	modeFn := func(name string) string {
		if name == "asp-installer" || name == "assets/asp1-child" || name == "assets/external-asp" {
			return "500"
		}
		return "600"
	}
	if err = transferTree(r, stage, release.Files, modeFn); err != nil {
		return err
	}
	exists, err := r.exists(echoInstaller)
	if err != nil {
		return err
	}
	script := "[ ! -L " + shQuote(echoInstaller) + " ]; "
	if exists {
		// All release bytes have already been staged and verified.
		// Replacement deletes only the fixed embedded-installer directory.
		script += "[ -d " + shQuote(echoInstaller) + " ]; rm -rf " + shQuote(echoInstaller) + "; "
	}
	script += "mv " + shQuote(stage) + " " + shQuote(echoInstaller)
	if _, err = r.exec(script); err != nil {
		return err
	}
	fmt.Println("REMOTE INSTALLER:", echoInstaller, "version", release.Version)
	return nil
}
func deploy(o options) error {
	var cache cacheResult
	var rel releaseArchive
	var err error
	if o.command != "deploy-asp" {
		if o.release != "" {
			rel, err = loadRelease(o.release, o.platform)
			if err != nil {
				return err
			}
		}
		cache, err = selectDesktopCache(o)
		if err != nil {
			return err
		}
	}
	if o.command != "deploy-cache" && len(rel.Original) == 0 {
		rel, err = loadRelease(o.release, o.platform)
		if err != nil {
			return err
		}
	}
	if o.command != "deploy-cache" && rel.CacheSchema != asp.CacheSchema {
		return fmt.Errorf("--asp-release archive does not declare %s; use a release built with the new helper-independent installer", asp.CacheSchema)
	}
	if o.command == "deploy-all" {
		if err := asp.ValidateHelperRuntimeForPaths(rel.Files["assets/asp1-child"], cache.Items, rel.Runtime.LibrarySearch); err != nil {
			return err
		}
	}
	r, err := makeSSH(o)
	if err != nil {
		return err
	}
	defer r.close()
	if o.command == "deploy-cache" || o.command == "deploy-all" {
		if err = deployCache(r, cache); err != nil {
			return err
		}
	}
	if o.command == "deploy-asp" || o.command == "deploy-all" {
		if err = deployInstaller(r, rel); err != nil {
			return err
		}
	}
	fmt.Println("Delivery complete. No installer, echod, or persistent service was executed or changed.")
	fmt.Printf("Manual follow-up:\n  ssh -p %d %s\n  cd %s\n  ./asp-installer status\n", o.port, o.host, echoInstaller)
	if o.command != "deploy-asp" {
		fmt.Print(installerFirmwareInstructions(cache))
	} else {
		fmt.Println("  # First disable External ASP in TECHO5 settings.")
		fmt.Println("  # If exactly one matching firmware cache is available and no runtime exists:")
		fmt.Println("  ./asp-installer install --yes")
		fmt.Println("  # If a runtime exists, use instead:")
		fmt.Println("  ./asp-installer update --yes")
		fmt.Println("  # For multiple firmware caches, add --firmware CACHE_DIRECTORY_NAME.")
	}
	return nil
}

// installerFirmwareInstructions prefers the Amazon firmware build identifier.
// The embedded installer resolves --firmware by build, Fire OS version or
// exact cache directory. The exact directory is needed only to disambiguate
// multiple cache variants with the same firmware build.
func installerFirmwareInstructions(cache cacheResult) string {
	firmware := cache.Metadata.Firmware
	cacheName := filepath.Base(cache.Path)
	if firmware == "" {
		firmware = cacheName
	}
	return fmt.Sprintf(`  # Amazon firmware build: %s
  # Delivered factory cache: %s
  # First disable External ASP in TECHO5 settings. If no runtime exists:
  ./asp-installer install --firmware %s --yes
  # If runtime already exists, use instead:
  ./asp-installer update --firmware %s --yes
  # Only if multiple caches share this firmware build, choose the exact cache:
  # --firmware %s
`, firmware, cacheName, firmware, firmware, cacheName)
}
