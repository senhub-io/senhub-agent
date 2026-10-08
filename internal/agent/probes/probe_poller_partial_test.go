package probes

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type erroringProbe struct {
	*types.BaseProbe
	partial bool
}

func (e *erroringProbe) ShouldStart() bool           { return true }
func (e *erroringProbe) GetInterval() time.Duration  { return time.Second }
func (e *erroringProbe) OnStart(chan struct{}) error { return nil }
func (e *erroringProbe) OnShutdown(context.Context) error {
	return nil
}
func (e *erroringProbe) EntitySource() entity.Source { return types.NoOpEntitySource{} }
func (e *erroringProbe) Collect() ([]datapoint.DataPoint, error) {
	return []datapoint.DataPoint{{Name: "self.metric", Value: 1, Timestamp: time.Now()}}, errors.New("one path unreadable")
}

type partialProbe struct{ *erroringProbe }

func (partialProbe) KeepsPartialResults() bool { return true }

func routedOnError(t *testing.T, probe types.Probe) int {
	t.Helper()
	routed := 0
	poller := &ProbePoller{
		ProbeId:      "partial-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
			routed += len(d)
			return nil
		},
	}
	if err := poller.collect(); err == nil {
		t.Fatal("collect must still report the probe error")
	}
	return routed
}

func TestCollectErrorDropsDatapointsUnlessTheProbeKeepsPartialResults(t *testing.T) {
	base := &types.BaseProbe{}
	base.SetName("erroring")
	base.SetProbeType("erroring_stub")
	plain := &erroringProbe{BaseProbe: base}

	if got := routedOnError(t, plain); got != 0 {
		t.Fatalf("a probe that did not opt in had %d datapoints routed on error, want 0", got)
	}
	if got := routedOnError(t, partialProbe{plain}); got != 1 {
		t.Fatalf("a partial-results probe had %d datapoints routed on error, want 1", got)
	}
}
