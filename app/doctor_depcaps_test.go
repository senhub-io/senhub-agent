package app

import (
	"errors"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func dependsOnConfig(enabled bool) doctorConfig {
	return doctorConfig{data: configuration.LocalConfigurationData{
		Entities: &configuration.EntitiesConfig{
			Enabled:   true,
			DependsOn: &configuration.EntitiesDependsOnConfig{Enabled: enabled},
		},
	}}
}

func TestDependsOnCapsCheck(t *testing.T) {
	const unitNoCaps = "[Service]\nUser=senhub\nAmbientCapabilities=\n"
	const grant = "[Service]\nAmbientCapabilities=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE\n"

	t.Run("silent when depends_on is off", func(t *testing.T) {
		if c := dependsOnCapsCheck(doctorDeps{}, dependsOnConfig(false), unitNoCaps, nil, "senhub"); c != nil {
			t.Errorf("got %+v, want no check", c)
		}
	})
	t.Run("silent when the unit was not read", func(t *testing.T) {
		if c := dependsOnCapsCheck(doctorDeps{}, dependsOnConfig(true), "", errors.New("no unit"), "senhub"); c != nil {
			t.Errorf("got %+v, want no check", c)
		}
	})
	t.Run("warns for a service account without the capabilities", func(t *testing.T) {
		c := dependsOnCapsCheck(doctorDeps{}, dependsOnConfig(true), unitNoCaps, nil, "senhub")
		if c == nil || c.Level != levelWarn {
			t.Fatalf("got %+v, want a warning", c)
		}
		if !strings.Contains(c.Message, "CAP_DAC_READ_SEARCH and CAP_SYS_PTRACE") || !strings.Contains(c.Fix, "AmbientCapabilities=") {
			t.Errorf("message %q fix %q", c.Message, c.Fix)
		}
	})
	t.Run("a drop-in that grants both makes it ok", func(t *testing.T) {
		d := doctorDeps{install: installProbe{dropIns: func() []string { return []string{grant} }}}
		c := dependsOnCapsCheck(d, dependsOnConfig(true), unitNoCaps, nil, "senhub")
		if c == nil || c.Level != levelOK {
			t.Fatalf("got %+v, want ok", c)
		}
	})
	t.Run("one missing capability is named", func(t *testing.T) {
		d := doctorDeps{install: installProbe{dropIns: func() []string { return []string{"[Service]\nAmbientCapabilities=CAP_SYS_PTRACE\n"} }}}
		c := dependsOnCapsCheck(d, dependsOnConfig(true), unitNoCaps, nil, "senhub")
		if c == nil || c.Level != levelWarn || !strings.Contains(c.Message, "lacks CAP_DAC_READ_SEARCH:") {
			t.Fatalf("got %+v", c)
		}
	})
	t.Run("a later empty assignment resets the grant", func(t *testing.T) {
		d := doctorDeps{install: installProbe{dropIns: func() []string { return []string{grant, "[Service]\nAmbientCapabilities=\n"} }}}
		if c := dependsOnCapsCheck(d, dependsOnConfig(true), unitNoCaps, nil, "senhub"); c == nil || c.Level != levelWarn {
			t.Fatalf("got %+v, want a warning", c)
		}
	})
	t.Run("root needs nothing", func(t *testing.T) {
		c := dependsOnCapsCheck(doctorDeps{}, dependsOnConfig(true), unitNoCaps, nil, rootServiceUser)
		if c == nil || c.Level != levelOK {
			t.Fatalf("got %+v, want ok", c)
		}
	})
}
