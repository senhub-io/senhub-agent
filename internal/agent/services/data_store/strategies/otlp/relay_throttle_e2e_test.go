package otlp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/otlpreceiver"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// A source agent exporting to a throttling otlp_receiver, end to end:
// real OTLP strategy, real HTTP exporters, real receivers, nothing faked
// between them. The receivers are reached through the loopback, so the
// whole path a customer relay takes is exercised except the network.
//
// The throttle is the receiver's own token bucket. A hammer goroutine
// keeps it empty for as long as the test wants a sustained 429, and
// stopping it lets the bucket refill, which is how the primary "comes
// back". The assertions never depend on a request landing in the single
// millisecond a token appears.

type e2eReceiver struct {
	name    string
	addr    string
	batches atomic.Int64
}

func startE2EReceiver(t *testing.T, name string, rps float64) *e2eReceiver {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()

	cfg := map[string]interface{}{
		"protocol": "http",
		"address":  addr,
		"signals":  []interface{}{"metrics", "logs"},
	}
	if rps > 0 {
		cfg["rate_limit_rps"] = rps
		cfg["rate_limit_burst"] = 1
	}
	zl := logger.NewLogger(&cliArgs.ParsedArgs{})
	p, err := otlpreceiver.NewOTLPReceiverProbe(cfg, zl)
	if err != nil {
		t.Fatalf("NewOTLPReceiverProbe(%s): %v", name, err)
	}
	probe := p.(*otlpreceiver.OTLPReceiverProbe)
	probe.SetName(name)
	r := &e2eReceiver{name: name, addr: addr}
	probe.SetCallback(func(points []datapoint.DataPoint) error {
		if len(points) > 0 {
			r.batches.Add(1)
		}
		return nil
	})
	quit := make(chan struct{})
	if err := probe.OnStart(quit); err != nil {
		t.Fatalf("starting receiver %s: %v", name, err)
	}
	t.Cleanup(func() {
		close(quit)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = probe.OnShutdown(ctx)
	})
	return r
}

// hammer holds the receiver's bucket empty until stopped. It returns once
// the receiver has answered 429 to it, so the caller starts from a
// throttled receiver.
func hammer(t *testing.T, r *e2eReceiver) (stop func()) {
	t.Helper()
	done := make(chan struct{})
	var wg sync.WaitGroup
	throttled := make(chan struct{})
	var once sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		client := &http.Client{Timeout: time.Second}
		for {
			select {
			case <-done:
				return
			default:
			}
			resp, err := client.Post("http://"+r.addr+"/v1/metrics", "application/x-protobuf", bytes.NewReader(nil))
			if err == nil {
				if resp.StatusCode == http.StatusTooManyRequests {
					once.Do(func() { close(throttled) })
				}
				_ = resp.Body.Close()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	select {
	case <-throttled:
	case <-time.After(5 * time.Second):
		close(done)
		wg.Wait()
		t.Fatalf("receiver %s never answered 429 to the hammer", r.name)
	}
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(done)
			wg.Wait()
		})
	}
}

// logTally counts, per body, the log records each receiver ingested.
type logTally struct {
	mu     sync.Mutex
	counts map[string]map[string]int // body -> receiver -> n
}

func watchReceiverLogs(t *testing.T, prefix string, receivers ...*e2eReceiver) *logTally {
	t.Helper()
	tally := &logTally{counts: map[string]map[string]int{}}
	ch := agentstate.SubscribeLogs(1024)
	names := map[string]bool{}
	for _, r := range receivers {
		names[r.name] = true
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case rec := <-ch:
				if !names[rec.ProducerProbeName] || len(rec.Body) < len(prefix) || rec.Body[:len(prefix)] != prefix {
					continue
				}
				tally.mu.Lock()
				if tally.counts[rec.Body] == nil {
					tally.counts[rec.Body] = map[string]int{}
				}
				tally.counts[rec.Body][rec.ProducerProbeName]++
				tally.mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
		agentstate.UnsubscribeLogs(ch)
	})
	return tally
}

func (l *logTally) total(body string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.counts[body] {
		n += c
	}
	return n
}

func (l *logTally) byReceiver(body, receiver string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[body][receiver]
}

func e2eSourceParams(endpoint string, fallbacks []interface{}, queueDir string) map[string]interface{} {
	params := map[string]interface{}{
		"endpoint":    endpoint,
		"protocol":    "http",
		"compression": "none",
		"timeout":     "2s",
		"tls":         map[string]interface{}{"enabled": false},
		"retry":       map[string]interface{}{"enabled": false},
		"persistence": map[string]interface{}{"enabled": true, "path": queueDir},
		"signals": map[string]interface{}{
			"metrics":  map[string]interface{}{"enabled": true, "interval": "1h"},
			"logs":     map[string]interface{}{"enabled": true, "batch_size": 1000, "batch_timeout": "1h"},
			"entities": map[string]interface{}{"enabled": false},
		},
	}
	if len(fallbacks) > 0 {
		params["fallback_endpoints"] = fallbacks
	}
	return params
}

// startE2ESource starts the source strategy and stops its relays. The
// relays drain the in-process receiver rails, which here are fed by the
// test's own receivers: left running they would forward every ingested
// batch back to the primary, a loop that cannot exist across two agents.
func startE2ESource(t *testing.T, params map[string]interface{}, cooldown time.Duration) *OTLPSyncStrategy {
	t.Helper()
	s := newTestStrategy(t, params)
	s.cfg.FailoverCooldown = cooldown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	if s.metricsRelay != nil {
		s.metricsRelay.stop(ctx)
	}
	if s.logsRelay != nil {
		s.logsRelay.stop(ctx)
	}
	return s
}

