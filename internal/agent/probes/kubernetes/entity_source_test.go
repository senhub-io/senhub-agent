package kubernetes

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

func TestK8sEntitySource_MonitorsEdge(t *testing.T) {
	src := newK8sEntitySource("https://api.cluster.local:6443")
	src.setClusterIdentity("https://api.cluster.local:6443", "uid-kube-system")
	wantID := "uid-kube-system"

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

// TestK8sEntitySource_LocalRunsOn: the cluster id is the kube-system UID, not
// an address, so a loopback API server anchors the cluster to this host; a
// remote API server yields no edge.
func TestK8sEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	hasRunsOn := func(endpoint string) bool {
		src := newK8sEntitySource(endpoint)
		src.setClusterIdentity(endpoint, "uid-1")
		src.hostID = "H"
		obs, _ := src.Observe()
		for _, ty := range k8sRelTypes(obs) {
			if ty == "runs_on" {
				return true
			}
		}
		return false
	}
	if !hasRunsOn("127.0.0.1:6443") {
		t.Error("loopback API server must anchor the cluster to this host")
	}
	if hasRunsOn("api.cluster.local:6443") {
		t.Error("remote cluster must NOT emit runs_on")
	}
}

// TestK8sEntitySource_NoUIDNoClusterEntity: an unreadable kube-system UID
// means no cluster entity and no edge: the address is never an identity.
func TestK8sEntitySource_NoUIDNoClusterEntity(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newK8sEntitySource("api.cluster.local:6443")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("no UID must yield no cluster entity or edge, got %+v", obs)
	}
}

func k8sRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}
