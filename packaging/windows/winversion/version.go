package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	companyName = "Sensor Factory"
	productName = "SenHub Agent"
	// The leading "0000" key is the language-neutral block go-winres
	// expects; "0409" (en-US) is the string table Explorer displays.
	neutralLang = "0000"
	stringLang  = "0409"
)

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// numericVersion turns a release version into the four 16-bit fields of
// a Windows file version: X.Y.Z from the release, and a build number as
// the fourth field.
//
// The fourth field is what lets Windows Installer replace an exe when two
// builds share X.Y.Z (a beta and its final, two betas, a re-cut): the
// installer only overwrites a versioned file with a strictly higher
// version, so an equal version keeps the file already on disk.
func numericVersion(version string, build int) ([4]uint16, error) {
	var out [4]uint16
	m := releaseVersion.FindStringSubmatch(version)
	if m == nil {
		return out, fmt.Errorf("version %q does not start with X.Y.Z", version)
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil || n > math.MaxUint16 {
			return out, fmt.Errorf("version %q: field %q does not fit a Windows version field (0-65535)", version, m[i+1])
		}
		out[i] = uint16(n)
	}
	if build < 0 || build > math.MaxUint16 {
		return [4]uint16{}, fmt.Errorf("build number %d does not fit a Windows version field (0-65535)", build)
	}
	out[3] = uint16(build)
	return out, nil
}

func formatNumeric(v [4]uint16) string {
	return fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
}

type resourceSpec struct {
	Version          string
	Build            int
	OriginalFilename string
	Description      string
	Icon             string
	Year             int
}

// winresJSON renders the go-winres input describing the version resource
// and, when an icon is given, the application icon. No manifest is
// emitted: adding one would change how Windows runs the binary (execution
// level, DPI awareness, OS compatibility), which is not this resource's
// concern.
func winresJSON(spec resourceSpec) ([]byte, error) {
	numeric, err := numericVersion(spec.Version, spec.Build)
	if err != nil {
		return nil, err
	}
	if spec.OriginalFilename == "" {
		return nil, fmt.Errorf("original filename is required")
	}
	fixed := map[string]string{
		"file_version":    formatNumeric(numeric),
		"product_version": formatNumeric(numeric),
	}
	if strings.Contains(spec.Version, "-") {
		fixed["flags"] = "Prerelease"
	}
	internalName := strings.TrimSuffix(spec.OriginalFilename, ".exe")
	doc := map[string]any{
		"RT_VERSION": map[string]any{
			"#1": map[string]any{
				neutralLang: map[string]any{
					"fixed": fixed,
					"info": map[string]any{
						stringLang: map[string]string{
							"CompanyName":      companyName,
							"FileDescription":  spec.Description,
							"FileVersion":      spec.Version,
							"InternalName":     internalName,
							"LegalCopyright":   fmt.Sprintf("Copyright (c) %d %s", spec.Year, companyName),
							"OriginalFilename": spec.OriginalFilename,
							"ProductName":      productName,
							"ProductVersion":   spec.Version,
						},
					},
				},
			},
		},
	}
	if spec.Icon != "" {
		doc["RT_GROUP_ICON"] = map[string]any{
			"APP": map[string]any{neutralLang: spec.Icon},
		}
	}
	return json.MarshalIndent(doc, "", "  ")
}
