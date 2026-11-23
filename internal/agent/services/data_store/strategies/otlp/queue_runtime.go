package otlp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// exporterHealth tracks whether the backend is answering, for an exporter
// that keeps failed batches on disk. While it is known down, live batches go
// straight to disk without a network attempt (one probe per probeInterval),
// so a batch is written once and stays there until the replayer gets an
// acknowledged export for it. onRecovered fires the first time an export
// succeeds after a failure.
type exporterHealth struct {
	// successKey names the signal in the export-success marker.
	successKey string
	logger     *logger.ModuleLogger
	now        func() time.Time

	healthy     atomic.Bool
	onRecovered atomic.Pointer[func()]
	// onQueued is called when a batch lands on disk, so the retry loop
	// starts its clock then rather than at the next record.
	onQueued atomic.Pointer[func()]

	lastSuccessNs atomic.Int64
	lastFailureNs atomic.Int64
	// probeInterval spaces the live exports attempted while the backend is
	// known down.
	probeInterval time.Duration
	// recentSuccess is how recent the last acknowledged export must be for
	// the backend to count as known-up at shutdown.
	recentSuccess time.Duration
	// stopping is set at shutdown: unless the backend answered recently it
	// is not tried at all, the batch goes to disk, and an attempt that is
	// made is bounded by drainTimeout. An export already in flight is
	// cancelled through stopCh and persisted.
	stopping     atomic.Bool
	drainTimeout atomic.Int64
	stopCh       chan struct{}
	stopOnce     sync.Once

	// lastFailWarnNs throttles the export-failure warning. Batches can
	// flush every few seconds; one warning per interval is enough to
	// surface a dying pipeline without flooding the journal (#821).
	lastFailWarnNs atomic.Int64
}

// logExportWarnInterval spaces the "OTLP logs export failed" warnings.
const logExportWarnInterval = 30 * time.Second

// logsProbeInterval is how often a live export is attempted while the
// backend is known down.
const logsProbeInterval = 5 * time.Second

// logsRecentSuccess is how recent the last acknowledged export must be for
// the backend to count as known-up at shutdown.
const logsRecentSuccess = 30 * time.Second

func (h *exporterHealth) init(successKey string, log *logger.ModuleLogger) {
	h.successKey = successKey
	h.logger = log
	h.now = time.Now
	h.stopCh = make(chan struct{})
	h.probeInterval = logsProbeInterval
	h.recentSuccess = logsRecentSuccess
	h.healthy.Store(true)
}

func (h *exporterHealth) setOnRecovered(fn func()) {
	h.onRecovered.Store(&fn)
}

func (h *exporterHealth) setOnQueued(fn func()) {
	h.onQueued.Store(&fn)
}

func (h *exporterHealth) fireQueued() {
	if p := h.onQueued.Load(); p != nil && *p != nil {
		(*p)()
	}
}

// backendDown reports whether the last export failed and none succeeded since.
func (h *exporterHealth) backendDown() bool { return !h.healthy.Load() }

// beginShutdown makes every further export bounded by attempt and keeps a
// known-down backend from being tried at all.
func (h *exporterHealth) beginShutdown(attempt time.Duration) {
	h.drainTimeout.Store(int64(attempt))
	h.stopping.Store(true)
	h.stopOnce.Do(func() { close(h.stopCh) })
}

// knownUp reports whether the backend answered recently.
func (h *exporterHealth) knownUp() bool {
	if !h.healthy.Load() {
		return false
	}
	last := h.lastSuccessNs.Load()
	return last != 0 && h.now().UnixNano()-last <= int64(h.recentSuccess)
}

// claimAttempt lets one live export through per probeInterval after a failure.
func (h *exporterHealth) claimAttempt() bool {
	if h.stopping.Load() {
		return false
	}
	now := h.now().UnixNano()
	last := h.lastFailureNs.Load()
	if now-last < int64(h.probeInterval) {
		return false
	}
	return h.lastFailureNs.CompareAndSwap(last, now)
}

func (h *exporterHealth) markSuccess() {
	agentstate.RecordExportSuccess(h.successKey)
	h.lastSuccessNs.Store(h.now().UnixNano())
	// Export succeeded: if we were unhealthy, the backend just recovered.
	if h.healthy.CompareAndSwap(false, true) {
		if p := h.onRecovered.Load(); p != nil && *p != nil {
			go (*p)()
		}
	}
}

func (h *exporterHealth) markFailure() {
	h.lastFailureNs.Store(h.now().UnixNano())
	h.healthy.Store(false)
}

