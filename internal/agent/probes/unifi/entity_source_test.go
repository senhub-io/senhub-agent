package unifi

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestUnifiEntitySource_MonitorsEdge(t *testing.T) {
	src := newEntitySource("https://unifi.example.com:8443")
	src.setControllerID("uuid-ctrl")
	src.markReachable(true)
	wantID := "uuid-ctrl"

	t.Run("emitted with agent id, ToID matches identity", func(t *testing.T) {
		agentstate.SetAgentInstanceID("agent-key")
		t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

		obs, ok := src.Observe()
		if !ok {
			t.Fatal("Observe() ok=false")
		}
		var found bool
		for _, r := range obs.Relations {
			if r.Type != "monitors" {
				continue
			}
			found = true
			if r.FromID[idKeyServiceInstanceID] != "agent-key" {
				t.Errorf("monitors From = %v, want agent-key", r.FromID)
			}
			if r.ToID[idKeyServiceInstanceID] != wantID {
				t.Errorf("monitors ToID must match the controller identity %q, got %v", wantID, r.ToID)
			}
		}
		if !found {
			t.Errorf("no monitors edge: %+v", obs.Relations)
		}
	})

	t.Run("skipped without agent id", func(t *testing.T) {
		agentstate.SetAgentInstanceID("")
		obs, _ := src.Observe()
		// Endpoint is remote here, so neither a monitors nor a runs_on edge is
		// present without an agent id.
		for _, r := range obs.Relations {
			t.Errorf("no edge expected without agent id, got %s", r.Type)
		}
	})
}

// TestUnifiEntitySource_LocalRunsOn: the id is no longer address-derived, so a
// loopback controller anchors to this host; a remote one never does.
func TestUnifiEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	hasRunsOn := func(endpoint string) bool {
		src := newEntitySource(endpoint)
		src.hostID = func() string { return "h-1" }
		src.setControllerID("uuid-ctrl")
		src.markReachable(true)
		obs, _ := src.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" {
				return true
			}
		}
		return false
	}
	if !hasRunsOn("https://localhost:8443") {
		t.Error("loopback controller must emit runs_on to the host")
	}
	if !hasRunsOn("https://127.0.0.1:8443") {
		t.Error("loopback IP controller must emit runs_on to the host")
	}
	if hasRunsOn("https://10.0.0.5:8443") {
		t.Error("remote controller must NOT emit runs_on")
	}
}
