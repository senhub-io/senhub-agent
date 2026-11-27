package probes

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type checkingProbe struct {
	*types.BaseProbe
	value float64
}

func (c *checkingProbe) ShouldStart() bool           { return true }
func (c *checkingProbe) GetInterval() time.Duration  { return time.Second }
func (c *checkingProbe) OnStart(chan struct{}) error { return nil }
func (c *checkingProbe) OnShutdown(context.Context) error {
	return nil
}
func (c *checkingProbe) EntitySource() entity.Source { return types.NoOpEntitySource{} }
func (c *checkingProbe) Collect() ([]datapoint.DataPoint, error) {
	return []datapoint.DataPoint{{Name: dbcommon.CheckResultMetric, Value: c.value, Timestamp: time.Now()}}, nil
}

func TestCollectRecordsTheLastCheckOutcome(t *testing.T) {
	agentstate.SetActiveProbes([]string{"target-test"})
	defer agentstate.SetActiveProbes(nil)

	base := &types.BaseProbe{}
	base.SetName("checking")
	base.SetProbeType("checking_stub")
	probe := &checkingProbe{BaseProbe: base, value: 1}
	poller := &ProbePoller{
		ProbeId:      "target-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
			return nil
		},
	}

	// A successful cycle reporting a failing check must flag the target as down.
	if err := poller.collect(context.Background()); err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	state := agentstate.GetProbeRunState("target-test")
	if !state.TargetDown {
		t.Fatal("TargetDown must be true when check result is 1")
	}
	if state.TargetError != "target_down" {
		t.Fatalf("TargetError = %q, want %q", state.TargetError, "target_down")
	}

	// A successful check must clear the target down flag.
	probe.value = 0
	if err := poller.collect(context.Background()); err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	state = agentstate.GetProbeRunState("target-test")
	if state.TargetDown {
		t.Fatal("TargetDown must be false when check result is 0")
	}
	if state.TargetError != "" {
		t.Fatalf("TargetError = %q, want empty", state.TargetError)
	}
}
