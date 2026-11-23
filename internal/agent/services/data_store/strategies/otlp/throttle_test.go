package otlp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// Timing margins used below. A server hint is whole seconds at best, so the
// tests use 2 to 3 s hints and assert only lower bounds that the code
// guarantees by construction (the next attempt is never earlier than the
// hint), plus a generous upper bound for CI jitter. Nothing asserts that an
// attempt happens on time.
const throttleUpperMargin = 8 * time.Second

func capturedLogger(buf *lockedBuffer) *logger.ModuleLogger {
	zl := zerolog.New(buf)
	return logger.NewModuleLogger(&zl, "test.throttle")
}

func throttledCount(signal string) uint64 { return agentstate.GetOTLPThrottledBySignal()[signal] }

func exportErrorCount(signal string) uint64 { return agentstate.GetOTLPExportErrorsBySignal()[signal] }

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"3", 3 * time.Second, true},
		{" 120 ", 120 * time.Second, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"", 0, false},
		{"soon", 0, false},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, false},
		{"1e3", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRetryAfter(c.in, now)
		if ok != c.ok || got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	// A value beyond any cap must not overflow into a negative duration.
	if got, ok := parseRetryAfter("9223372036854775807", now); !ok || got <= 0 {
		t.Errorf("huge delta = %v, %v; want a positive duration", got, ok)
	}
}

func TestRetryInfoDelay(t *testing.T) {
	st, err := status.New(codes.ResourceExhausted, "slow").
		WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := retryInfoDelay(st.Err()); !ok || d != 2*time.Second {
		t.Errorf("RetryInfo = %v, %v; want 2s", d, ok)
	}
	if _, ok := retryInfoDelay(status.Error(codes.Unavailable, "down")); ok {
		t.Error("a status without RetryInfo yielded a hint")
	}
}

func newTestHealth(t *testing.T, buf *lockedBuffer) (*exporterHealth, *time.Time) {
	t.Helper()
	h := &exporterHealth{}
	h.init("otlp/metrics", capturedLogger(buf))
	clock := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return clock }
	return h, &clock
}

