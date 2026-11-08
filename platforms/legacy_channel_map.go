package platforms

import (
	"errors"
	"strconv"
	"strings"
)

const channelPrefix = "ASP1-CHANNEL-MAP-V1\n"

func ValidateChannelMap(raw []byte) error {
	if len(raw) > 95 || !strings.HasPrefix(string(raw), channelPrefix) {
		return errors.New("invalid ASP1 channel-map header")
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 4 || lines[3] != "" || !strings.HasPrefix(lines[1], "mic=") || !strings.HasPrefix(lines[2], "ref=") {
		return errors.New("invalid channel-map lines")
	}
	for _, entry := range []struct {
		source string
		length int
	}{{strings.TrimPrefix(lines[1], "mic="), 4}, {strings.TrimPrefix(lines[2], "ref="), 2}} {
		parts := strings.Split(entry.source, ",")
		if len(parts) != entry.length {
			return errors.New("wrong captured-channel count")
		}
		seen := map[int]bool{}
		for _, part := range parts {
			if len(part) != 1 {
				return errors.New("noncanonical channel index")
			}
			n, e := strconv.Atoi(part)
			if e != nil || n < 0 || n >= entry.length || seen[n] {
				return errors.New("channel-map indices must form a permutation")
			}
			seen[n] = true
		}
	}
	return nil
}
