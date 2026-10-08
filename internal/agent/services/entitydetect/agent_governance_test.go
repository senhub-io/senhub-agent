package entitydetect

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// The agent-level governance block describes the host whichever way
// detection was switched on, and a change to it is carried by the same
// reconfiguration that follows any other edit of the file.
func TestAgentGovernanceReachesTheResolvedSettingsAndReloads(t *testing.T) {
	svc := New(Resolve(nil, nil, "agent-1").WithAgentGovernance(map[string]interface{}{"criticality": "low"}),
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if got := svc.cfg.Governance.Criticality; got != "low" {
		t.Fatalf("criticality at start = %q, want low", got)
	}

	reloaded := Resolve(nil, nil, "agent-1").WithAgentGovernance(map[string]interface{}{
		"criticality": "critical",
		"owner":       map[interface{}]interface{}{"team": "sre"},
	})
	if err := svc.Reconfigure(reloaded); err != nil {
		t.Fatalf("reconfigure: %v", err)
	}
	attrs := svc.cfg.Governance.Attributes()
	if attrs["service.criticality"] != "critical" || attrs["entity.owner.team"] != "sre" {
		t.Errorf("a reload must carry the new governance into the settings, got %v", attrs)
	}

	invalid := Resolve(nil, nil, "agent-1").WithAgentGovernance(map[string]interface{}{"criticality": "urgent"})
	if !invalid.Governance.IsZero() {
		t.Error("an invalid block must not be half applied")
	}
}

// With a global `entities:` block the OTLP fallback is not read, so the
// agent-level block is the only source of host governance: it must still
// land in the resolved settings.
func TestAgentGovernanceCoversTheGlobalEntitiesBranch(t *testing.T) {
	global := &configuration.EntitiesConfig{Enabled: true}
	storage := []configuration.StorageConfig{{Name: "otlp", Params: map[string]interface{}{
		"signals": map[string]interface{}{"entities": map[string]interface{}{
			"enabled":    true,
			"governance": map[string]interface{}{"criticality": "low"},
		}},
	}}}
	block := map[string]interface{}{"criticality": "high"}

	cfg := Resolve(global, storage, "agent-1").WithAgentGovernance(block)
	if !cfg.Enabled {
		t.Fatal("global block must enable detection")
	}
	if got := cfg.Governance.Criticality; got != "high" {
		t.Errorf("host criticality = %q, want high from the agent block", got)
	}

	fallback := Resolve(nil, storage, "agent-1").WithAgentGovernance(block)
	if got := fallback.Governance.Criticality; got != "high" {
		t.Errorf("fallback branch: host criticality = %q, want high (agent block wins)", got)
	}
}
