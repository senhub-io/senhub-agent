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
