package proxmox

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestEntitySource_LocalRunsOn verifies a loopback-monitored PVE surface emits a
// runs_on→host edge, while a remote one does not.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	log := logger.NewModuleLogger(testLogger(), "test")

	local := newProxmoxEntitySource(probeConfig{Endpoint: "https://127.0.0.1:8006"}, log)
	local.hostID = "H"
	local.refresh("test-cluster")
	obs, _ := local.Observe()
	runsOn := proxmoxRelByType(obs, "runs_on")
	if runsOn == nil {
		t.Fatalf("loopback proxmox: expected a runs_on edge, got %v", proxmoxRelTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "proxmox:test-cluster" {
		t.Errorf("runs_on source = %v, want proxmox:test-cluster", runsOn.FromID)
	}

	remote := newProxmoxEntitySource(probeConfig{Endpoint: "https://10.0.0.5:8006"}, log)
	remote.hostID = "H"
	remote.refresh("test-cluster")
	robs, _ := remote.Observe()
	if proxmoxRelByType(robs, "runs_on") != nil {
		t.Errorf("remote proxmox must NOT emit runs_on; relations=%v", proxmoxRelTypes(robs))
	}
}

func proxmoxRelByType(obs entity.Observation, ty string) *entity.Relation {
	for i := range obs.Relations {
		if obs.Relations[i].Type == ty {
			return &obs.Relations[i]
		}
	}
	return nil
}

func proxmoxRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}
