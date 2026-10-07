package entitydetect

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
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