func TestNoteThrottle_HintAboveCapIsClampedAndLoggedOnce(t *testing.T) {
	var buf lockedBuffer
	h, _ := newTestHealth(t, &buf)
	before := throttledCount("metrics")

	for i := 0; i < 3; i++ {
		if !h.noteThrottle(&throttledError{err: context.DeadlineExceeded, wait: 2 * time.Hour}) {
			t.Fatal("a hint was not recognised")
		}
	}
	if got := h.throttleRemaining(); got != maxThrottleHint {
		t.Errorf("wait = %v, want the cap %v", got, maxThrottleHint)
	}
	if n := strings.Count(buf.String(), "capped"); n != 1 {
		t.Errorf("cap warning logged %d times, want once:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "is throttling"); n != 1 {
		t.Errorf("episode warning logged %d times, want once:\n%s", n, buf.String())
	}
	if got := throttledCount("metrics") - before; got != 3 {
		t.Errorf("throttled counter moved by %d, want 3 (one per throttled export)", got)
	}
}

func TestNoteThrottle_NoHintLeavesTheClockAlone(t *testing.T) {
	var buf lockedBuffer
	h, _ := newTestHealth(t, &buf)
	before := throttledCount("metrics")
	if h.noteThrottle(status.Error(codes.Unavailable, "down")) {
		t.Error("a failure without a hint counted as a throttle")
	}
	if h.throttleRemaining() != 0 {
		t.Error("a failure without a hint set a wait")
	}
	if throttledCount("metrics") != before {
		t.Error("throttled counter moved without a hint")
	}
	if strings.Contains(buf.String(), "throttling") {
		t.Errorf("warned without a hint:\n%s", buf.String())
	}
}

func TestExporterHealth_ThrottleGatesLiveAttemptsUntilItPasses(t *testing.T) {
	var buf lockedBuffer
	h, clock := newTestHealth(t, &buf)
	h.probeInterval = time.Millisecond
	h.noteThrottle(&throttledError{err: context.DeadlineExceeded, wait: 3 * time.Second})
	h.markFailure()

	*clock = clock.Add(time.Second)
	if h.claimAttempt() {
		t.Error("a live attempt was let through inside the hint")
	}
	*clock = clock.Add(2*time.Second + time.Millisecond)
	if !h.claimAttempt() {
		t.Error("the live attempt is still refused after the hint")
	}
	h.markSuccess()
	// A new episode warns again.
	h.noteThrottle(&throttledError{err: context.DeadlineExceeded, wait: time.Second})
	if n := strings.Count(buf.String(), "is throttling"); n != 2 {
		t.Errorf("episode warnings = %d, want 2 (one per episode)", n)
	}
}

func TestReplayLoop_NeverProbesBeforeTheHint(t *testing.T) {
	r := &replayLoop{}
	var hold time.Duration
	r.hold = func() time.Duration { return hold }
	if got := r.armed(5 * time.Second); got != 5*time.Second {
		t.Errorf("no hint: gap = %v, want the clock's 5s", got)
	}
	hold = 3 * time.Second
	if got := r.armed(5 * time.Second); got != 5*time.Second {
		t.Errorf("short hint: gap = %v, want the clock's 5s (the hint is a minimum)", got)
	}
	hold = 90 * time.Second
	if got := r.armed(5 * time.Second); got != 90*time.Second {
		t.Errorf("long hint: gap = %v, want 90s", got)
	}
}

// pushKept pushes one metric request and requires it to be on disk, which a
// throttled export reports as errExportQueued.
func pushKept(t *testing.T, rig *wireRig, i int) {
	t.Helper()
	if err := rig.pushMetric(i, 1); !errors.Is(err, errExportQueued) {
		t.Fatalf("push %d = %v, want the request kept on disk", i, err)
	}
}

// startRigLoop runs the rig's real retry loop with small gaps, so the hint is
// the only thing that can hold an attempt back.
func startRigLoop(t *testing.T, rig *wireRig) {
	t.Helper()
	rig.ch.probeInterval = time.Millisecond
	rig.rp.firstDelay = 20 * time.Millisecond
	rig.rp.maxDelay = 50 * time.Millisecond
	rig.ch.setOnRecovered(func() { _, _ = rig.rp.replay() })
	rig.ch.setOnQueued(rig.rp.kick)
	rig.rp.start()
	t.Cleanup(rig.rp.stop)
}

// A throttle with a hint: the batch goes to disk, the server is not asked
// again before the hint has passed (neither by live traffic nor by the
// replay probe), and the data is delivered afterwards.
func TestThrottle_HintHoldsLiveAndReplayAttemptsThenDelivers(t *testing.T) {
	cases := []struct {
		name, protocol, retryAfter string
		retryDelay, hint           time.Duration
	}{
		{"http delta-seconds", "http", "3", 0, 3 * time.Second},
		{"grpc RetryInfo", "grpc", "", 2 * time.Second, 2 * time.Second},
		// The date has whole-second resolution and is cut down: at least
		// two of the three seconds remain at any rate.
		{"http HTTP-date", "http", "date", 0, 2 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			col := newFakeCollector(t, c.protocol)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			startRigLoop(t, rig)

			start := time.Now()
			until := start.Add(c.hint)
			retryAfter := c.retryAfter
			if retryAfter == "date" {
				until = start.Add(3 * time.Second)
				retryAfter = until.UTC().Format(http.TimeFormat)
				until = until.Truncate(time.Second)
			}
			col.throttleFor(until, retryAfter, c.retryDelay)
			throttledBefore, errorsBefore := throttledCount("metrics"), exportErrorCount("metrics")

			pushKept(t, rig, 1)
			if rig.files() != 1 {
				t.Fatalf("files = %d, want the throttled batch on disk", rig.files())
			}
			if rem := rig.ch.throttleRemaining(); rem <= 0 || rem > c.hint+time.Second {
				t.Errorf("remaining wait = %v, want within (0, %v]", rem, c.hint+time.Second)
			}
			// New batches during the wait go to disk without a request.
			for i := 2; i <= 4; i++ {
				pushKept(t, rig, i)
			}
			if n := col.attemptCount(); n != 1 {
				t.Errorf("server asked %d times during the wait, want 1", n)
			}
			if rig.files() != 4 {
				t.Errorf("files = %d, want 4 kept on disk", rig.files())
			}

			eventually(t, "the backlog to be delivered", func() bool {
				return len(col.receivedMetrics()) == 4
			})
			times := col.attemptsAt()
			if len(times) < 2 {
				t.Fatalf("attempts = %d", len(times))
			}
			if gap := times[1].Sub(start); gap < c.hint {
				t.Errorf("second attempt after %v, before the %v hint", gap, c.hint)
			}
			if gap := times[1].Sub(start); gap > c.hint+throttleUpperMargin {
				t.Errorf("second attempt after %v, far later than the %v hint", gap, c.hint)
			}
			got := col.metricTimes()
			for i := 1; i < len(got); i++ {
				if got[i] < got[i-1] {
					t.Errorf("replay out of order: %v", got)
				}
			}
			if got := throttledCount("metrics") - throttledBefore; got != 1 {
				t.Errorf("throttled counter moved by %d, want 1", got)
			}
			if got := exportErrorCount("metrics") - errorsBefore; got != 0 {
				t.Errorf("a throttle counted %d export errors, want 0", got)
			}
		})
	}
}

