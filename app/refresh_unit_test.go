package app

import (
	"strings"
	"testing"
)

func TestDiffLines_Identical(t *testing.T) {
	s := "a\nb\nc\n"
	got := diffLines(s, s)
	if len(got) != 0 {
		t.Errorf("diffLines(s, s) = %v, want nil or empty", got)
	}
}

func TestDiffLines_AddedLine(t *testing.T) {
	old := "a\n"
	new := "a\nb\n"
	got := diffLines(old, new)
	hasPlus := false
	hasMinus := false
	for _, l := range got {
		if strings.HasPrefix(l, "+ b") {
			hasPlus = true
		}
		if strings.HasPrefix(l, "- ") {
			hasMinus = true
		}
	}
	if !hasPlus {
		t.Errorf("expected '+ b' in diff output, got %v", got)
	}
	if hasMinus {
		t.Errorf("unexpected removal line in diff output, got %v", got)
	}
}

func TestDiffLines_RemovedLine(t *testing.T) {
	old := "a\nb\n"
	new := "a\n"
	got := diffLines(old, new)
	hasMinus := false
	hasPlus := false
	for _, l := range got {
		if strings.HasPrefix(l, "- b") {
			hasMinus = true
		}
		if strings.HasPrefix(l, "+ ") {
			hasPlus = true
		}
	}
	if !hasMinus {
		t.Errorf("expected '- b' in diff output, got %v", got)
	}
	if hasPlus {
		t.Errorf("unexpected addition line in diff output, got %v", got)
	}
}

func TestDiffLines_ChangedLine(t *testing.T) {
	old := "a\n"
	new := "c\n"
	got := diffLines(old, new)
	hasMinus := false
	hasPlus := false
	for _, l := range got {
		if strings.HasPrefix(l, "- a") {
			hasMinus = true
		}
		if strings.HasPrefix(l, "+ c") {
			hasPlus = true
		}
	}
	if !hasMinus {
		t.Errorf("expected '- a' in diff output, got %v", got)
	}
	if !hasPlus {
		t.Errorf("expected '+ c' in diff output, got %v", got)
	}
}

