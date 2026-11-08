package platforms

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed */release-contract.json
var embeddedContracts embed.FS

// RuntimeContract declares capability-based vendor modes and ordered library lookup.
type RuntimeContract struct {
	VendorModes   []string `json:"vendor_modes"`
	LibrarySearch []string `json:"library_search"`
}
type DataRequirement struct {
	Path      string `json:"path"`
	InstallTo string `json:"install_to"`
}
type ReleaseContract struct {
	Platform                   string            `json:"platform"`
	DisplayName                string            `json:"display_name"`
	KnownFireOS                map[string]string `json:"known_fireos"`
	Runtime                    RuntimeContract   `json:"runtime"`
	LegacyV3Files              []string          `json:"legacy_v3_files"`
	RequiredData               []DataRequirement `json:"required_data"`
	LegacyV3ValidateChannelMap bool              `json:"legacy_v3_validate_channel_map"`
}

// LegacyContract supplies the historical metadata missing from V3. The caller
// must not use presence in this inventory to admit or deny V4 platforms.
func LegacyContract(platform string) (ReleaseContract, bool, error) {
	switch platform {
	case "athena", "rook":
	default:
		return ReleaseContract{}, false, nil
	}
	b, err := embeddedContracts.ReadFile(platform + "/release-contract.json")
	if err != nil {
		return ReleaseContract{}, true, err
	}
	var c ReleaseContract
	if err = json.Unmarshal(b, &c); err != nil {
		return ReleaseContract{}, true, err
	}
	if c.Platform != platform {
		return ReleaseContract{}, true, fmt.Errorf("release contract identity conflict for %q", platform)
	}
	if len(c.LegacyV3Files) < 5 || len(c.Runtime.VendorModes) == 0 || len(c.Runtime.LibrarySearch) == 0 {
		return ReleaseContract{}, true, errors.New("incomplete legacy release contract")
	}
	return c, true, nil
}
