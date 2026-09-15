package probes

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// listenerStub is a probe whose data arrives by subscription: its
// Collect is a no-op, exactly like the shipped syslog / event / OTLP
// receiver probes, and its liveness is a separate fact.
type listenerStub struct {
	*types.BaseProbe
	health error
}

func (l *listenerStub) ShouldStart() bool           { return true }
func (l *listenerStub) GetInterval() time.Duration  { return time.Second }
func (l *listenerStub) OnStart(chan struct{}) error { return nil }
func (l *listenerStub) OnShutdown(context.Context) error {
	return nil
}
func (l *listenerStub) EntitySource() entity.Source { return types.NoOpEntitySource{} }

// Collect returns nothing and no error — the shape that used to make
// these probes look permanently healthy.
func (l *listenerStub) Collect() ([]datapoint.DataPoint, error) { return nil, nil }

func (l *listenerStub) ListenerHealth() error { return l.health }

func newListenerPoller(t *testing.T, probe types.Probe) *ProbePoller {
	t.Helper()
	return &ProbePoller{
		ProbeId:      "listener-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func([]datapoint.DataPoint, data_store.StrategyRouter) error {
			return nil
		},
	}
}

// TestListenerProbeHealthFollowsTheListener is the defect #289 names: a
// listener probe's no-op Collect returning nil says the no-op ran, not
// that the socket is alive. Health must come from the listener.
func TestListenerProbeHealthFollowsTheListener(t *testing.T) {
	base := &types.BaseProbe{}
	base.SetName("listener-under-test")
	base.SetProbeType("listener_stub")

	probe := &listenerStub{BaseProbe: base}
	poller := newListenerPoller(t, probe)

	agentstate.SetActiveProbes([]string{poller.ProbeId})

	// Listener alive: a no-op cycle reports healthy, as before.
	probe.health = nil
	if err := poller.collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, healthy := agentstate.GetProbeCounts(); healthy != 1 {
		t.Fatal("a live listener should report healthy")
	}

	// Listener dead: the SAME no-op Collect must now report unhealthy.
	probe.health = errors.New("socket closed")
	if err := poller.collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, healthy := agentstate.GetProbeCounts(); healthy != 0 {
		t.Error("a dead listener still reported healthy — the no-op Collect is being used as evidence of liveness")
	}

	// And it recovers when the listener comes back.
	probe.health = nil
	if err := poller.collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, healthy := agentstate.GetProbeCounts(); healthy != 1 {
		t.Error("a recovered listener should report healthy again")
	}
}

// pollingStub is an ordinary polling probe: no ListenerHealth, so its
// health stays "did the last Collect succeed".
type pollingStub struct {
	*types.BaseProbe
	points []datapoint.DataPoint
	err    error
}

func (p *pollingStub) ShouldStart() bool                { return true }
func (p *pollingStub) GetInterval() time.Duration       { return time.Second }
func (p *pollingStub) OnStart(chan struct{}) error      { return nil }
func (p *pollingStub) OnShutdown(context.Context) error { return nil }
func (p *pollingStub) EntitySource() entity.Source      { return types.NoOpEntitySource{} }
func (p *pollingStub) Collect() ([]datapoint.DataPoint, error) {
	return p.points, p.err
}

// TestIdentityTagsAreGuaranteedCentrally pins the guarantee that
// replaces a convention: a probe that never calls
// EnrichDataPointsWithProbeName used to ship datapoints with no
// probe_name and no probe_type, which collide in the cache and are
// indistinguishable at the sinks. The poller now supplies them.
func TestIdentityTagsAreGuaranteedCentrally(t *testing.T) {
	base := &types.BaseProbe{}
	base.SetName("forgetful-probe")
	base.SetProbeType("forgetful")

	probe := &pollingStub{
		BaseProbe: base,
		points: []datapoint.DataPoint{
			{Name: "some.metric", Value: 1, Tags: []tags.Tag{{Key: "disk", Value: "sda"}}},
		},
	}

	var routed []datapoint.DataPoint
	poller := &ProbePoller{
		ProbeId:      "identity-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
			routed = d
			return nil
		},
	}

	if err := poller.collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(routed) != 1 {
		t.Fatalf("routed %d datapoints, want 1", len(routed))
	}
	assertTag(t, routed[0].Tags, "probe_name", "forgetful-probe")
	assertTag(t, routed[0].Tags, "probe_type", "forgetful")
	assertTag(t, routed[0].Tags, "disk", "sda")
}

// TestIdentityTagsAreNotDuplicated is what lets the probe-side calls
// stay where they are: a probe that already enriched must come through
// untouched, or every well-behaved probe would ship two probe_name tags.
func TestIdentityTagsAreNotDuplicated(t *testing.T) {
	base := &types.BaseProbe{}
	base.SetName("well-behaved")
	base.SetProbeType("polite")

	probe := &pollingStub{
		BaseProbe: base,
		points: []datapoint.DataPoint{
			{Name: "some.metric", Value: 1, Tags: []tags.Tag{
				{Key: "probe_name", Value: "well-behaved"},
				{Key: "probe_type", Value: "polite"},
			}},
		},
	}

	var routed []datapoint.DataPoint
	poller := &ProbePoller{
		ProbeId:      "dedup-test",
		Probe:        probe,
		moduleLogger: testModuleLogger(t),
		addDataPoint: func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
			routed = d
			return nil
		},
	}

	if err := poller.collect(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if n := countTag(routed[0].Tags, "probe_name"); n != 1 {
		t.Errorf("probe_name appears %d times, want exactly 1", n)
	}
	if n := countTag(routed[0].Tags, "probe_type"); n != 1 {
		t.Errorf("probe_type appears %d times, want exactly 1", n)
	}
}

func assertTag(t *testing.T, ts []tags.Tag, key, want string) {
	t.Helper()
	for _, tag := range ts {
		if tag.Key == key {
			if tag.Value != want {
				t.Errorf("tag %q = %q, want %q", key, tag.Value, want)
			}
			return
		}
	}
	t.Errorf("tag %q missing from %+v", key, ts)
}

func countTag(ts []tags.Tag, key string) int {
	n := 0
	for _, tag := range ts {
		if tag.Key == key {
			n++
		}
	}
	return n
}

// testModuleLogger builds a logger that writes nowhere useful; the
// tests assert on published state, not on log output.
func testModuleLogger(t *testing.T) *logger.ModuleLogger {
	t.Helper()
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "probe.test")
}
