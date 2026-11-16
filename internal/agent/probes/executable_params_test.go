package probes_test

import (
	"regexp"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/spec"
)

var programKey = regexp.MustCompile(`(?i)(^command$|_path$|^executable|binary|script)`)

// A console running with allow_exec off refuses the parameters flagged
// Executable and nothing else, so a new parameter naming a program that
// is not flagged would slip past it.
func TestProbeSpecs_ProgramParametersAreFlaggedExecutable(t *testing.T) {
	var walk func(probe string, ps []probes.ParamSpec)
	walk = func(probe string, ps []probes.ParamSpec) {
		for _, p := range ps {
			names := programKey.MatchString(p.Key) &&
				(p.Key == "command" || strings.Contains(strings.ToLower(p.Description), "binary") ||
					strings.Contains(strings.ToLower(p.Description), "program"))
			if names && !p.Executable {
				t.Errorf("%s: parameter %q names a program but is not flagged Executable", probe, p.Key)
			}
			if p.Executable && p.Kind != probes.KindString {
				t.Errorf("%s: Executable parameter %q must be a string", probe, p.Key)
			}
			walk(probe, p.Fields)
		}
	}
	for _, s := range spec.Registered() {
		walk(s.Type, s.Params)
	}
}

func TestProbeSpecs_ExecutablePathsReportsDefaults(t *testing.T) {
	smart, ok := spec.For("smart")
	if !ok {
		t.Fatal("smart spec not registered")
	}
	if got := smart.ExecutablePaths()["smartctl_path"]; got != "smartctl" {
		t.Errorf("smartctl_path default = %q, want smartctl", got)
	}
	ex, _ := spec.For("exec")
	if _, has := ex.ExecutablePaths()["command"]; !has {
		t.Error("exec command is not flagged Executable")
	}
}
