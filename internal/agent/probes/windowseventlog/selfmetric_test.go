package windowseventlog

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// Regression for #701: the conduit's throughput self-metric must route to
// the metric sinks. windows_eventlog used to return []string{}, so a
// datapoint from Collect would have been dropped to no sink. The Windows
// increment path itself is verified on the Windows recette host; these
// cross-platform assertions pin the routing + Collect surface.
func TestWindowsEventLog_SelfMetricRoutesToOTLP(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	probe, err := NewWindowsEventLogProbe(map[string]interface{}{
		"channels": []interface{}{"System"},
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewWindowsEventLogProbe: %v", err)
	}
	p := probe.(*WindowsEventLogProbe)

	has := false
	for _, s := range p.GetTargetStrategies() {
		if s == "otlp" {
			has = true
		}
	}
	if !has {
		t.Errorf("GetTargetStrategies %v must include otlp", p.GetTargetStrategies())
	}

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	found := false
	for _, dp := range pts {
		if dp.Name == "senhub.windows_eventlog.records_emitted" {
			found = true
		}
	}
	if !found {
		t.Errorf("Collect did not emit senhub.windows_eventlog.records_emitted; got %+v", pts)
	}
}
