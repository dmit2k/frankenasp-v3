//go:build linux

package hostos

import "github.com/dmit2k/frankenasp-v3/common-tools/asp-extractor/internal/hostos/linux"

func Current() Adapter { return linux.Adapter{} }
