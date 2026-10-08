package configuration

import (
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestHasConfigurationChangedSeesReloadedSections(t *testing.T) {
	zl := zerolog.Nop()
	base := (*logger.Logger)(&zl)
	lc := &LocalConfiguration{logger: logger.NewModuleLogger(base, "configuration.local.test")}

	build := func() LocalConfigurationData {
		return LocalConfigurationData{
			Governance: map[string]interface{}{"criticality": "high"},
			Entities:   &EntitiesConfig{Enabled: true},
			Agent:      LocalAgentConfig{GlobalTags: map[string]string{"site": "a"}},
		}
	}

	if lc.hasConfigurationChanged(build(), build()) {
		t.Fatal("identical content must not be reported as changed")
	}

	gov := build()
	gov.Governance = map[string]interface{}{"criticality": "low"}
	if !lc.hasConfigurationChanged(build(), gov) {
		t.Error("a governance-only change must be detected")
	}

	ent := build()
	ent.Entities = &EntitiesConfig{Enabled: false}
	if !lc.hasConfigurationChanged(build(), ent) {
		t.Error("an entities-only change must be detected")
	}

	tags := build()
	tags.Agent.GlobalTags = map[string]string{"site": "b"}
	if !lc.hasConfigurationChanged(build(), tags) {
		t.Error("a global_tags-only change must be detected")
	}
}
