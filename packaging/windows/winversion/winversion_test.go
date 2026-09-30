package main

import (
	"encoding/json"
	"testing"
)

func TestNumericVersion(t *testing.T) {
	cases := []struct {
		version string
		build   int
		want    [4]uint16
		wantErr bool
	}{
		{"0.6.1-beta2", 2864, [4]uint16{0, 6, 1, 2864}, false},
		{"0.6.1", 2870, [4]uint16{0, 6, 1, 2870}, false},
		{"v1.2.3", 7, [4]uint16{1, 2, 3, 7}, false},
		{"0.6.0-dev.2864.gb1e429db", 2864, [4]uint16{0, 6, 0, 2864}, false},
		{"not-a-version", 1, [4]uint16{}, true},
		{"70000.0.0", 1, [4]uint16{}, true},
		{"0.6.1", 65536, [4]uint16{}, true},
		{"0.6.1", -1, [4]uint16{}, true},
	}
	for _, c := range cases {
		got, err := numericVersion(c.version, c.build)
		if (err != nil) != c.wantErr {
			t.Errorf("numericVersion(%q, %d) error = %v, wantErr %v", c.version, c.build, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("numericVersion(%q, %d) = %v, want %v", c.version, c.build, got, c.want)
		}
	}
}

// A later build of the same X.Y.Z must carry a strictly higher file
// version, or Windows Installer keeps the exe already installed.
func TestNumericVersion_LaterBuildOfSameReleaseIsHigher(t *testing.T) {
	beta, err := numericVersion("0.6.1-beta2", 2864)
	if err != nil {
		t.Fatal(err)
	}
	final, err := numericVersion("0.6.1", 2870)
	if err != nil {
		t.Fatal(err)
	}
	if !(final[3] > beta[3] && [3]uint16(final[:3]) == [3]uint16(beta[:3])) {
		t.Fatalf("final %v is not above beta %v", final, beta)
	}
}

func decodeWinres(t *testing.T, spec resourceSpec) map[string]any {
	t.Helper()
	out, err := winresJSON(spec)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	return doc["RT_VERSION"].(map[string]any)["#1"].(map[string]any)[neutralLang].(map[string]any)
}

func TestWinresJSON_PrereleaseKeepsFullStringAndFlag(t *testing.T) {
	v := decodeWinres(t, resourceSpec{Version: "0.6.1-beta2", Build: 2864, OriginalFilename: "senhub-agent.exe", Description: "SenHub Agent", Year: 2026})
	fixed := v["fixed"].(map[string]any)
	if fixed["file_version"] != "0.6.1.2864" || fixed["product_version"] != "0.6.1.2864" {
		t.Errorf("fixed versions = %v", fixed)
	}
	if fixed["flags"] != "Prerelease" {
		t.Errorf("flags = %v, want Prerelease", fixed["flags"])
	}
	strs := v["info"].(map[string]any)[stringLang].(map[string]any)
	for key, want := range map[string]string{
		"FileVersion":      "0.6.1-beta2",
		"ProductVersion":   "0.6.1-beta2",
		"CompanyName":      "Sensor Factory",
		"ProductName":      "SenHub Agent",
		"OriginalFilename": "senhub-agent.exe",
		"LegalCopyright":   "Copyright (c) 2026 Sensor Factory",
	} {
		if strs[key] != want {
			t.Errorf("%s = %v, want %q", key, strs[key], want)
		}
	}
}

func TestWinresJSON_FinalHasNoPrereleaseFlag(t *testing.T) {
	v := decodeWinres(t, resourceSpec{Version: "0.6.1", Build: 2870, OriginalFilename: "senhub-agent.exe"})
	if flags, ok := v["fixed"].(map[string]any)["flags"]; ok {
		t.Errorf("final release carries flags %v", flags)
	}
}
