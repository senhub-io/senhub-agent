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

	// Refreshing a root install must NOT reintroduce User=senhub — that
	// is the 217/USER crash-loop the fix prevents (#575).
	root := canonicalUnitForUser(rootServiceUser)
	if !strings.Contains(root, "User=root") || !strings.Contains(root, "Group=root") {
		t.Errorf("root unit must run as root\n%s", root)
	}
	if strings.Contains(root, "User=senhub") || strings.Contains(root, "Group=senhub") {
		t.Errorf("root unit must not reference the senhub user\n%s", root)
	}
	// Every hardening directive of the packaged unit survives — only
	// User=/Group= are re-templated.
	for _, line := range strings.Split(packagedSystemdUnit, "\n") {
		if strings.HasPrefix(line, "User=") || strings.HasPrefix(line, "Group=") {
			continue
		}
		if !strings.Contains(root, line) {
			t.Errorf("root unit lost packaged line %q", line)
		}
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
func TestRefreshedUnit_RootInstallKeepsRootUserAndExecStart(t *testing.T) {
	execLine := `ExecStart=/usr/local/bin/senhub-agent "run" "--config-path" "/etc/senhub-agent/agent.yaml"`
	installed := cliInstalledUnit(rootServiceUser, execLine, "")
	got := refreshedUnit(installed, binaryAlways(true))

	if !strings.Contains(got, "User=root") || !strings.Contains(got, "Group=root") {
		t.Errorf("root install must stay root\n%s", got)
	}
	if strings.Contains(got, "User=senhub") || strings.Contains(got, "Group=senhub") {
		t.Errorf("refresh must not switch a root install to the senhub user (#575)\n%s", got)
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
