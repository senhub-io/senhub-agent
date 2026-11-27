package otlp

import (
	"context"
	"strings"
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

	// hints is the throttle hint the signal's transport saw; nil when the
	// transport has no hook (the relays carry theirs in the error).
	hints *hintSink
	// backlog reports whether anything waits on disk.
	backlog func() bool
	// throttledUntilNs is when the server asked the agent to try again, as
	// a unix time; live exports and replay probes both wait for it.
	throttledUntilNs atomic.Int64
	// throttleNotedNs is when the latest hint was noted.
	throttleNotedNs atomic.Int64
	// throttled is set for the length of one throttling episode: from the
	// first hint until an export is acknowledged. It keeps the episode to
	// one warning however many batches the server turns away.
	throttled atomic.Bool
	// capWarned keeps the "hint above the cap" warning to once per episode.
	capWarned atomic.Bool

	// recovering keeps one recovery callback in flight, whatever the backend's
	// flapping; recoverWG lets the replayer wait for it at stop, and
	// recoveryClosed ends the callbacks for good.
	recovering     atomic.Bool
	recoverWG      sync.WaitGroup
	recoveryClosed atomic.Bool
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
	if h.stopping.Load() || h.throttleRemaining() > 0 {
		return false
	}
	now := h.now().UnixNano()
	last := h.lastFailureNs.Load()
	if now-last < int64(h.probeInterval) {
		return false
	}
	return h.lastFailureNs.CompareAndSwap(last, now)
}

func (h *exporterHealth) markSuccess() { h.markSuccessSince(h.now()) }

// markSuccessSince records an acknowledged export that began at started. An
// export that began before the server's hint was noted does not lift the
// wait the hint asked for: it was already in flight when the server said
// slow down, so it proves nothing about the pace the server now wants. Live
// exports stay held and newer batches do not overtake the backlog.
func (h *exporterHealth) markSuccessSince(started time.Time) { h.recordSuccess(started, true) }

// markReplaySuccess is markSuccessSince for the replayer's own exports, which
// must not start another replay behind themselves.
func (h *exporterHealth) markReplaySuccess(started time.Time) { h.recordSuccess(started, false) }

func (h *exporterHealth) recordSuccess(started time.Time, kickBacklog bool) {
	agentstate.RecordExportSuccess(h.successKey)
	h.lastSuccessNs.Store(h.now().UnixNano())
	if noted := h.throttleNotedNs.Load(); noted != 0 && started.UnixNano() < noted && h.throttleRemaining() > 0 {
		return
	}
	h.throttledUntilNs.Store(0)
	h.throttled.Store(false)
	h.capWarned.Store(false)
	// Export succeeded: if we were unhealthy, the backend just recovered.
	// A live export that gets through while a backlog waits says the same,
	// whatever the health flag held, so the backlog is not left for the
	// retry clock.
	recovered := h.healthy.CompareAndSwap(false, true)
	if recovered || (kickBacklog && h.backlog != nil && h.backlog()) {
		h.runRecovered()
	}
}

func (h *exporterHealth) runRecovered() {
	p := h.onRecovered.Load()
	if p == nil || *p == nil || h.recoveryClosed.Load() {
		return
	}
	if !h.recovering.CompareAndSwap(false, true) {
		return // one is running; the replay it drives picks up the rest
	}
	h.recoverWG.Add(1)
	go func() {
		defer h.recoverWG.Done()
		defer h.recovering.Store(false)
		(*p)()
	}()
}

// closeRecovery stops the recovery callbacks and waits for the one in flight.
func (h *exporterHealth) closeRecovery() {
	h.recoveryClosed.Store(true)
	h.recoverWG.Wait()
}

// signalName is the signal this health belongs to, from its success key.
func (h *exporterHealth) signalName() string {
	return strings.TrimPrefix(h.successKey, "otlp/")
}

// throttleRemaining is how long the server asked the agent to wait still.
func (h *exporterHealth) throttleRemaining() time.Duration {
	until := h.throttledUntilNs.Load()
	if until == 0 {
		return 0
	}
	if d := time.Duration(until - h.now().UnixNano()); d > 0 {
		return d
	}
	return 0
}

