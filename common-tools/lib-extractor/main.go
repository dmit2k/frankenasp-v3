package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var revision = "development"

func printJSON(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b)) }
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "asp-installer:", e)
		os.Exit(2)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: asp-installer <inspect|install|status|version> [--platform athena|rook --ota PATH|--ota-url URL --assets DIR]")
	}
	cmd := os.Args[1]
	if cmd == "version" {
		printJSON(map[string]string{"version": revision})
		return nil
	}
	if cmd == "status" {
		raw, e := os.ReadFile(filepath.Join(destination, "etc/installer-receipt.json"))
		if e != nil {
			return e
		}
		fmt.Print(string(raw))
		return nil
	}
	if cmd != "inspect" && cmd != "install" {
		return errors.New("unknown command")
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	platform := f.String("platform", "", "device platform: athena or rook")
	ota := f.String("ota", "", "local downloaded OTA ZIP")
	otaURL := f.String("ota-url", "", "user selected OTA URL")
	assets := f.String("assets", "./assets", "directory containing release launcher/helper/config")
	temp := f.String("tmp-dir", "/data/techo5-linux", "OTA temporary download directory on Echo")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	if *platform != "" && *platform != "athena" && *platform != "rook" {
		return errors.New("--platform must be athena or rook")
	}
	if (*ota == "") == (*otaURL == "") {
		return errors.New("select exactly one --ota or --ota-url")
	}
	if cmd == "install" {
		if os.Geteuid() != 0 {
			return errors.New("installation requires root; inspect is read-only")
		}
		if _, e := os.Lstat(destination); e == nil {
			return errors.New("external-asp already exists: refusing automatic replacement; no download performed")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	manifestRaw, e := os.ReadFile(filepath.Join(*assets, "manifest.json"))
	if e != nil {
		return fmt.Errorf("release manifest missing: %w", e)
	}
	var manifest struct {
		Schema   string `json:"schema"`
		Platform string `json:"platform"`
	}
	if e := json.Unmarshal(manifestRaw, &manifest); e != nil {
		return e
	}
	if manifest.Schema != "ASP1-ASSETS-V3" || (manifest.Platform != "athena" && manifest.Platform != "rook") {
		return errors.New("invalid release platform manifest")
	}
	if *platform == "" {
		*platform = manifest.Platform
	} else if *platform != manifest.Platform {
		return errors.New("release platform does not match requested Echo platform")
	}
	assetBytes, e := os.ReadFile(filepath.Join(*assets, "asp1-child"))
	if e != nil {
		return fmt.Errorf("missing bundled helper: %w", e)
	}
	if _, e = elfNeeded(assetBytes, false, "/system/bin/linker"); e != nil {
		return fmt.Errorf("bad bundled Bionic helper: %w", e)
	}
	launcher, e := os.ReadFile(filepath.Join(*assets, "external-asp"))
	if e != nil {
		return fmt.Errorf("missing bundled launcher: %w", e)
	}
	if _, e = elfNeeded(launcher, false, ""); e != nil {
		return fmt.Errorf("bad bundled native launcher: %w", e)
	}
	if *platform == "athena" {
		b, e := os.ReadFile(filepath.Join(*assets, "channel-map"))
		if e != nil {
			return e
		}
		if e = validateChannelMap(b); e != nil {
			return e
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	path := *ota
	cleanup := func() {}
	if *otaURL != "" {
		path, cleanup, e = download(ctx, *otaURL, *temp)
		if e != nil {
			return e
		}
		defer cleanup()
	}
	sources, fhandle, e := openOTA(path, *platform)
	if e != nil {
		return e
	}
	defer fhandle.Close()
	for _, source := range sources.selective {
		source.ctx = ctx
	}
	c := newCollector(sources)
	if e = c.closure(*platform, assetBytes); e != nil {
		return fmt.Errorf("runtime dependency closure: %w", e)
	}
	var bytes int
	for _, v := range c.items {
		bytes += len(v)
	}
	summary := map[string]any{"platform": *platform, "ota_format": sources.kind, "runtime_files": len(c.items), "runtime_bytes": bytes, "dependency_edges": c.edgeCount, "source_authentication": "user-selected; no OTA signature, hash or TLS certificate check", "installation_path": destination}
	if cmd == "inspect" {
		printJSON(summary)
		return nil
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = install(*platform, *assets, c.items, sources.kind, destination); e != nil {
		return e
	}
	summary["installed"] = true
	summary["activation"] = "not changed; TECHO5 selector is managed separately"
	printJSON(summary)
	return nil
}