func publishE2ELogs(prefix string, n int) []string {
	bodies := make([]string, n)
	for i := range bodies {
		bodies[i] = fmt.Sprintf("%s-%d", prefix, i)
		agentstate.PublishLog(agentstate.LogRecord{
			Timestamp:         time.Now(),
			Severity:          agentstate.SeverityFromText("INFO"),
			SeverityText:      "INFO",
			Body:              bodies[i],
			ProducerProbeName: "syslog_e2e",
			ProducerProbeType: "syslog",
		})
	}
	return bodies
}

// flushLogsUntil flushes the source's log pipeline until every body has
// reached a receiver, then once more after a grace period so a duplicate
// would have had the time to show.
func flushLogsUntil(t *testing.T, s *OTLPSyncStrategy, tally *logTally, bodies []string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = s.logs.provider.ForceFlush(ctx)
		missing := 0
		for _, b := range bodies {
			if tally.total(b) == 0 {
				missing++
			}
		}
		if missing == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d log records never reached a receiver", missing, len(bodies))
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	_ = s.logs.provider.ForceFlush(ctx)
	time.Sleep(50 * time.Millisecond)
}

func TestRelayThrottle_FailoverCarriesBothSignalsAndComesBack(t *testing.T) {
	agentstate.ResetExportRejectedForTest()
	primary := startE2EReceiver(t, "e2e_primary", 5)
	standby := startE2EReceiver(t, "e2e_standby", 0)
	tally := watchReceiverLogs(t, "fo-", primary, standby)

	const cooldown = 300 * time.Millisecond
	s := startE2ESource(t, e2eSourceParams(primary.addr, []interface{}{standby.addr}, t.TempDir()), cooldown)
	ctx := context.Background()

	pushes := 0
	delivered := func() int64 { return primary.batches.Load() + standby.batches.Load() }
	push := func() {
		before := delivered()
		s.pushPeriodic(ctx)
		pushes++
		if got := delivered() - before; got != 1 {
			t.Fatalf("push %d reached receivers %d times, want exactly once (a gap, or a double delivery)", pushes, got)
		}
	}

	// The bucket starts full: the first push and first logs land on the primary.
	push()
	if primary.batches.Load() != 1 {
		t.Fatalf("baseline metrics went to primary=%d standby=%d, want primary", primary.batches.Load(), standby.batches.Load())
	}
	base := publishE2ELogs("fo-base", 3)
	flushLogsUntil(t, s, tally, base)

	// Sustained 429 on the primary.
	stop := hammer(t, primary)
	defer stop()
	standbyBefore := standby.batches.Load()
	push()
	push()
	if standby.batches.Load()-standbyBefore < 2 {
		t.Errorf("standby received %d of the 2 throttled pushes", standby.batches.Load()-standbyBefore)
	}
	throttled := publishE2ELogs("fo-throttled", 5)
	flushLogsUntil(t, s, tally, throttled)
	for _, b := range throttled {
		if tally.byReceiver(b, standby.name) != 1 {
			t.Errorf("log %q reached the standby %d times, want 1", b, tally.byReceiver(b, standby.name))
		}
	}

	// The primary recovers: the next pushes return to it.
	stop()
	time.Sleep(cooldown + 300*time.Millisecond)
	primaryBefore := primary.batches.Load()
	for i := 0; i < 10 && primary.batches.Load() == primaryBefore; i++ {
		push()
		time.Sleep(cooldown)
	}
	if primary.batches.Load() == primaryBefore {
		t.Error("metrics never returned to the primary after it stopped throttling")
	}
	after := publishE2ELogs("fo-after", 3)
	flushLogsUntil(t, s, tally, after)

	for _, group := range [][]string{base, throttled, after} {
		for _, b := range group {
			if n := tally.total(b); n != 1 {
				t.Errorf("log %q arrived %d times, want exactly once", b, n)
			}
		}
	}
	if switches := agentstate.GetOTLPEndpointSwitchesTotal(); switches < 2 {
		t.Errorf("endpoint switches = %d, want at least 2 (away and back)", switches)
	}
}

func TestRelayThrottle_QueuedLogsReplayOnceAfterThrottle(t *testing.T) {
	primary := startE2EReceiver(t, "e2e_only", 5)
	tally := watchReceiverLogs(t, "q-", primary)

	queueDir := t.TempDir()
	s := startE2ESource(t, e2eSourceParams(primary.addr, nil, queueDir), 0)
	if s.logsQueue == nil || s.logsReplayer == nil {
		t.Fatal("the logs queue is off; the test needs it on")
	}

	stop := hammer(t, primary)
	defer stop()
	bodies := publishE2ELogs("q", 4)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = s.logs.provider.ForceFlush(context.Background())
		if n, _ := s.logsQueue.pending(); n >= len(bodies) {
			break
		}
		if time.Now().After(deadline) {
			n, _ := s.logsQueue.pending()
			t.Fatalf("only %d of %d records were queued during the 429", n, len(bodies))
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, b := range bodies {
		if n := tally.total(b); n != 0 {
			t.Errorf("log %q reached the receiver %d times while it was throttling", b, n)
		}
	}

	stop()
	time.Sleep(400 * time.Millisecond)
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, _ = s.logsReplayer.replay()
		if n, _ := s.logsQueue.pending(); n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queue never drained after the receiver stopped throttling")
		}
		time.Sleep(100 * time.Millisecond)
	}
	flushLogsUntil(t, s, tally, bodies)
	for _, b := range bodies {
		if n := tally.total(b); n != 1 {
			t.Errorf("log %q arrived %d times, want exactly once", b, n)
		}
	}
}