func TestInstalledServiceUser(t *testing.T) {
	cases := []struct {
		name string
		unit string
		want string
	}{
		{"hardened senhub", "[Service]\nUser=senhub\nGroup=senhub\n", "senhub"},
		{"legacy root explicit", "[Service]\nUser=root\nGroup=root\n", rootServiceUser},
		{"no User= line is root", "[Service]\nExecStart=/usr/local/bin/senhub-agent run\n", rootServiceUser},
		{"empty User= is root", "[Service]\nUser=\n", rootServiceUser},
		{"custom user", "[Service]\nUser=monitor\n", "monitor"},
		{"indented User=", "[Service]\n   User=senhub\n", "senhub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := installedServiceUser(tc.unit); got != tc.want {
				t.Errorf("installedServiceUser() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalUnitForUser(t *testing.T) {
	// Default user yields the packaged unit byte-for-byte.
	if got := canonicalUnitForUser(defaultServiceUser); got != packagedSystemdUnit {
		t.Error("canonicalUnitForUser(senhub) must equal packagedSystemdUnit verbatim")
	}

	// A custom non-root user still gets the hardened unit, re-templated.
	custom := canonicalUnitForUser("monitoring")
	if !strings.Contains(custom, "User=monitoring") || !strings.Contains(custom, "Group=monitoring") {
		t.Errorf("custom user must be templated into the hardened unit\n%s", custom)
	}
	if !strings.Contains(custom, "CapabilityBoundingSet=") {
		t.Error("a non-root install keeps the hardening directives")
	}
}

// A root install must come out of a refresh with the unit `install
// --user root` writes — capabilities included. Refreshing used to yield
// the hardened template with User=root, which drops every capability, so
// a --user root install silently lost the raw sockets and privileged
// ports it was chosen for (#689).
//
// This supersedes the previous expectation that the root unit was the
// packaged one with User=/Group= re-templated: it now carries no User= at
// all, root being implicit. The #575 property it guarded — a refresh must
// never reintroduce User=senhub on a root install — is asserted below and
// still holds.
func TestCanonicalUnitForUser_RootConvergesWithInstall(t *testing.T) {
	root := canonicalUnitForUser(rootServiceUser)

	if strings.Contains(root, "User=senhub") || strings.Contains(root, "Group=senhub") {
		t.Errorf("root unit must not reference the senhub user (#575)\n%s", root)
	}
	for _, dropped := range []string{"CapabilityBoundingSet=", "AmbientCapabilities=", "ProtectSystem=", "User="} {
		if strings.Contains(root, dropped) {
			t.Errorf("root unit must not carry %q — --user root exists to keep full privileges\n%s", dropped, root)
		}
	}

	// Convergence with the install path, which is the acceptance
	// criterion: identical to the template install --user root hands to
	// kardianos, modulo the two templated lines.
	installed := linuxSystemdScript(rootServiceUser)
	for _, line := range strings.Split(installed, "\n") {
		if strings.HasPrefix(line, "ExecStart=") || strings.HasPrefix(line, "{{if .WorkingDirectory}}") {
			continue
		}
		if !strings.Contains(root, line) {
			t.Errorf("refresh-unit root unit lost the install line %q", line)
		}
	}

	if !strings.Contains(root, "ExecStart=") {
		t.Error("root unit must carry a concrete ExecStart for refreshedUnit to splice over")
	}
}

func TestDiffLines_ContextLines(t *testing.T) {
	old := "line1\nline2\nold\nline4\nline5\n"
	new := "line1\nline2\nnew\nline4\nline5\n"
	got := diffLines(old, new)
	// line1/line2 are context before the change, line4/line5 after.
	hasContext := false
	for _, l := range got {
		if strings.HasPrefix(l, "  line") {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Errorf("expected context lines with '  ' prefix in diff output, got %v", got)
	}
}

const cliExecStart = `ExecStart=/opt/senhub/bin/senhub-agent "run" "--config-path" "/custom/agent.yaml"`

func cliInstalledUnit(user, execStart, workDir string) string {
	lines := []string{
		"[Unit]",
		"Description=SenHub Agent",
		"",
		"[Service]",
		"User=" + user,
		"Group=" + user,
		execStart,
	}
	if workDir != "" {
		lines = append(lines, workDir)
	}
	lines = append(lines, "", "[Install]", "WantedBy=multi-user.target", "")
	return strings.Join(lines, "\n")
}

func binaryAlways(exists bool) func(string) bool {
	return func(string) bool { return exists }
}

func TestRefreshedUnit_CanonicalInstallStaysCanonical(t *testing.T) {
	if got := refreshedUnit(packagedSystemdUnit, binaryAlways(true)); got != packagedSystemdUnit {
		t.Error("refreshing a canonical install must be a no-op")
	}
}

// A CLI install renders its own ExecStart (binary path, --config-path,
// flags). Refreshing must keep it while updating the hardening
// directives — silently repointing at the packaging path breaks the
// service when the binary is not there (#396).
func TestRefreshedUnit_PreservesCLIExecStart(t *testing.T) {
	installed := cliInstalledUnit(defaultServiceUser, cliExecStart, "")
	got := refreshedUnit(installed, binaryAlways(true))

	if !strings.Contains(got, cliExecStart) {
		t.Errorf("CLI ExecStart not preserved\n%s", got)
	}
	if strings.Contains(got, packagedExecStartLine()) {
		t.Errorf("packaging ExecStart must not replace the CLI one\n%s", got)
	}
	for _, line := range strings.Split(packagedSystemdUnit, "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		if !strings.Contains(got, line) {
			t.Errorf("refreshed unit lost packaged line %q", line)
		}
	}
}

func TestRefreshedUnit_PreservesWorkingDirectory(t *testing.T) {
	installed := cliInstalledUnit(defaultServiceUser, cliExecStart, "WorkingDirectory=/opt/senhub/bin")
	got := refreshedUnit(installed, binaryAlways(true))
	if !strings.Contains(got, "\nWorkingDirectory=/opt/senhub/bin\n") {
		t.Errorf("WorkingDirectory not preserved alongside the CLI ExecStart\n%s", got)
	}
}

// An ExecStart whose binary vanished (installer invoked from /tmp, #576)
// is repointed at the staged managed binary, keeping the rendered
// arguments — refresh-unit stays the documented repair for 203/EXEC.
func TestRefreshedUnit_MissingBinaryRepointsAtStagedBinary(t *testing.T) {
	installed := cliInstalledUnit(defaultServiceUser,
		`ExecStart=/tmp/senhub-agent "run" "--config-path" "/custom/agent.yaml"`,
		"WorkingDirectory=/tmp")
	got := refreshedUnit(installed, binaryAlways(false))

	want := `ExecStart=/var/lib/senhub-agent/bin/senhub-agent "run" "--config-path" "/custom/agent.yaml"`
	if !strings.Contains(got, want) {
		t.Errorf("expected repointed ExecStart %q\n%s", want, got)
	}
	if strings.Contains(got, "/tmp") {
		t.Errorf("vanished /tmp path must not survive the refresh\n%s", got)
	}
}

func TestRefreshedUnit_MissingBinaryNoArgsFallsBackToCanonical(t *testing.T) {
	installed := cliInstalledUnit(defaultServiceUser, "ExecStart=/tmp/senhub-agent", "")
	if got := refreshedUnit(installed, binaryAlways(false)); got != packagedSystemdUnit {
		t.Errorf("argument-less vanished ExecStart must fall back to the packaged unit\n%s", got)
	}
}

func TestRefreshedUnit_NoExecStartFallsBackToCanonical(t *testing.T) {
	installed := "[Service]\nUser=senhub\nGroup=senhub\n"
	if got := refreshedUnit(installed, binaryAlways(true)); got != packagedSystemdUnit {
		t.Errorf("unit without ExecStart must refresh to the packaged unit\n%s", got)
	}
}

// A legacy root install keeps both its root identity (#575) and its
// existing ExecStart (#396) through a refresh.
//
// Root identity is now expressed the way `install --user root` expresses
// it — by the ABSENCE of a User= directive, systemd's default being root
// — rather than by an explicit User=root on the hardened unit. That
// change is the point of #689: the hardened unit drops every capability,
// so writing it over a --user root install disarmed the very thing that
// install was chosen for.
func TestRefreshedUnit_RootInstallKeepsRootUserAndExecStart(t *testing.T) {
	execLine := `ExecStart=/usr/local/bin/senhub-agent "run" "--config-path" "/etc/senhub-agent/agent.yaml"`
	installed := cliInstalledUnit(rootServiceUser, execLine, "")
	got := refreshedUnit(installed, binaryAlways(true))

	if strings.Contains(got, "User=") || strings.Contains(got, "Group=") {
		t.Errorf("a root unit carries no User=/Group= — root is systemd's default\n%s", got)
	}
	if strings.Contains(got, "AmbientCapabilities=") || strings.Contains(got, "CapabilityBoundingSet=") {
		t.Errorf("refresh must not strip a root install of its capabilities (#689)\n%s", got)
	}
	if !strings.Contains(got, execLine) {
		t.Errorf("root install ExecStart not preserved\n%s", got)
	}
}

func TestSplitExecStartLine(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantBin  string
		wantArgs string
	}{
		{"path only", "ExecStart=/usr/bin/senhub-agent", "/usr/bin/senhub-agent", ""},
		{"quoted args", `ExecStart=/opt/a "run" "--verbose"`, "/opt/a", `"run" "--verbose"`},
		{"plain args", "ExecStart=/opt/a run --config-path /etc/a.yaml", "/opt/a", "run --config-path /etc/a.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin, args := splitExecStartLine(tc.line)
			if bin != tc.wantBin || args != tc.wantArgs {
				t.Errorf("splitExecStartLine(%q) = (%q, %q), want (%q, %q)", tc.line, bin, args, tc.wantBin, tc.wantArgs)
			}
		})
	}
}

func TestUnescapeSystemdPath(t *testing.T) {
	if got := unescapeSystemdPath(`/opt/my\x20dir/agent`); got != "/opt/my dir/agent" {
		t.Errorf("unescapeSystemdPath = %q", got)
	}
}
