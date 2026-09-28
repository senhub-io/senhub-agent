package cliArgs

import (
	"os"
	"strings"
	"testing"
)

// Two cuts of the same tag keep the same commit — the identifier comes
// from a merge that did not change — so the build time is what tells a
// host under acceptance which binary it runs (#862).
func TestPrintVersionCarriesTheBuildTime(t *testing.T) {
	saveV, saveC, saveB := Version, CommitHash, BuildTime
	t.Cleanup(func() { Version, CommitHash, BuildTime = saveV, saveC, saveB })

	Version, CommitHash = "0.5.5-beta", "deadbee"

	first := captureVersion(t, "2026-09-10T08:00:00+0200")
	second := captureVersion(t, "2026-09-10T19:30:00+0200")

	if first == second {
		t.Fatal("two builds of the same tag and commit print the same line; a re-cut stays indistinguishable")
	}
	for _, want := range []string{"0.5.5-beta", "deadbee", "2026-09-10T19:30:00+0200"} {
		if !strings.Contains(second, want) {
			t.Errorf("version line %q does not carry %q", second, want)
		}
	}
}

func captureVersion(t *testing.T, buildTime string) string {
	t.Helper()
	BuildTime = buildTime

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	PrintVersion()
	_ = w.Close()
	os.Stdout = saved

	buf := make([]byte, 512)
	n, _ := r.Read(buf)
	_ = r.Close()
	return string(buf[:n])
}
