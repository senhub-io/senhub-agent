package auto_update

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetMsiUrl(t *testing.T) {
	a := &autoUpdate{}
	got, err := a.GetMsiUrl("https://reg.example.io/", "0.4.2")
	if err != nil {
		t.Fatalf("GetMsiUrl: %v", err)
	}
	want := "https://reg.example.io/download/0.4.2/senhub-agent-0.4.2-amd64.msi"
	if got != want {
		t.Errorf("GetMsiUrl = %q, want %q", got, want)
	}
	// The signature is fetched from <url>.minisig (same convention as the ZIP).
	if !strings.HasSuffix(want, ".msi") {
		t.Errorf("MSI url should end in .msi: %q", want)
	}
}

// TestIsMsiManagedOffWindows pins the safety property: on non-Windows the MSI
// path is inert, so the binary self-replace flow is unchanged. (On Windows the
// result depends on the registry marker and is covered by host validation.)
func TestIsMsiManagedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("registry-dependent on Windows; validated on a real MSI host")
	}
	if isMsiManaged() {
		t.Error("isMsiManaged() must be false off Windows so the ZIP update path stays in effect")
	}
}
