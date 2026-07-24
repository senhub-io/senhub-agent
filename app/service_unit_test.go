package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// TestEmbeddedUnitMatchesPackagedUnit pins the embedded copy of the
// hardened unit byte-for-byte against the canonical file the .deb/.rpm
// packages ship. go:embed cannot reach ../packaging from this package,
// so the file is duplicated and this test is the anti-drift guard: any
// edit to packaging/systemd/senhub-agent.service must be mirrored in
// app/senhub-agent.service (and vice versa).
func TestEmbeddedUnitMatchesPackagedUnit(t *testing.T) {
	packaged, err := os.ReadFile("../packaging/systemd/senhub-agent.service")
	if err != nil {
		t.Fatalf("reading packaged unit: %v", err)
	}
	if string(packaged) != packagedSystemdUnit {
		t.Fatal("app/senhub-agent.service differs from packaging/systemd/senhub-agent.service — keep the two copies identical")
	}
}

// renderSystemdScript executes the install-time template the same way
// kardianos/service does (same template funcs, same field names), so
// the assertions below see the exact unit `agent install` writes.
func renderSystemdScript(t *testing.T, script string, path string, arguments []string, workingDirectory string) string {
	t.Helper()
	funcs := template.FuncMap{
		"cmd": func(s string) string {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		},
		"cmdEscape": func(s string) string {
			return strings.ReplaceAll(s, " ", `\x20`)
		},
	}
	tmpl, err := template.New("").Funcs(funcs).Parse(script)
	if err != nil {
		t.Fatalf("parsing systemd script template: %v", err)
	}
	data := struct {
		Path             string
		Arguments        []string
		WorkingDirectory string
	}{path, arguments, workingDirectory}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		t.Fatalf("executing systemd script template: %v", err)
	}
	return sb.String()
}

func TestHardenedSystemdScript_DefaultUserAndExecStart(t *testing.T) {
	unit := renderSystemdScript(t,
		hardenedSystemdScript(defaultServiceUser),
		"/opt/senhub/bin/senhub-agent",
		[]string{"run", "--config-path", "/etc/senhub-agent/agent.yaml"},
		"/opt/senhub/bin",
	)

	for _, want := range []string{
		"User=senhub",
		"Group=senhub",
		`ExecStart=/opt/senhub/bin/senhub-agent "run" "--config-path" "/etc/senhub-agent/agent.yaml"`,
		"WorkingDirectory=/opt/senhub/bin",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("rendered unit missing %q\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "User=root") {
		t.Errorf("rendered unit must not run as root\n%s", unit)
	}
}

// TestHardenedSystemdScript_KeepsPackagedDirectives asserts every
// directive of the packaged hardened unit survives in the unit the CLI
// installer writes — only ExecStart/User/Group are re-templated. This
// covers the capability posture (CapabilityBoundingSet= /
// AmbientCapabilities= empty, per-probe re-grants left as comments for
// snmp_trap UDP/162 and ICMP raw sockets) exactly as #223 shipped it.
func TestHardenedSystemdScript_KeepsPackagedDirectives(t *testing.T) {
	unit := renderSystemdScript(t,
		hardenedSystemdScript(defaultServiceUser),
		"/usr/bin/senhub-agent",
		[]string{"run"},
		"/usr/bin",
	)

	for _, line := range strings.Split(packagedSystemdUnit, "\n") {
		switch {
		case strings.HasPrefix(line, "ExecStart="),
			strings.HasPrefix(line, "User="),
			strings.HasPrefix(line, "Group="):
			continue
		}
		if !strings.Contains(unit, line) {
			t.Errorf("rendered unit lost packaged line %q", line)
		}
	}
}

// unitSections groups the non-comment directives of a unit file by the
// [Section] they appear under.
func unitSections(unit string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			current = trimmed
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		default:
			sections[current] = append(sections[current], trimmed)
		}
	}
	return sections
}