// warnThrottled emits at most one warning per logExportWarnInterval, so
// a sustained outage does not turn the journal into the incident.
func (h *exporterHealth) warnThrottled(emit func(*zerolog.Event)) {
	if h.logger == nil {
		return
	}
	now := time.Now().UnixNano()
	last := h.lastFailWarnNs.Load()
	if now-last < int64(logExportWarnInterval) || !h.lastFailWarnNs.CompareAndSwap(last, now) {
		return
	}
	emit(h.logger.Warn())
}

// The logs rail is the sparse one, and a queued batch used to wait for
// the next record on that same rail to be retried: on a quiet host that
// is minutes, long enough for a consumer to expire the whole host. These
// bound the wait instead: the first probe comes after replayFirstDelay and
// the gap doubles up to replayMaxDelay for as long as the backend stays down.
const (
	replayFirstDelay = 5 * time.Second
	replayMaxDelay   = 5 * time.Minute
	// replayExportTimeout bounds one replayed batch.
	replayExportTimeout = 10 * time.Second
)

// queueSweepInterval spaces the age sweeps.
const queueSweepInterval = 10 * time.Minute

// replayLoop is the retry clock shared by every queue replayer: a queued
// batch is tried again on its own, on a doubling delay, until it leaves or
// the agent stops. Concurrent kicks collapse to one running clock.
type replayLoop struct {
	label   string
	logger  *logger.ModuleLogger
	pending func() (int, time.Duration)
	// run drains the queue once; sweep applies the age limit.
	run   func() (int, error)
	sweep func()

	// probing is true while the loop owns the retry clock for a non-empty
	// queue; a batch queued meanwhile does not restart it.
	probing atomic.Bool

	firstDelay time.Duration
	maxDelay   time.Duration
	// after is the retry clock; tests replace it.
	after func(time.Duration) <-chan time.Time

	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	quit   chan struct{}
	wg     sync.WaitGroup
}

func (r *replayLoop) initLoop(label string, log *logger.ModuleLogger) {
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.label = label
	r.logger = log
	r.firstDelay = replayFirstDelay
	r.maxDelay = replayMaxDelay
	r.after = time.After
	r.wake = make(chan struct{}, 1)
	r.quit = make(chan struct{})
}

// start runs the retry loop and the age sweeper.
func (r *replayLoop) start() {
	r.wg.Add(2)
	go r.loop()
	go r.sweepLoop()
}

func (r *replayLoop) sweepLoop() {
	defer r.wg.Done()
	t := time.NewTicker(queueSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-r.quit:
			return
		case <-t.C:
			r.sweep()
		}
	}
}

// stop ends the retry loop and aborts a replay in flight. Safe to call once.
func (r *replayLoop) stop() {
	r.cancel()
	close(r.quit)
	r.wg.Wait()
}

// kick tells the loop a batch was just queued. It starts the retry clock if
// it is idle; it never replays on the spot (the backend just failed) and it
// never shortens a backoff already running.
func (r *replayLoop) kick() {
	if r.probing.Load() {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// nextReplayDelay is the retry gap after an attempt that left batches on
// disk: doubled after a failure up to max, back to first otherwise.
func nextReplayDelay(cur, first, max time.Duration, failed bool) time.Duration {
	if !failed {
		return first
	}
	cur *= 2
	if cur > max {
		return max
	}
	return cur
}

func (r *replayLoop) loop() {
	defer r.wg.Done()
	delay := r.firstDelay
	if n, _ := r.pending(); n > 0 {
		r.probing.Store(true)
	} else {
		delay = r.maxDelay
	}
	// tick is armed once per state change, so a stray wake never pushes
	// the pending probe back.
	tick := r.after(delay)
	for {
		select {
		case <-r.quit:
			return
		case <-r.wake:
			if !r.probing.CompareAndSwap(false, true) {
				continue
			}
			delay = r.firstDelay
			tick = r.after(delay)
			continue
		case <-tick:
		}

		if n, waited := r.pending(); n > 0 && r.logger != nil {
			r.logger.Info().Int("records", n).Dur("waiting", waited).Msg(r.label + ": retrying the queued records")
		}
		_, err := r.run()

		if n, _ := r.pending(); n > 0 {
			r.probing.Store(true)
			delay = nextReplayDelay(delay, r.firstDelay, r.maxDelay, err != nil)
		} else {
			r.probing.Store(false)
			delay = r.maxDelay
		}
		tick = r.after(delay)
	}
}
