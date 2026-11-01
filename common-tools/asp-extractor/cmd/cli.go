package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	commands := map[string]bool{"extract": true, "list": true, "verify": true, "create-bundle": true, "deploy-cache": true, "deploy-asp": true, "deploy-all": true}
	ix := -1
	for i, a := range args {
		if commands[a] {
			if ix >= 0 {
				return usageError()
			}
			ix = i
		}
	}
	if ix < 0 {
		if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
			printUsage()
			return nil
		}
		return usageError()
	}
	cmd := args[ix]
	filtered := append(append([]string{}, args[:ix]...), args[ix+1:]...)
	root, err := hostos.Current().DefaultStore()
	if err != nil {
		return err
	}
	o := options{command: cmd, cacheDir: filepath.Join(root, "asp-installer-cache"), port: 22}
	f := flag.NewFlagSet("asp-extractor "+cmd, flag.ContinueOnError)
	f.StringVar(&o.platform, "platform", "", "target Echo codename (athena|rook)")
	f.StringVar(&o.firmware, "firmware", "", "firmware build/Fire OS version or exact cache directory name")
	f.StringVar(&o.cacheDir, "cache-dir", o.cacheDir, "absolute PC asp-installer-cache root")
	f.StringVar(&o.ota, "ota", "", "local full OTA ZIP")
	f.StringVar(&o.otaURL, "ota-url", "", "explicit full OTA URL (owner-selected)")
	f.StringVar(&o.backup, "backup-dir", "", "directory holding raw TECHO5 backup images")
	f.StringVar(&o.release, "release", "", "original versioned FrankenASP release .tar.gz")
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
	if cmd != "list" && o.platform == "" {
		return errors.New("--platform is required")
	}
	if o.platform != "" && o.platform != "athena" && o.platform != "rook" {
		return errors.New("--platform must be athena or rook")
	}
	if !filepath.IsAbs(o.cacheDir) || filepath.Clean(o.cacheDir) == "/" {
		return errors.New("--cache-dir must be an absolute non-root directory")
	}
	if o.firmware != "" && (!safePath(o.firmware) || strings.Contains(o.firmware, "/")) {
		return errors.New("unsafe --firmware")
	}
	if o.port < 1 || o.port > 65535 {
		return errors.New("invalid SSH port")
	}
	switch cmd {
	case "list":
		return listLocal(o)
	case "extract":
		if o.release == "" {
			return errors.New("extract requires --release to identify the Bionic helper and exact dependency closure")
		}
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
		result, err := selectDesktopCache(o, "")
		if err != nil {
			return err
		}
		fmt.Printf("VERIFIED: %s (%s, Fire OS %s, %d files, %d bytes)\n", result.Path, result.Metadata.Firmware, fireOSForCache(result.Metadata), result.Metadata.Files, result.Metadata.Bytes)
		return nil
	case "create-bundle":
		if o.release == "" || o.output == "" {
			return errors.New("create-bundle requires --release and --output")
		}
		return createBundle(o)
	case "deploy-cache", "deploy-asp", "deploy-all":
		if o.host == "" {
			return errors.New("deployment requires --host root@hostname")
		}
		if cmd != "deploy-cache" && o.release == "" {
			return errors.New("deploy-asp/deploy-all require --release")
		}
		return deploy(o)
	}
	return usageError()
}
func usageError() error { return errors.New("use asp-extractor help for commands and flags") }
func printUsage() {
	fmt.Print(`asp-extractor: offline ASP factory cache and delivery (Linux x86_64)

Usage:
  asp-extractor --version
  asp-extractor --platform athena|rook [flags] COMMAND

Commands: extract, list, verify, create-bundle, deploy-cache, deploy-asp, deploy-all

  extract        --release ARCHIVE --ota FILE | --ota-url URL | --backup-dir DIR
  list           [--platform CODENAME] [--cache-dir ABS_PATH]
  verify         [--firmware VERSION_OR_CACHE]
  create-bundle  --firmware VERSION --release ARCHIVE --output BUNDLE.tar.gz
  deploy-cache   --host root@ECHO [--firmware VERSION]
  deploy-asp     --host root@ECHO --release ARCHIVE
  deploy-all     --host root@ECHO --release ARCHIVE [--firmware VERSION]

Common: --platform, --firmware, --cache-dir, --release,
        --host, --port, --identity, --known-hosts, --output,
        --ota, --ota-url, --backup-dir, --refresh-cache

The extractor NEVER runs the embedded installer or modifies the active ASP runtime.
`)
}