func sectionHasDirective(directives []string, prefix string) bool {
	for _, d := range directives {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

// systemd only honours StartLimitIntervalSec/StartLimitBurst in [Unit];
// placed in [Service] they are silently ignored ("Unknown key ...,
// ignoring") and the crash-loop throttling never applies (#577).
func TestPackagedUnit_StartLimitDirectivesLiveInUnitSection(t *testing.T) {
	sections := unitSections(packagedSystemdUnit)
	for _, directive := range []string{"StartLimitIntervalSec=", "StartLimitBurst="} {
		if !sectionHasDirective(sections["[Unit]"], directive) {
			t.Errorf("%s missing from [Unit]", directive)
		}
		if sectionHasDirective(sections["[Service]"], directive) {
			t.Errorf("%s must not appear in [Service] — systemd ignores it there (#577)", directive)
		}
	}
}

func TestHardenedSystemdScript_StartLimitDirectivesLiveInUnitSection(t *testing.T) {
	unit := renderSystemdScript(t,
		hardenedSystemdScript(defaultServiceUser),
		"/var/lib/senhub-agent/bin/senhub-agent",
		[]string{"run", "--config-path", "/etc/senhub-agent/agent.yaml"},
		"/var/lib/senhub-agent/bin",
	)
	sections := unitSections(unit)
	for _, directive := range []string{"StartLimitIntervalSec=", "StartLimitBurst="} {
		if !sectionHasDirective(sections["[Unit]"], directive) {
			t.Errorf("%s missing from [Unit] in the installed unit", directive)
		}
		if sectionHasDirective(sections["[Service]"], directive) {
			t.Errorf("%s must not appear in [Service] in the installed unit (#577)", directive)
		}
	}
}

// The packaged ExecStart must reference the staged managed binary — the
// path install stages to and auto-update replaces in place — never an
// invocation path that can vanish (203/EXEC, #576). This also keeps
// package installs and refresh-unit's fallback path converged (#396).
func TestPackagedUnit_ExecStartPointsAtManagedBinary(t *testing.T) {
	binPath, _ := splitExecStartLine(packagedExecStartLine())
	want := filepath.Join(managedBinaryDir, "senhub-agent")
	if binPath != want {
		t.Errorf("packaged ExecStart binary = %q, want the staged managed path %q", binPath, want)
	}
}

// `install --user root` must not fall through to kardianos's built-in
// systemd script — that script emits StartLimitInterval/StartLimitBurst
// inside [Service] under their legacy names, which is exactly the
// misplacement #577 fixes.
func TestLinuxSystemdScript_SelectsTemplatePerUser(t *testing.T) {
	if got := linuxSystemdScript(rootServiceUser); got != rootSystemdScript {
		t.Error("linuxSystemdScript(root) must return the corrected root template")
	}
	if got := linuxSystemdScript(defaultServiceUser); got != hardenedSystemdScript(defaultServiceUser) {
		t.Error("linuxSystemdScript(senhub) must return the hardened template")
	}
}

func TestRootSystemdScript_StartLimitDirectivesLiveInUnitSection(t *testing.T) {
	unit := renderSystemdScript(t,
		rootSystemdScript,
		filepath.Join(managedBinaryDir, "senhub-agent"),
		[]string{"run", "--config-path", "/etc/senhub-agent/agent-config.yaml"},
		managedBinaryDir,
	)
	sections := unitSections(unit)
	for _, directive := range []string{"StartLimitIntervalSec=", "StartLimitBurst="} {
		if !sectionHasDirective(sections["[Unit]"], directive) {
			t.Errorf("%s missing from [Unit] in the root unit", directive)
		}
		if sectionHasDirective(sections["[Service]"], directive) {
			t.Errorf("%s must not appear in [Service] in the root unit (#577)", directive)
		}
	}
	// The legacy pre-v230 spelling kardianos's default script used must
	// not resurface anywhere ("StartLimitInterval=" does not prefix-match
	// the modern "StartLimitIntervalSec=").
	for _, directives := range sections {
		if sectionHasDirective(directives, "StartLimitInterval=") {
			t.Errorf("legacy StartLimitInterval= directive present in the root unit\n%s", unit)
		}
	}
}

// The root unit runs as root through systemd's implicit default: no
// User=/Group= directive at all, so there is no user to resolve and no
// 217/USER failure mode — and none of the hardened unit's capability
// drops, which would defeat the point of --user root (raw sockets).
func TestRootSystemdScript_NoUserDirectiveAndNoCapabilityDrops(t *testing.T) {
	unit := renderSystemdScript(t,
		rootSystemdScript,
		filepath.Join(managedBinaryDir, "senhub-agent"),
		[]string{"run"},
		managedBinaryDir,
	)
	for _, line := range strings.Split(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, forbidden := range []string{"User=", "Group=", "CapabilityBoundingSet=", "AmbientCapabilities="} {
			if strings.HasPrefix(trimmed, forbidden) {
				t.Errorf("root unit must not contain %q directive: %q", forbidden, trimmed)
			}
		}
	}
	if !strings.Contains(unit, "\nRestart=always\n") {
		t.Errorf("root unit must keep Restart=always (#567)\n%s", unit)
	}
}

// After install stages the binary (#576), the unit rendered for BOTH
// service users references the staged /var/lib path — never the
// installer's invocation path.
func TestInstallUnit_ExecStartPointsAtStagedBinary_BothUsers(t *testing.T) {
	staged := filepath.Join(managedBinaryDir, "senhub-agent")
	args := []string{"run", "--config-path", "/etc/senhub-agent/agent-config.yaml"}
	for _, user := range []string{defaultServiceUser, rootServiceUser} {
		t.Run(user, func(t *testing.T) {
			unit := renderSystemdScript(t, linuxSystemdScript(user), staged, args, managedBinaryDir)
			execLine, workDir := installedExecStart(unit)
			binPath, _ := splitExecStartLine(execLine)
			if binPath != staged {
				t.Errorf("ExecStart binary = %q, want the staged managed path %q", binPath, staged)
			}
			if workDir != "WorkingDirectory="+managedBinaryDir {
				t.Errorf("WorkingDirectory = %q, want %q", workDir, "WorkingDirectory="+managedBinaryDir)
			}
		})
	}
}

func TestHardenedSystemdScript_CustomUser(t *testing.T) {
	unit := renderSystemdScript(t,
		hardenedSystemdScript("monitor"),
		"/usr/bin/senhub-agent",
		[]string{"run"},
		"/usr/bin",
	)
	if !strings.Contains(unit, "User=monitor") || !strings.Contains(unit, "Group=monitor") {
		t.Errorf("custom service user not applied\n%s", unit)
	}
	if strings.Contains(unit, "User=senhub") {
		t.Errorf("default user leaked into custom-user unit\n%s", unit)
	}
}
