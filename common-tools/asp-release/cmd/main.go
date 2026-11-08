package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"github.com/dmit2k/frankenasp-v3/platforms"
)

type dataArgs []string

func (v *dataArgs) String() string     { return strings.Join(*v, ",") }
func (v *dataArgs) Set(s string) error { *v = append(*v, s); return nil }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "asp-release:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	f := flag.NewFlagSet("asp-release", flag.ContinueOnError)
	platform := f.String("platform", "", "Echo codename")
	version := f.String("version", "", "Release version")
	launcher := f.String("launcher", "", "Native ARM32 launcher")
	child := f.String("child", "", "Native ARM32 child")
	installer := f.String("installer", "", "Linux ARMv7 installer")
	license := f.String("license", "", "Public license")
	contract := f.String("contract", "", "Platform release contract JSON")
	out := f.String("out-dir", "", "Existing destination directory")
	verify := f.String("validate", "", "Validate an existing release archive")
	jsonOutput := f.Bool("json", false, "Emit validated archive identity as JSON (only with --validate)")
	var files dataArgs
	f.Var(&files, "data", "Asset mapping assets/name=lib/rootfs/factory/name:/source/path")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unknown positional arguments")
	}
	if *verify != "" {
		if *platform != "" || *version != "" || *launcher != "" || *child != "" || *installer != "" || *license != "" || *contract != "" || *out != "" || len(files) > 0 {
			return errors.New("--validate must be used alone")
		}
		v, e := asp.LoadRelease(*verify, "")
		if e != nil {
			return e
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(map[string]string{"name": v.Name, "schema": v.Schema, "platform": v.Platform, "version": v.Version})
		}
		fmt.Printf("VERIFIED %s schema=%s platform=%s version=%s files=%d\n", v.Name, v.Schema, v.Platform, v.Version, len(v.Files))
		return nil
	}
	if *jsonOutput {
		return errors.New("--json requires --validate")
	}
	if *platform == "" || *version == "" || *launcher == "" || *child == "" || *installer == "" || *license == "" || *out == "" {
		return errors.New("required: --platform, --version, --launcher, --child, --installer, --license, --out-dir")
	}
	var runtime asp.ReleaseRuntime
	var required []platforms.DataRequirement
	if *contract != "" {
		b, e := os.ReadFile(*contract)
		if e != nil {
			return e
		}
		var c platforms.ReleaseContract
		if e = asp.DecodeStrictJSON(b, &c); e != nil {
			return fmt.Errorf("invalid platform contract: %w", e)
		}
		if c.Platform != *platform {
			return errors.New("platform contract identity mismatch")
		}
		runtime = asp.ReleaseRuntime{VendorModes: c.Runtime.VendorModes, LibrarySearch: c.Runtime.LibrarySearch}
		required = c.RequiredData
	} else {
		c, ok, e := platforms.LegacyContract(*platform)
		if e != nil {
			return e
		}
		if !ok {
			return errors.New("unknown platform requires --contract with runtime capabilities")
		}
		runtime = asp.ReleaseRuntime{VendorModes: c.Runtime.VendorModes, LibrarySearch: c.Runtime.LibrarySearch}
		required = c.RequiredData
	}
	read := func(p string) ([]byte, error) {
		s, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if !s.Mode().IsRegular() || s.Size() <= 0 || s.Size() > 32<<20 {
			return nil, fmt.Errorf("invalid release source file %q", filepath.Base(p))
		}
		return os.ReadFile(p)
	}
	req := asp.V4Package{Platform: *platform, Version: *version, Runtime: runtime}
	var e error
	if req.Installer, e = read(*installer); e != nil {
		return e
	}
	if req.License, e = read(*license); e != nil {
		return e
	}
	if req.Launcher, e = read(*launcher); e != nil {
		return e
	}
	if req.Child, e = read(*child); e != nil {
		return e
	}
	for _, entry := range files {
		lhs, filename, ok := strings.Cut(entry, ":")
		if !ok || filename == "" {
			return fmt.Errorf("invalid --data %q", entry)
		}
		name, dest, ok := strings.Cut(lhs, "=")
		if !ok || name == "" || dest == "" {
			return fmt.Errorf("invalid --data destination %q", entry)
		}
		data, e := read(filename)
		if e != nil {
			return e
		}
		req.Data = append(req.Data, asp.ReleaseResource{Path: name, InstallTo: dest, Bytes: data})
	}
	for _, r := range required {
		found := false
		for _, resource := range req.Data {
			if resource.Path == r.Path && resource.InstallTo == r.InstallTo {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing required platform release data %q", r.Path)
		}
	}
	verified, e := asp.PackV4(req)
	if e != nil {
		return e
	}
	written, e := asp.SaveRelease(*out, verified)
	if e != nil {
		return e
	}
	fmt.Println(written)
	return nil
}
