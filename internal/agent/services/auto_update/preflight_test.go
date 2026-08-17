package auto_update

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// What counts as a correct layout is the OPPOSITE on the two platforms, so
// every case names its OS explicitly. An earlier version of this test called
// WritabilityPreflight directly and therefore asserted whatever the runner
// happened to be: it passed on a macOS developer machine while pinning the
// wrong contract for the Linux CI runner it shipped through.
func TestWritabilityPreflight(t *testing.T) {
	const exe = "/usr/local/bin/senhub-agent"

	writable := func(string) bool { return true }
	readOnly := func(string) bool { return false }
	fileOnly := func(p string) bool { return p == exe }

	owned := func(uid int, mode fs.FileMode) func(string) (ownership, error) {
		return func(string) (ownership, error) { return ownership{uid: uid, mode: mode}, nil }
	}
	unknownOwner := func(string) (ownership, error) { return ownership{}, errors.New("nope") }

	cases := []struct {
		name     string
		goos     string
		canWrite func(string) bool
		owner    func(string) (ownership, error)
		wantWarn bool
		mustSay  string
	}{
		// Linux: the question is ownership, not effective writability.
		{name: "linux root-owned 0755 is correct", goos: "linux", owner: owned(0, 0o755), wantWarn: false},
		{name: "linux owned by the service account", goos: "linux", owner: owned(999, 0o755),
			wantWarn: true, mustSay: "non-root account"},
		{name: "linux root-owned but group-writable", goos: "linux", owner: owned(0, 0o775),
			wantWarn: true, mustSay: "non-root account"},
		{name: "linux root-owned but world-writable", goos: "linux", owner: owned(0, 0o757),
			wantWarn: true, mustSay: "non-root account"},

		// The regression this replaced: run under sudo, canWrite says true for
		// every file on the host, and the old check warned on every correctly
		// installed agent. Ownership does not care who is asking.
		{name: "linux root-owned, evaluated by a root caller, stays quiet", goos: "linux",
			canWrite: writable, owner: owned(0, 0o755), wantWarn: false},

		// Cannot determine ownership: say nothing rather than warn on a guess.
		{name: "linux unknown ownership warns about nothing", goos: "linux",
			owner: unknownOwner, wantWarn: false},

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
			cw := tc.canWrite
			if cw == nil {
				cw = readOnly
			}
			ow := tc.owner
			if ow == nil {
				ow = unknownOwner
			}
			got := writabilityPreflightFor(tc.goos, exe, cw, ow)
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
		})
	}
}

func TestWritabilityPreflightIgnoresAnUnknownExecutable(t *testing.T) {
	nothing := func(string) (ownership, error) { return ownership{}, nil }
	for _, goos := range []string{"linux", "windows"} {
		if got := writabilityPreflightFor(goos, "", func(string) bool { return true }, nothing); got != "" {
			t.Errorf("%s: an unresolvable executable path must not produce a warning; got %q", goos, got)
		}
	}
}

func TestOwnershipExposure(t *testing.T) {
	cases := []struct {
		uid     int
		mode    fs.FileMode
		exposed bool
	}{
		{0, 0o755, false},
		{0, 0o700, false},
		{0, 0o775, true},   // group-writable
		{0, 0o757, true},   // world-writable
		{999, 0o755, true}, // owned by a service account
	}
	for _, tc := range cases {
		if got := (ownership{uid: tc.uid, mode: tc.mode}).exposedToNonRoot(); got != tc.exposed {
			t.Errorf("uid=%d mode=%04o exposed = %v, want %v", tc.uid, tc.mode.Perm(), got, tc.exposed)
		}
	}
}
