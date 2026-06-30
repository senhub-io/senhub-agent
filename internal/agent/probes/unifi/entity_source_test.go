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
		// Endpoint is remote here, so neither a monitors nor a runs_on edge is
		// present without an agent id.
		for _, r := range obs.Relations {
			t.Errorf("no edge expected without agent id, got %s", r.Type)
		}
	})
}

// TestUnifiEntitySource_LocalRunsOn: the service.instance.id is endpoint-derived
// ("unifi://<endpoint>"), so it embeds the host and is identical on every host.
// The collapse guard therefore refuses the runs_on even on a loopback endpoint
// (anchoring it would false-join hosts). A remote endpoint never anchors either.
// So no runs_on edge is ever emitted — wired for correctness, gate suppresses it.
func TestUnifiEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	hasRunsOn := func(endpoint string) bool {
		src := newEntitySource(endpoint)
		src.hostID = func() string { return "h-1" }
		src.markReachable(true)
		obs, _ := src.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" {
				return true
			}
		}
		return false
	}
	if hasRunsOn("https://localhost:8443") {
		t.Error("endpoint-derived id must NOT emit runs_on on loopback (collapse guard)")
	}
	if hasRunsOn("https://127.0.0.1:8443") {
		t.Error("endpoint-derived id must NOT emit runs_on on loopback IP (collapse guard)")
	}
	if hasRunsOn("https://10.0.0.5:8443") {
		t.Error("remote controller must NOT emit runs_on→host")
	}
}