// The traces channel counts export errors itself; a throttle must not be
// one of them.
func TestThrottle_TracesAreKeptAndNotCountedAsErrors(t *testing.T) {
	col := newFakeCollector(t, "grpc")
	rig := newWireRig(t, col, t.TempDir(), "traces", 0)
	col.throttleFor(time.Now().Add(time.Minute), "", 2*time.Second)
	throttledBefore, errorsBefore := throttledCount("traces"), exportErrorCount("traces")

	if err := rig.pushSpan("kept-span"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if rig.files() != 1 {
		t.Fatalf("files = %d, want 1", rig.files())
	}
	if got := throttledCount("traces") - throttledBefore; got != 1 {
		t.Errorf("throttled counter moved by %d, want 1", got)
	}
	if got := exportErrorCount("traces") - errorsBefore; got != 0 {
		t.Errorf("export errors moved by %d, want 0", got)
	}
}

// A hint above the cap is clamped through the real transport as well.
func TestThrottle_HugeHintIsCappedEndToEnd(t *testing.T) {
	col := newFakeCollector(t, "http")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
	col.throttleFor(time.Now().Add(time.Minute), "86400", 0)

	pushKept(t, rig, 1)
	rem := rig.ch.throttleRemaining()
	if rem > maxThrottleHint || rem < maxThrottleHint-time.Minute {
		t.Errorf("wait = %v, want the cap %v", rem, maxThrottleHint)
	}
}

// Without a hint the backend is treated as down and the existing clock
// applies: no wait is recorded and the throttle counter stays put.
func TestThrottle_NoHintKeepsTheExistingClock(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			col.setDown(true)
			before := throttledCount("metrics")

			if err := rig.pushMetric(1, 1); err == nil {
				t.Fatal("a push against a down backend reported success")
			}
			if rig.ch.throttleRemaining() != 0 {
				t.Error("a plain outage set a throttle wait")
			}
			if throttledCount("metrics") != before {
				t.Error("a plain outage moved the throttle counter")
			}
			if got := rig.rp.armed(rig.rp.firstDelay); got != rig.rp.firstDelay {
				t.Errorf("retry gap = %v, want the clock's %v", got, rig.rp.firstDelay)
			}
		})
	}
}

