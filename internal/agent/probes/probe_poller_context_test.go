package probes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// ctxProbe implements ContextCollector. Its first call returns at once
// so Start (which runs the first collection synchronously) completes;
// every later call blocks until its context ends, the shape of a
// request to a target that stopped answering.
type ctxProbe struct {
	*types.BaseProbe
	interval   time.Duration
	blockFirst bool
	calls      atomic.Int64
	plainCalls atomic.Int64
	entered    chan struct{}
	ended      chan error
	deadline   chan time.Time
}

func newCtxProbe(interval time.Duration, blockFirst bool) *ctxProbe {
	return &ctxProbe{
		BaseProbe:  &types.BaseProbe{},
		interval:   interval,
		blockFirst: blockFirst,
		entered:    make(chan struct{}, 1),
		ended:      make(chan error, 1),
		deadline:   make(chan time.Time, 1),
	}
}

func (p *ctxProbe) ShouldStart() bool                  { return true }
func (p *ctxProbe) GetInterval() time.Duration         { return p.interval }
func (p *ctxProbe) OnStart(chan struct{}) error        { return nil }
func (p *ctxProbe) OnShutdown(_ context.Context) error { return nil }

func (p *ctxProbe) Collect() ([]datapoint.DataPoint, error) {
	p.plainCalls.Add(1)
	return p.CollectContext(context.Background())
}

func (p *ctxProbe) CollectContext(ctx context.Context) ([]datapoint.DataPoint, error) {
	n := p.calls.Add(1)
	if dl, ok := ctx.Deadline(); ok {
		select {
		case p.deadline <- dl:
		default:
		}
	}
	if n == 1 && !p.blockFirst {
		return []datapoint.DataPoint{{Name: "ctx.metric", Value: 1, Timestamp: time.Now()}}, nil
	}
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case p.ended <- ctx.Err():
	default:
	}
	return nil, fmt.Errorf("waiting for the target: %w", ctx.Err())
}

// plainProbe implements only Collect, as every probe written before
// ContextCollector existed.
type plainProbe struct {
	*types.BaseProbe
	calls atomic.Int64
}

func (p *plainProbe) ShouldStart() bool                  { return true }
func (p *plainProbe) GetInterval() time.Duration         { return time.Hour }
func (p *plainProbe) OnStart(chan struct{}) error        { return nil }
func (p *plainProbe) OnShutdown(_ context.Context) error { return nil }
func (p *plainProbe) Collect() ([]datapoint.DataPoint, error) {
	p.calls.Add(1)
	return []datapoint.DataPoint{{Name: "plain.metric", Value: 1, Timestamp: time.Now()}}, nil
}

// nameWithShortOffset returns an instance name whose start offset is under
// 20ms, so a test that expects the first collection right after Start does
// not wait for the spread.
func nameWithShortOffset(probeType string, interval time.Duration) string {
	for i := 0; ; i++ {
		name := fmt.Sprintf("%s-instance-%d", probeType, i)
		id := GenerateProbeId(configuration.ProbeConfig{Name: name, Type: probeType})
		if StartOffset(id, interval) < 20*time.Millisecond {
			return name
		}
	}
}

func newPollerFor(t *testing.T, probeType string, probe types.Probe, routed *atomic.Int64) *ProbePoller {
	t.Helper()
	probeConstructors[probeType] = func(_ map[string]interface{}, _ *logger.Logger) (types.Probe, error) {
		return probe, nil
	}
	t.Cleanup(func() { delete(probeConstructors, probeType) })

	poller, err := NewProbePoller(
		configuration.ProbeConfig{Name: nameWithShortOffset(probeType, probe.GetInterval()), Type: probeType},
		logger.NewLogger(&cliArgs.ParsedArgs{}),
		func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
			if routed != nil {
				routed.Add(int64(len(d)))
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("NewProbePoller: %v", err)
	}
	t.Cleanup(func() { _ = poller.Shutdown(context.Background()) })
	return poller
}

func waitEntered(t *testing.T, p *ctxProbe) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the blocking collection never started")
	}
}

