package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A device codename is provenance metadata, not an extraction profile.
var codenamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,47}$`)

func normalizedCodename(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !codenamePattern.MatchString(value) {
		return ""
	}
	return value
}

func fingerprintCodename(fingerprint string) string {
	parts := strings.Split(fingerprint, "/")
	if len(parts) < 3 {
		return ""
	}
	device, _, ok := strings.Cut(parts[2], ":")
	if !ok {
		return ""
	}
	return normalizedCodename(device)
}

func sourceCodename(explicit, fingerprint string) (string, error) {
	fromDevice := normalizedCodename(explicit)
	fromFingerprint := fingerprintCodename(fingerprint)
	if fromDevice != "" && fromFingerprint != "" && fromDevice != fromFingerprint {
		return "", fmt.Errorf("source metadata device %s conflicts with fingerprint device %s", fromDevice, fromFingerprint)
	}
	if fromDevice != "" {
		return fromDevice, nil
	}
	return fromFingerprint, nil
}

func resolveCodename(requested, detected string) (string, error) {
	if requested != "" && normalizedCodename(requested) != requested {
		return "", fmt.Errorf("invalid --platform %q", requested)
	}
	if detected != "" && requested != "" && requested != detected {
		return "", fmt.Errorf("--platform %s conflicts with source platform %s", requested, detected)
	}
	if detected != "" {
		return detected, nil
	}
	return requested, nil
}

func buildPropCodename(raw []byte) (string, error) {
	for _, key := range []string{"ro.product.device", "ro.build.product"} {
		if name := sourceProperty(raw, key); name != "" {
			return sourceCodename(name, sourceProperty(raw, "ro.build.fingerprint"))
		}
	}
	return sourceCodename("", sourceProperty(raw, "ro.build.fingerprint"))
}

func sourceProperty(raw []byte, key string) string {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+"="))
		}
	}
	return ""
}

func readSystemBuildProp(src *otaSources) ([]byte, error) {
	var prop []byte
	err := src.resolve(func() error {
		var err error
		prop, err = src.system.Read("build.prop")
		if errors.Is(err, errMissing) {
			return nil
		}
		return err
	})
	return prop, err
}
