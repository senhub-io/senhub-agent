package modbus

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

func TestModbusEntitySource_MonitorsEdge(t *testing.T) {
	src := newModbusEntitySource("modbus://10.0.0.5:502", "10.0.0.5")
	src.markLive()

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
			if r.ToID["service.instance.id"] != "modbus://10.0.0.5:502" {
				t.Errorf("monitors ToID must match the target identity, got %v", r.ToID)
			}
		}
		if !found {
			t.Errorf("no monitors edge: %+v", obs.Relations)
		}
	})

	t.Run("skipped without agent id", func(t *testing.T) {
		agentstate.SetAgentInstanceID("")
		obs, _ := src.Observe()
		// Remote target (10.0.0.5) — neither a monitors nor a runs_on edge.
		for _, ty := range modbusRelTypes(obs) {
			if ty == "monitors" {
				t.Errorf("no monitors edge expected without agent id, got %+v", obs.Relations)
			}
		}
	})
}

// TestModbusEntitySource_LocalRunsOn exercises the runs_on wiring. The modbus id
// is "modbus://<host>:<port>", which embeds the monitored host, so the
// LocalRunsOn collapse guard refuses the edge even for a loopback gateway — a
// loopback-derived id is not host-unique and must not anchor a host. A remote
// gateway yields no edge either. The probe is wired for consistency; the gate
// guarantees correctness.
func TestModbusEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback gateway, but the id embeds "127.0.0.1" — the guard refuses runs_on.
	local := newModbusEntitySource("modbus://127.0.0.1:502", "127.0.0.1")
	local.hostID = func() string { return "H" }
	local.markLive()
	obs, _ := local.Observe()
	for _, ty := range modbusRelTypes(obs) {
		if ty == "runs_on" {
			t.Errorf("loopback-embedding id must NOT emit runs_on (collapse guard); relations=%v", modbusRelTypes(obs))
		}
	}

	// Remote gateway — no runs_on.
	remote := newModbusEntitySource("modbus://10.0.0.5:502", "10.0.0.5")
	remote.hostID = func() string { return "H" }
	remote.markLive()
	robs, _ := remote.Observe()
	for _, ty := range modbusRelTypes(robs) {
		if ty == "runs_on" {
			t.Errorf("remote gateway must NOT emit runs_on; relations=%v", modbusRelTypes(robs))
		}
	}
}

func modbusRelTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}