// A reload shuts the probe down; a collection blocked on a target that
// stopped answering must be released, not waited for.
func TestContextCollectorIsCancelledOnShutdown(t *testing.T) {
	probe := newCtxProbe(10*time.Millisecond, false)
	poller := newPollerFor(t, "ctx_shutdown_test", probe, nil)

	if err := poller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitEntered(t, probe)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if err := poller.Shutdown(stopCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Shutdown took %v with a collection blocked on its context", elapsed)
	}
	select {
	case err := <-probe.ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the collection ended with %v, want context.Canceled", err)
		}
	default:
		t.Fatal("Shutdown returned but the collection in flight was not cancelled")
	}
	if probe.plainCalls.Load() != 0 {
		t.Fatal("the poller called Collect on a probe that implements CollectContext")
	}
}

// Stopping the agent cancels the context Start received; that must reach
// a collection in progress, including the first one, which Start runs
// itself.
func TestContextCollectorIsCancelledWhenTheAgentStops(t *testing.T) {
	probe := newCtxProbe(time.Hour, true)
	poller := newPollerFor(t, "ctx_agent_stop_test", probe, nil)

	agentCtx, stopAgent := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() { startDone <- poller.Start(agentCtx) }()
	waitEntered(t, probe)
	stopAgent()

	select {
	case <-startDone:
	case <-time.After(time.Second):
		t.Fatal("the first collection was not cancelled when the agent context ended")
	}
	if err := <-probe.ended; !errors.Is(err, context.Canceled) {
		t.Fatalf("the collection ended with %v, want context.Canceled", err)
	}
}

// The default deadline never drops below five minutes, however short the
// interval, so a collection longer than its interval is not cut.
func TestScheduledContextCollectorRunsUnderTheFiveMinuteFloor(t *testing.T) {
	probe := newCtxProbe(400*time.Millisecond, false)
	poller := newPollerFor(t, "ctx_deadline_test", probe, nil)

	if err := poller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case dl := <-probe.deadline:
		if left := time.Until(dl); left > minCollectDeadline || left < minCollectDeadline-10*time.Second {
			t.Fatalf("deadline %v away, want about %v", left, minCollectDeadline)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CollectContext was never called")
	}
}

// slowFirstProbe takes longer than its interval on the first call only.
type slowFirstProbe struct {
	*types.BaseProbe
	interval, first time.Duration
	calls           atomic.Int64
	firstErr        chan error
}

func (p *slowFirstProbe) ShouldStart() bool                  { return true }
func (p *slowFirstProbe) GetInterval() time.Duration         { return p.interval }
func (p *slowFirstProbe) OnStart(chan struct{}) error        { return nil }
func (p *slowFirstProbe) OnShutdown(_ context.Context) error { return nil }
func (p *slowFirstProbe) Collect() ([]datapoint.DataPoint, error) {
	return p.CollectContext(context.Background())
}
func (p *slowFirstProbe) CollectContext(ctx context.Context) ([]datapoint.DataPoint, error) {
	if p.calls.Add(1) > 1 {
		return nil, nil
	}
	select {
	case <-time.After(p.first):
		p.firstErr <- nil
		return []datapoint.DataPoint{{Name: "slow.metric", Value: 1, Timestamp: time.Now()}}, nil
	case <-ctx.Done():
		p.firstErr <- ctx.Err()
		return nil, ctx.Err()
	}
}

