package agentmetrics

import (
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/exporthook"
)

// TestBuildAgentRecords_AddsProviderRecords checks the records of a
// registered provider ride with the agent's own metrics.
func TestBuildAgentRecords_AddsProviderRecords(t *testing.T) {
	var on atomic.Bool
	on.Store(true)
	// The registration outlives the test; the switch keeps it silent
	// for the tests that count records.
	defer on.Store(false)
	exporthook.RegisterSelfRecordProvider(func(time.Time) []exporthook.SelfRecord {
		if !on.Load() {
			return nil
		}
		return []exporthook.SelfRecord{{Name: "test.provider.records", Unit: "{series}", Type: "gauge", Value: 7,
			Attributes: map[string]string{"output": "otlp"}}}
	})

	recs := BuildAgentRecords(AgentMetricsSnapshot{StartTime: time.Now()})
	for _, r := range recs {
		if r.Name == "test.provider.records" {
			if r.Value != 7 || r.Attributes["output"] != "otlp" || r.Type != "gauge" {
				t.Errorf("provider record = %+v", r)
			}
			return
		}
	}
	t.Error("the provider record is missing from BuildAgentRecords")
}
