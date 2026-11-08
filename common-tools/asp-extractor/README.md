# asp-extractor — Linux x86_64

Desktop utility for extracting owner-provided factory ASP dependencies independently of Echo platform codenames. The V4 format supports release archives for arbitrary Echo codenames without changes to this tool; the native launcher/child and hardware validation remain platform-owned. Only Athena and Rook are currently hardware-tested. It does **not** install or activate the runtime on the Echo.

**Full documentation:** [ASP-EXTRACTOR.md](../../docs/ASP-EXTRACTOR.md) in the source repository (or `ASP-EXTRACTOR.md` beside this README in the packaged archive), including all commands, option defaults, storage, OTA/backup handling, bundles, SSH delivery and device follow-up. For the embedded tool, see [ASP-INSTALL.md](../../docs/ASP-INSTALL.md).

```sh
# From the repository root:
GOTOOLCHAIN=local GOFLAGS=-p=2 GOMAXPROCS=2 go test -race -count=1 ./...
GOTOOLCHAIN=local GOFLAGS=-p=2 GOMAXPROCS=2 go vet ./...
GO=/path/to/supported/go common-tools/asp-extractor/build.sh
common-tools/asp-extractor/package.sh \
  common-tools/asp-extractor/dist/asp-extractor-vVERSION-linux-amd64
```

The versioned package includes this README and the full `ASP-EXTRACTOR.md` guide. No Amazon firmware or proprietary factory binaries are included.
