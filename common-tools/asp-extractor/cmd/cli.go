package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmit2k/frankenasp-v3/common-tools/asp-extractor/internal/hostos"
)

var extractorVersion = "3.dev"
var revision = "3.dev" // For legacy installer source compiled but never invoked.

type options struct {
	command, platform, firmware, cacheDir, ota, otaURL, backup, release, output string
	host, identity, knownHosts                                                  string
	port                                                                        int
	refresh                                                                     bool
}

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "asp-extractor:", err)
		os.Exit(2)
	}
}
func runCLI(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Println("asp-extractor v" + extractorVersion)
		return nil
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		printUsage()
		return nil
	}
	ix, err := findCommand(args)
	if err != nil {
		return err
	}
	cmd := args[ix]
	filtered := append(append([]string{}, args[:ix]...), args[ix+1:]...)
	root, err := hostos.Current().DefaultStore()
	if err != nil {
		return err
	}
	o := options{command: cmd, cacheDir: filepath.Join(root, "asp-installer-cache"), port: 22}
	f := flag.NewFlagSet("asp-extractor "+cmd, flag.ContinueOnError)
	f.StringVar(&o.platform, "platform", "", "optional Echo codename; checked against source metadata if known")
	f.StringVar(&o.firmware, "firmware", "", "firmware build/Fire OS version or exact cache directory name")
	f.StringVar(&o.cacheDir, "cache-dir", o.cacheDir, "PC asp-installer-cache root (absolute or relative)")
	f.StringVar(&o.ota, "ota", "", "local full OTA ZIP")
	f.StringVar(&o.otaURL, "ota-url", "", "explicit full OTA URL (owner-selected)")
	f.StringVar(&o.backup, "backup-dir", "", "directory holding raw TECHO5 backup images")
	f.StringVar(&o.release, "asp-release", "", "path to local FrankenASP versioned release archive (.tar.gz), NOT a version string")
	f.StringVar(&o.output, "output", "", "bundle output archive path")
	f.StringVar(&o.host, "host", "", "SSH target root@hostname")
	f.StringVar(&o.identity, "identity", "", "SSH private key (default ~/.ssh/id_ed25519 or id_rsa)")
	f.StringVar(&o.knownHosts, "known-hosts", "", "SSH known_hosts file (default ~/.ssh/known_hosts)")
	f.IntVar(&o.port, "port", 22, "SSH port")
	f.BoolVar(&o.refresh, "refresh-cache", false, "re-extract and replace matching cache atomically")
	f.Usage = func() { printUsage() }
	if err = f.Parse(filtered); err != nil {
		return err
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	if cmd == "extract" {
		if o.release != "" {
			return errors.New("extract reads factory data only; omit --asp-release")
		}
		if o.backup != "" {
			detected, err := detectBackupPlatform(o.backup)
			if err != nil {
				return err
			}
			o.platform, err = resolveCodename(o.platform, detected)
			if err != nil {
				return err
			}
		} else if o.ota != "" {
			detected, err := detectOTAPlatform(o.ota)
			if err != nil {
				return err
			}
			o.platform, err = resolveCodename(o.platform, detected)
			if err != nil {
				return err
			}
		}
	}
	normalizedPlatform, err := resolveCodename(o.platform, "")
	if err != nil {
		return err
	}
	o.platform = normalizedPlatform
	if o.cacheDir == "" {
		return errors.New("--cache-dir must be a non-root directory")
	}
	o.cacheDir, err = filepath.Abs(o.cacheDir)
	if err != nil {
		return fmt.Errorf("--cache-dir: %w", err)
	}
	if o.cacheDir == "/" {
		return errors.New("--cache-dir must be a non-root directory")
	}
	if o.firmware != "" && (!asp.SafePath(o.firmware) || strings.Contains(o.firmware, "/")) {
		return errors.New("unsafe --firmware")
	}
	if o.port < 1 || o.port > 65535 {
		return errors.New("invalid SSH port")
	}
	switch cmd {
	case "list":
		return listLocal(o)
	case "extract":
		sources := 0
		for _, s := range []string{o.ota, o.otaURL, o.backup} {
			if s != "" {
				sources++
			}
		}
		if sources != 1 {
			return errors.New("extract requires exactly one --ota, --ota-url, or --backup-dir")
		}
		if o.firmware != "" && o.backup == "" {
			return errors.New("--firmware selects a cache; for OTA extraction firmware is read from the OTA")
		}
		return extractSource(context.Background(), o)
	case "verify":
		result, err := selectDesktopCache(o)
		if err != nil {
			return err
		}
		fmt.Printf("VERIFIED: %s (%s, Fire OS %s, %d files, %d bytes)\n", result.Path, result.Metadata.Firmware, asp.FireOSForCache(result.Metadata), result.Metadata.Files, result.Metadata.Bytes)
		return nil
	case "create-bundle":
		if o.release == "" || o.output == "" {
			return errors.New("create-bundle requires --asp-release FILE (local .tar.gz) and --output")
		}
		rel, err := loadRelease(o.release, o.platform)
		if err != nil {
			return fmt.Errorf("--asp-release: %w", err)
		}
		o.platform = rel.Platform
		return createBundle(o)
	case "deploy-cache", "deploy-asp", "deploy-all":
		if cmd == "deploy-asp" && o.firmware != "" {
			return errors.New("deploy-asp sends only the installer; --firmware is not used. Use deploy-all to transfer both the installer and the selected firmware cache")
		}
		if o.host == "" {
			return errors.New("deployment requires --host root@hostname")
		}
		if cmd != "deploy-cache" && o.release == "" {
			return errors.New("deploy-asp/deploy-all require --asp-release FILE (local .tar.gz)")
		}
		if o.release != "" {
			rel, err := loadRelease(o.release, o.platform)
			if err != nil {
				return fmt.Errorf("--asp-release: %w", err)
			}
			o.platform = rel.Platform
		}
		return deploy(o)
	}
	return usageError()
}

