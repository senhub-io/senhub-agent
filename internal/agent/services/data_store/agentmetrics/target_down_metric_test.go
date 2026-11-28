package agentmetrics

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

func TestBuildAgentRecords_TargetDownCount(t *testing.T) {
	agentstate.SetActiveProbes([]string{"p1"})
	agentstate.RecordProbeTarget("p1", true, "timeout")
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
	})

	snap := AgentMetricsSnapshot{
		StartTime:    time.Now(),
		BuildVersion: "1.2.3",
	}
	recs := BuildAgentRecords(snap)
	var rec *otelmapper.OtelRecord
	for i := range recs {
		if recs[i].Name == "senhub.agent.probes.target_down" {
			rec = &recs[i]
			break
		}
	}
	if rec == nil {
		t.Fatal("record senhub.agent.probes.target_down not found")
	}
	if rec.Value != 1 {
		t.Fatalf("Value = %f, want 1", rec.Value)
	}
}