// A throttling episode is one warning however many exports the server turns
// away, and the counter counts each of them.
func TestThrottle_OneWarningPerEpisode(t *testing.T) {
	col := newFakeCollector(t, "http")
	rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
	var buf lockedBuffer
	rig.ch.logger = capturedLogger(&buf)
	rig.ch.exporterHealth.logger = rig.ch.logger
	col.throttleFor(time.Now().Add(time.Minute), "30", 0)
	before := throttledCount("metrics")

	pushKept(t, rig, 1)
	for i := 0; i < 3; i++ {
		// Skip the wait to make the server turn the replay away again.
		rig.ch.throttledUntilNs.Store(0)
		if _, err := rig.rp.replay(); err == nil {
			t.Fatal("replay against a throttling server succeeded")
		}
	}
	if got := throttledCount("metrics") - before; got != 4 {
		t.Errorf("throttled counter moved by %d, want 4", got)
	}
	if n := strings.Count(buf.String(), "is throttling"); n != 1 {
		t.Errorf("episode warnings = %d, want exactly 1:\n%s", n, buf.String())
	}
	if rig.files() != 1 {
		t.Errorf("files = %d, want the batch still on disk", rig.files())
	}
}

// throttlingLogExporter fails every export the way a gRPC collector asking
// for a slower pace does.
type throttlingLogExporter struct {
	mu    sync.Mutex
	calls int
	delay time.Duration
}

func (e *throttlingLogExporter) Export(context.Context, []sdklog.Record) error {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	st, err := status.New(codes.ResourceExhausted, "slow down").
		WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(e.delay)})
	if err != nil {
		return err
	}
	return st.Err()
}
func (e *throttlingLogExporter) ForceFlush(context.Context) error { return nil }
func (e *throttlingLogExporter) Shutdown(context.Context) error   { return nil }
func (e *throttlingLogExporter) attempts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestPersistentLogExporter_ThrottleKeepsBatchesAndIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	exp := &throttlingLogExporter{delay: 30 * time.Second}
	var buf lockedBuffer
	log := capturedLogger(&buf)
	q := newLogsQueue(dir, 0, log)
	ple := newPersistentLogExporter(exp, q, log)

	pipe := buildLogsPipeline(ple, resource.NewSchemaless(), LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour}, "test")
	throttledBefore, errorsBefore := throttledCount("logs"), exportErrorCount("logs")
	ctx := context.Background()
	for _, body := range []string{"one", "two", "three"} {
		pipe.emit(ctx, agentstate.LogRecord{
			Timestamp: time.Unix(1700000000, 0), Severity: 9, SeverityText: "INFO",
			Body: body, ProducerProbeName: "syslog",
		})
		_ = pipe.provider.ForceFlush(ctx)
	}

	if n := exp.attempts(); n != 1 {
		t.Errorf("collector asked %d times, want 1: the others wait for the hint", n)
	}
	if got := countQueueFiles(t, dir); got != 3 {
		t.Errorf("queued files = %d, want 3", got)
	}
	if rem := ple.throttleRemaining(); rem <= 25*time.Second || rem > 30*time.Second {
		t.Errorf("remaining wait = %v, want about 30s", rem)
	}
	if got := throttledCount("logs") - throttledBefore; got != 1 {
		t.Errorf("throttled counter moved by %d, want 1", got)
	}
	if got := exportErrorCount("logs") - errorsBefore; got != 0 {
		t.Errorf("export errors moved by %d, want 0", got)
	}
	if n := strings.Count(buf.String(), "is throttling"); n != 1 {
		t.Errorf("episode warnings = %d, want 1:\n%s", n, buf.String())
	}
	if strings.Contains(buf.String(), "persisted to dead-letter queue") {
		t.Errorf("the per-batch failure warning fired for a throttle:\n%s", buf.String())
	}
}