// findCommand recognises options anywhere in the invocation without confusing
// a flag value for a command. Unknown commands must never produce a generic
// "see help" error: the user needs to see the bad token and a useful hint.
func findCommand(args []string) (int, error) {
	commands := map[string]bool{"extract": true, "list": true, "verify": true, "create-bundle": true, "deploy-cache": true, "deploy-asp": true, "deploy-all": true}
	valueFlags := map[string]bool{
		"--platform": true, "--firmware": true, "--cache-dir": true,
		"--ota": true, "--ota-url": true, "--backup-dir": true,
		"--asp-release": true, "--output": true, "--host": true,
		"--identity": true, "--known-hosts": true, "--port": true,
	}
	ix := -1
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagName, _, attachedValue := strings.Cut(arg, "=")
			if valueFlags[flagName] && !attachedValue && i+1 < len(args) {
				i++
			}
			continue
		}
		if commands[arg] {
			if ix >= 0 {
				return -1, fmt.Errorf("multiple commands %q and %q; choose one", args[ix], arg)
			}
			ix = i
			continue
		}
		if ix < 0 {
			if arg == "deploy-app" {
				return -1, errors.New("unknown command \"deploy-app\"; use deploy-asp to transfer only the installer, or deploy-all to transfer both installer and firmware cache")
			}
			return -1, fmt.Errorf("unknown command %q; valid commands: extract, list, verify, create-bundle, deploy-cache, deploy-asp, deploy-all", arg)
		}
	}
	if ix < 0 {
		return -1, usageError()
	}
	return ix, nil
}

func usageError() error {
	return errors.New("missing command; valid commands: extract, list, verify, create-bundle, deploy-cache, deploy-asp, deploy-all (or run asp-extractor help)")
}
func printUsage() {
	fmt.Print(`asp-extractor: offline ASP factory cache and delivery (Linux x86_64)

Usage:
  asp-extractor --version
  asp-extractor [flags] COMMAND

Commands: extract, list, verify, create-bundle, deploy-cache, deploy-asp, deploy-all

  extract        --ota FILE | --ota-url URL | --backup-dir DIR
  list           [--platform CODENAME] [--cache-dir PATH]
  verify         [--firmware BUILD_OR_EXACT_DIRECTORY]
  create-bundle  [--firmware BUILD_OR_EXACT_DIRECTORY] --asp-release FILE --output BUNDLE.tar.gz
  deploy-cache   --host root@ECHO [--firmware VERSION]
  deploy-asp     --host root@ECHO --asp-release FILE             (installer only)
  deploy-all     --host root@ECHO --asp-release FILE [--firmware VERSION] (installer + cache)

Common: --platform, --firmware, --cache-dir, --asp-release,
        --host, --port, --identity, --known-hosts, --output,
        --ota, --ota-url, --backup-dir, --refresh-cache

Platform is optional for factory extraction and cache selection.
When available, device identity is detected from source metadata.
--asp-release FILE is a local .tar.gz archive path, NOT a version number.
--firmware normally takes Amazon's firmware build (e.g. 690918120), not a
cache directory; use the exact directory name only to distinguish variants.
--cache-dir is optional; defaults to:
  $XDG_CACHE_HOME/frankenasp/asp-extractor/asp-installer-cache
  (or ~/.cache/frankenasp/asp-extractor/asp-installer-cache).
Use --cache-dir if caches were extracted into a different directory.

Deployment commands transfer files ONLY. None runs the embedded installer,
changes the active ASP runtime, or restarts TECHO5.
`)
}
