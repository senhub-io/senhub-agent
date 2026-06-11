package app

import (
	"os"
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
