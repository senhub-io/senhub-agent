package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unicode/utf16"
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

// encodeNode writes one VS_VERSIONINFO-style block, the inverse of parseNode.
func encodeNode(key string, value []byte, valueLen uint16, text bool, children ...[]byte) []byte {
	b := make([]byte, 6)
	for _, c := range utf16.Encode([]rune(key)) {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	b = binary.LittleEndian.AppendUint16(b, 0)
	pad := func() {
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	pad()
	b = append(b, value...)
	for _, child := range children {
		pad()
		b = append(b, child...)
	}
	binary.LittleEndian.PutUint16(b, uint16(len(b)))
	binary.LittleEndian.PutUint16(b[2:], valueLen)
	if text {
		binary.LittleEndian.PutUint16(b[4:], 1)
	}
	return b
}

func encodeString(key, value string) []byte {
	var v []byte
	u := append(utf16.Encode([]rune(value)), 0)
	for _, c := range u {
		v = binary.LittleEndian.AppendUint16(v, c)
	}
	return encodeNode(key, v, uint16(len(u)), true)
}

func TestParseVersionInfo_RoundTrip(t *testing.T) {
	fixed := make([]byte, 52)
	binary.LittleEndian.PutUint32(fixed, fixedFileInfoMagic)
	binary.LittleEndian.PutUint32(fixed[8:], 0<<16|6)
	binary.LittleEndian.PutUint32(fixed[12:], 1<<16|2864)
	binary.LittleEndian.PutUint32(fixed[16:], 0<<16|6)
	binary.LittleEndian.PutUint32(fixed[20:], 1<<16|2864)
	table := encodeNode("040904B0", nil, 0, true,
		encodeString("CompanyName", companyName),
		encodeString("ProductName", productName),
		encodeString("FileVersion", "0.6.1-beta2"),
		encodeString("ProductVersion", "0.6.1-beta2"),
	)
	blob := encodeNode("VS_VERSION_INFO", fixed, 52, false,
		encodeNode("StringFileInfo", nil, 0, true, table))

	vi, err := parseVersionInfo(blob)
	if err != nil {
		t.Fatal(err)
	}
	want := [4]uint16{0, 6, 1, 2864}
	if err := checkVersionInfo(vi, want, "0.6.1-beta2"); err != nil {
		t.Fatal(err)
	}
	if err := checkVersionInfo(vi, [4]uint16{0, 6, 1, 2865}, "0.6.1-beta2"); err == nil {
		t.Fatal("a different build number was accepted")
	}
}

// The guard the build relies on: an exe linked without the .syso has no
// version resource, and verify must say so rather than pass.
func TestReadVersionInfo_ExeWithoutResourceIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles a Windows binary")
	}
	exe := filepath.Join(t.TempDir(), "plain.exe")
	cmd := exec.Command("go", "build", "-o", exe, "senhub-agent.go/cmd/console-launcher")
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building a windows exe: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "..", "cmd", "console-launcher", "rsrc_windows_amd64.syso")); err == nil {
		t.Skip("a generated .syso is present in cmd/console-launcher; the plain build is not plain")
	}
	_, err := readVersionInfo(exe)
	if !errors.Is(err, errNoVersionResource) {
		t.Fatalf("readVersionInfo on a plain exe = %v, want errNoVersionResource", err)
	}
}
