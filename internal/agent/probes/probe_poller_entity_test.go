package probes

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// entityWireProbe is a minimal Probe whose entity source is controlled by the
// test. Its lifecycle hooks do nothing: the point is to observe what the
// POLLER does with EntitySource(), with no probe-side entity code at all.
type entityWireProbe struct {
	*types.BaseProbe
}

func (p *entityWireProbe) ShouldStart() bool                       { return true }
func (p *entityWireProbe) GetInterval() time.Duration              { return time.Hour }
func (p *entityWireProbe) OnStart(_ chan struct{}) error           { return nil }
func (p *entityWireProbe) OnShutdown(_ context.Context) error      { return nil }
func (p *entityWireProbe) Collect() ([]datapoint.DataPoint, error) { return nil, nil }

type staticEntitySource struct{}

func (staticEntitySource) Observe() (entity.Observation, bool) {
	return entity.Observation{}, true
}

// newEntityWirePoller builds a ProbePoller around an entityWireProbe carrying
// the given source (nil = BaseProbe's NoOpEntitySource fallback). The test
// constructor is registered directly in the catalogue and removed on cleanup.
func newEntityWirePoller(t *testing.T, src entity.Source) *ProbePoller {
	t.Helper()
	const probeType = "entity_wire_test"
	probeConstructors[probeType] = func(_ map[string]interface{}, _ *logger.Logger) (types.Probe, error) {
		p := &entityWireProbe{BaseProbe: &types.BaseProbe{}}
		p.SetProbeType(probeType)
		if src != nil {
			p.SetEntitySource(src)
		}
		return p, nil
	}
	t.Cleanup(func() { delete(probeConstructors, probeType) })

	poller, err := NewProbePoller(
		configuration.ProbeConfig{Name: "entity-wire", Type: probeType},
		logger.NewLogger(&cliArgs.ParsedArgs{}),
		func(_ []datapoint.DataPoint, _ data_store.StrategyRouter) error { return nil },
	)
	if err != nil {
		t.Fatalf("NewProbePoller: %v", err)
	}
	t.Cleanup(func() { _ = poller.Shutdown(context.Background()) })
	return poller
}

// TestProbePollerRegistersEntitySourceOnStart pins the runtime half of the
// #471 unification: the poller registers probe.EntitySource() with the entity
// detector on Start and unregisters it on Shutdown. Combined with
// TestEveryRegisteredProbeHasEntitySource (which inspects the same
// EntitySource() accessor), this closes the gap where the invariant validated
// one source while OnStart registered another: there is no other registration
// path left for probe sources.
func TestProbePollerRegistersEntitySourceOnStart(t *testing.T) {
	before := entity.RegisteredSourceCount()
	poller := newEntityWirePoller(t, staticEntitySource{})

	quit := make(chan struct{})
	defer close(quit)
	if err := poller.Start(quit); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := entity.RegisteredSourceCount(); got != before+1 {
		t.Fatalf("after Start the registry holds %d entity sources, want %d — "+
			"ProbePoller.Start must register the probe's EntitySource() with the "+
			"entity detector; it is the only registration path for probe sources (#471)",
			got, before+1)
	}

	if err := poller.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := entity.RegisteredSourceCount(); got != before {
		t.Fatalf("after Shutdown the registry holds %d entity sources, want %d — "+
			"ProbePoller.Shutdown must unregister the entity source, otherwise a "+
			"stopped or reloaded probe heartbeats its cached topology forever",
			got, before)
	}
}

// TestProbePollerSkipsNoOpEntitySource pins the other half of the contract:
// the NoOpEntitySource fallback of host-level probes and log conduits is never
// registered — the host entity is already emitted by the detector foundation,
// so registering N no-op sources would only pad the registry.
func TestProbePollerSkipsNoOpEntitySource(t *testing.T) {
	before := entity.RegisteredSourceCount()
	poller := newEntityWirePoller(t, nil)

	quit := make(chan struct{})
	defer close(quit)
	if err := poller.Start(quit); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := entity.RegisteredSourceCount(); got != before {
		t.Fatalf("Start registered %d entity sources for a NoOpEntitySource probe, want %d — "+
			"the poller must skip the NoOp fallback", got, before)
	}
}