// noteThrottle reads the server's hint out of a failed export (the error
// itself, or what the transport saw) and, when there is one, holds every
// further attempt until it has passed. It reports whether the failure was a
// throttle. The wait is capped at maxThrottleHint. The counter moves once
// per throttled export; the warning once per episode.
func (h *exporterHealth) noteThrottle(err error) bool {
	now := h.now()
	var wait time.Duration
	if te, ok := asThrottled(err); ok && te.wait > wait {
		wait = te.wait
	}
	if d, ok := retryInfoDelay(err); ok && d > wait {
		wait = d
	}
	if d := h.hints.remaining(now); d > wait {
		wait = d
	}
	if wait <= 0 {
		return false
	}
	asked := wait
	if wait > maxThrottleHint {
		wait = maxThrottleHint
		if h.logger != nil && h.capWarned.CompareAndSwap(false, true) {
			h.logger.Warn().Str("signal", h.signalName()).Dur("asked", asked).Dur("applied", wait).
				Msg("OTLP backend asked for a longer wait than the agent allows; the wait is capped")
		}
	}
	until := now.Add(wait).UnixNano()
	for {
		cur := h.throttledUntilNs.Load()
		if until <= cur || h.throttledUntilNs.CompareAndSwap(cur, until) {
			break
		}
	}
	h.throttleNotedNs.Store(now.UnixNano())
	agentstate.IncrementOTLPThrottled(h.signalName())
	if h.throttled.CompareAndSwap(false, true) && h.logger != nil {
		h.logger.Warn().Str("signal", h.signalName()).Dur("retry_after", wait).
			Msg("OTLP backend is throttling this signal; new data is kept on disk and sent again when the wait is over")
	}
	return true
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
	replayMaxDelay   = time.Minute
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

	// hold returns how long the server asked the agent to wait still; the
	// loop never probes before it has passed. Nil means no hint source.
	hold func() time.Duration

	firstDelay time.Duration
	maxDelay   time.Duration
	// after is the retry clock; tests replace it.
	after func(time.Duration) <-chan time.Time

	ctx      context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	quit     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
	// afterStop runs once the loops have ended.
	afterStop func()
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

// stop ends the retry loop and aborts a replay in flight. Safe to call twice.
func (r *replayLoop) stop() {
	r.stopOnce.Do(func() {
		r.cancel()
		close(r.quit)
		r.wg.Wait()
		if r.afterStop != nil {
			r.afterStop()
		}
	})
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

// armed is the retry gap, stretched to the server's hint when that is longer.
func (r *replayLoop) armed(delay time.Duration) time.Duration {
	if r.hold != nil {
		if w := r.hold(); w > delay {
			return w
		}
	}
	return delay
}

func (r *replayLoop) loop() {
	defer r.wg.Done()
	delay := r.firstDelay
	// tick is armed once per state change, so a stray wake never pushes
	// the pending probe back. An empty queue arms nothing: every queued
	// batch kicks the loop, so an idle agent does not wake to find nothing.
	var tick <-chan time.Time
	if n, _ := r.pending(); n > 0 {
		r.probing.Store(true)
		tick = r.after(r.armed(delay))
	}
	for {
		select {
		case <-r.quit:
			return
		case <-r.wake:
			if !r.probing.CompareAndSwap(false, true) {
				continue
			}
			delay = r.firstDelay
			tick = r.after(r.armed(delay))
			continue
		case <-tick:
		}
		// A hint can arrive while the clock runs (a live export was
		// throttled): the probe waits for it too.
		if r.hold != nil {
			if w := r.hold(); w > 0 {
				tick = r.after(w)
				continue
			}
		}

		if n, waited := r.pending(); n > 0 && r.logger != nil {
			r.logger.Info().Int("records", n).Dur("waiting", waited).Msg(r.label + ": retrying the queued records")
		}
		_, err := r.run()

		if n, _ := r.pending(); n > 0 {
			r.probing.Store(true)
			delay = nextReplayDelay(delay, r.firstDelay, r.maxDelay, err != nil)
			tick = r.after(r.armed(delay))
		} else {
			r.probing.Store(false)
			tick = nil
			// A batch queued while probing was still true sent no kick:
			// look once more now that a kick would get through.
			if n, _ := r.pending(); n > 0 && r.probing.CompareAndSwap(false, true) {
				delay = r.firstDelay
				tick = r.after(r.armed(delay))
			}
		}
	}
}
