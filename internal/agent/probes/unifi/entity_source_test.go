package unifi

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestUnifiEntitySource_MonitorsEdge(t *testing.T) {
	src := newEntitySource("https://unifi.example.com:8443")
	src.markReachable(true)
	wantID := "unifi://https://unifi.example.com:8443"

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
		if len(obs.Relations) != 0 {
			t.Errorf("no edge expected without agent id, got %+v", obs.Relations)
		}
	})
}
