package app

import (
	"strings"
	"testing"
)

func existsIn(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, q := range paths {
			if p == q {
				return true
			}
		}
		return false
	}
}

func TestResolveUnitPath(t *testing.T) {
	const etc = "/etc/systemd/system/senhub-agent.service"
	const usr = "/usr/lib/systemd/system/senhub-agent.service"
	const lib = "/lib/systemd/system/senhub-agent.service"
	cases := []struct {
		name     string
		fragment string
		exists   func(string) bool
		want     string
	}{
		{"systemd reports the packaged unit", usr, existsIn(usr, etc), usr},
		{"systemd answer missing, falls back to /etc first", "", existsIn(etc, usr), etc},
		{"packaged only, systemd silent", "", existsIn(usr), usr},
		{"legacy /lib location", "", existsIn(lib), lib},
		{"reported file gone, known path wins", "/run/gone.service", existsIn(usr), usr},
		{"nothing anywhere", "", existsIn(), ""},
	}
	for _, c := range cases {
		if got := resolveUnitPath(c.fragment, c.exists); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestUnitOwnedByPackage(t *testing.T) {
	if unitOwnedByPackage("/etc/systemd/system/senhub-agent.service") {
		t.Error("an /etc unit is the operator's, not the package's")
	}
	for _, p := range []string{"/usr/lib/systemd/system/senhub-agent.service", "/lib/systemd/system/senhub-agent.service"} {
		if !unitOwnedByPackage(p) {
			t.Errorf("%s should be package-owned", p)
		}
	}
}

// A packaged unit execs /usr/bin; refreshing it must keep that line instead
// of rewriting it to the `install` layout's /usr/local/bin.
func TestRefreshedUnitKeepsPackagedExecStart(t *testing.T) {
	installed := strings.Replace(packagedSystemdUnit, "ExecStart=/usr/local/bin/", "ExecStart=/usr/bin/", 1)
	got := refreshedUnit(installed, func(p string) bool { return p == "/usr/bin/senhub-agent" })
	line, _ := installedExecStart(got)
	if line != "ExecStart=/usr/bin/senhub-agent run --config-path /etc/senhub-agent/agent.yaml" {
		t.Errorf("ExecStart rewritten to %q", line)
	}
}
