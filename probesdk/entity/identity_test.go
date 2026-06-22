package entity

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestAgentInstanceID mirrors the internal agentstate value so an SDK probe can
// resolve the From of its monitors edge.
func TestAgentInstanceID(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	if got := AgentInstanceID(); got != "" {
		t.Errorf("AgentInstanceID() = %q, want empty when emission is off", got)
	}

	agentstate.SetAgentInstanceID("agent-key-42")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })
	if got := AgentInstanceID(); got != "agent-key-42" {
		t.Errorf("AgentInstanceID() = %q, want agent-key-42", got)
	}
}

// TestHostID is a smoke test: HostID must not panic and returns a string (the
// value is the real machine-id, which may be "" in a minimal CI container).
func TestHostID(t *testing.T) {
	_ = HostID()
}
