# asp-extractor — Linux x86_64

Desktop utility for preparing and delivering owner-provided factory ASP dependencies to supported FrankenASP V3 devices (`athena` and `rook`). It does **not** install or activate the runtime on the Echo.

**Full documentation:** [ASP-EXTRACTOR.md](../../docs/ASP-EXTRACTOR.md) in the source repository (or `ASP-EXTRACTOR.md` beside this README in the packaged archive), including all commands, option defaults, storage, OTA/backup handling, bundles, SSH delivery and device follow-up. For the embedded tool, see [ASP-INSTALL.md](../../docs/ASP-INSTALL.md).

```sh
cd common-tools/asp-extractor
GO=/path/to/go1.26.9/bin/go ./build.sh
GO=/path/to/go1.26.9/bin/go ./test.sh
./package.sh dist/asp-extractor-vVERSION-linux-amd64
```

The versioned package includes this README and the full `ASP-EXTRACTOR.md` guide. No Amazon firmware or proprietary factory binaries are included.