// A collection longer than its interval but inside its deadline keeps its
// data and counts as exactly one overrun.
func TestCollectionLongerThanItsIntervalKeepsItsDataAndCountsOneOverrun(t *testing.T) {
	agentstate.ResetCollectionScheduleForTest()
	probe := &slowFirstProbe{BaseProbe: &types.BaseProbe{}, interval: 100 * time.Millisecond, first: 350 * time.Millisecond, firstErr: make(chan error, 1)}
	var routed atomic.Int64
	poller := newPollerFor(t, "slow_first_test", probe, &routed)

	if err := poller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case err := <-probe.firstErr:
		if err != nil {
			t.Fatalf("the slow collection was cut: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the slow collection never ended")
	}
	for end := time.Now().Add(3 * time.Second); probe.calls.Load() < 2 && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
	if got := routed.Load(); got != 1 {
		t.Errorf("%d datapoints routed, want the 1 of the slow collection", got)
	}
	if got := agentstate.GetCollectOverruns()[agentstate.ProbeKey{Name: poller.config.Name, Type: "slow_first_test"}]; got != 1 {
		t.Errorf("overruns = %d, want 1", got)
	}
}

// A collect_timeout shorter than the interval cancels the call at that
// timeout.
func TestCollectTimeoutShorterThanTheIntervalCancelsTheCall(t *testing.T) {
	probe := newCtxProbe(time.Hour, true)
	probeType := "ctx_collect_timeout_test"
	probeConstructors[probeType] = func(_ map[string]interface{}, _ *logger.Logger) (types.Probe, error) {
		return probe, nil
	}
	t.Cleanup(func() { delete(probeConstructors, probeType) })

	cfg := configuration.ProbeConfig{Type: probeType, CollectTimeout: "150ms"}
	for i := 0; ; i++ {
		cfg.Name = fmt.Sprintf("ct-%d", i)
		if StartOffset(GenerateProbeId(cfg), time.Hour) < 20*time.Millisecond {
			break
		}
	}
	poller, err := NewProbePoller(
		cfg,
		logger.NewLogger(&cliArgs.ParsedArgs{}),
		func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil },
	)
	if err != nil {
		t.Fatalf("NewProbePoller: %v", err)
	}
	t.Cleanup(func() { _ = poller.Shutdown(context.Background()) })

	began := time.Now()
	go func() { _ = poller.Start(context.Background()) }()
	select {
	case err := <-probe.ended:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the call ended with %v, want DeadlineExceeded", err)
		}
		if took := time.Since(began); took > 2*time.Second {
			t.Errorf("cancelled after %v, want about 150ms", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collect_timeout did not cancel the call")
	}
}

func TestInvalidCollectTimeoutFailsTheProbe(t *testing.T) {
	_, err := NewProbePoller(
		configuration.ProbeConfig{Name: "bad", Type: "cpu", CollectTimeout: "-5s"},
		logger.NewLogger(&cliArgs.ParsedArgs{}),
		func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "collect_timeout") {
		t.Fatalf("error = %v, want one naming collect_timeout", err)
	}
}

// A probe that only has Collect is driven exactly as before.
func TestPlainCollectProbeStillCollects(t *testing.T) {
	probe := &plainProbe{BaseProbe: &types.BaseProbe{}}
	var routed atomic.Int64
	poller := newPollerFor(t, "plain_collect_test", probe, &routed)

	if err := poller.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for end := time.Now().Add(3 * time.Second); probe.calls.Load() == 0 && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
	if err := poller.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := probe.calls.Load(); got != 1 {
		t.Fatalf("Collect ran %d times, want 1 (the start-up collection)", got)
	}
	if got := routed.Load(); got != 1 {
		t.Fatalf("%d datapoints routed, want 1", got)
	}
}

// A cycle interrupted by the probe's own shutdown says nothing about the
// target: it must not count as a collection error.
func TestCancelledCycleIsNotACollectError(t *testing.T) {
	probe := newCtxProbe(time.Hour, true)
	poller := newPollerFor(t, "ctx_not_an_error_test", probe, nil)

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- poller.collect(runCtx) }()
	waitEntered(t, probe)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a cycle cancelled by shutdown reported %v, want nil", err)
	}
}
