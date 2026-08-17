package auto_update

import (
	"strings"
	"testing"
)

// What counts as a correct layout is the OPPOSITE on the two platforms, so
// every case names its OS explicitly. The previous version of this test called
// WritabilityPreflight directly and therefore asserted whatever the runner
// happened to be: it passed on a macOS developer machine while pinning the
// wrong contract for the Linux CI runner it actually shipped through.
func TestWritabilityPreflight(t *testing.T) {
	const exe = "/usr/local/bin/senhub-agent"

	writable := func(string) bool { return true }
	readOnly := func(string) bool { return false }
	fileOnly := func(p string) bool { return p == exe }

	cases := []struct {
		name       string
		goos       string
		canWrite   func(string) bool
		wantWarn   bool
		mustSay    string
		mustNotSay string
	}{
		// Linux: the daemon must not be able to write its own executable
		// (#794), so a read-only binary is the healthy state. Getting this
		// backwards would print a warning on every correctly installed host,
		// which is how operators learn to ignore warnings.
		{name: "linux read-only binary is correct", goos: "linux", canWrite: readOnly, wantWarn: false},
		{name: "linux writable binary is a security finding", goos: "linux", canWrite: writable,
			wantWarn: true, mustSay: "compromised", mustNotSay: "auto_update.enabled"},

		// Elsewhere the in-process updater renames a sibling over the running
		// binary, so it needs both the file and its directory (#377).
		{name: "windows writable binary and dir is correct", goos: "windows", canWrite: writable, wantWarn: false},
		{name: "windows read-only binary breaks every update cycle", goos: "windows", canWrite: readOnly,
			wantWarn: true, mustSay: "auto_update.enabled"},
		{name: "windows writable file in a read-only dir still cannot be replaced", goos: "windows",
			canWrite: fileOnly, wantWarn: true, mustSay: "auto_update.enabled"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := writabilityPreflightFor(tc.goos, exe, tc.canWrite)
			if warned := got != ""; warned != tc.wantWarn {
				t.Fatalf("warned = %v, want %v (message: %q)", warned, tc.wantWarn, got)
			}
			if !tc.wantWarn {
				return
			}
			if !strings.Contains(got, exe) {
				t.Errorf("the warning must name the binary path; got %q", got)
			}
			if tc.mustSay != "" && !strings.Contains(got, tc.mustSay) {
				t.Errorf("the warning should mention %q; got %q", tc.mustSay, got)
			}
			if tc.mustNotSay != "" && strings.Contains(got, tc.mustNotSay) {
				t.Errorf("the warning must not blame %q — on Linux this is a security finding, "+
					"not an auto-update setting, and it holds whether or not auto-update is on; got %q",
					tc.mustNotSay, got)
			}
		})
	}
}

func TestWritabilityPreflightIgnoresAnUnknownExecutable(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		if got := writabilityPreflightFor(goos, "", func(string) bool { return true }); got != "" {
			t.Errorf("%s: an unresolvable executable path must not produce a warning; got %q", goos, got)
		}
	}
}
