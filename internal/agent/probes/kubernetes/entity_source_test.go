package kubernetes

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

func TestK8sEntitySource_MonitorsEdge(t *testing.T) {
	src := newK8sEntitySource("https://api.cluster.local:6443")
	wantID := "kubernetes://https://api.cluster.local:6443"

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
			if r.FromID["service.instance.id"] != "agent-key" {
				t.Errorf("monitors From = %v, want agent-key", r.FromID)
			}
			if r.ToID["service.instance.id"] != wantID {
				t.Errorf("monitors ToID must match the cluster identity %q, got %v", wantID, r.ToID)
			}
		}
		if !found {
			t.Errorf("no monitors edge: %+v", obs.Relations)
		}
	})

	t.Run("skipped without agent id", func(t *testing.T) {
		agentstate.SetAgentInstanceID("")
		obs, _ := src.Observe()
		// Remote API server — neither a monitors nor a runs_on edge.
		for _, ty := range k8sRelTypes(obs) {
			if ty == "monitors" {
				t.Errorf("no monitors edge expected without agent id, got %+v", obs.Relations)
			}
		}
	})
}

// TestK8sEntitySource_LocalRunsOn exercises the runs_on wiring. The cluster id is
// "kubernetes://<endpoint>", which embeds the API server host, so the LocalRunsOn
// collapse guard refuses the edge even for a loopback API server — a
// loopback-derived id is not host-unique and must not anchor a host. A remote
// API server yields no edge either. The probe is wired for consistency; the gate
// guarantees correctness.
func TestK8sEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback API server, but the id embeds "127.0.0.1" — the guard refuses runs_on.
	local := newK8sEntitySource("127.0.0.1:6443")
	local.hostID = "H"
	obs, _ := local.Observe()
	for _, ty := range k8sRelTypes(obs) {
		if ty == "runs_on" {
			t.Errorf("loopback-embedding id must NOT emit runs_on (collapse guard); relations=%v", k8sRelTypes(obs))
		}
	}

	// Remote API server — no runs_on.
	remote := newK8sEntitySource("api.cluster.local:6443")
	remote.hostID = "H"
	robs, _ := remote.Observe()
	for _, ty := range k8sRelTypes(robs) {
		if ty == "runs_on" {
			t.Errorf("remote cluster must NOT emit runs_on; relations=%v", k8sRelTypes(robs))
		}
	}
}

func k8sRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}
