package periodic_scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/services/logger"
)

// maxBackoffTicks caps the failure backoff: at worst the scheduler
// attempts once every (maxBackoffTicks+1) intervals. With the typical
// 30-60s sync intervals that is a few minutes between attempts —
// degraded, visible in logs and health, but never dead.
const maxBackoffTicks = 8

type PeriodicSchedulerCall func() error
type PeriodicSchedulerContextCall func(ctx context.Context) error
type PeriodicSchedulerOnStart func(quitChannel chan struct{}) error
type PeriodicSchedulerOnShutdown func(ctx context.Context) error

// CallObserver is told how each call was scheduled. The scheduler reports
// it and never acts on it, so it stays free of any metrics package.
type CallObserver interface {
	CallStarted(lag time.Duration) // actual start minus planned start, >= 0
	CallEnded(first bool)          // first is true for the initial call only
	CallOverran()                  // a call was still running after a full interval; reported once per call
}

type PeriodicSchedulerConfig struct {
	// Should the call be made on start
	ExecuteOnStart bool
	// Should the call be made on stop
	ExecuteOnShutdown bool
	// Should the start fail if the initial call fails
	FailOnStartError bool
	// interval between calls
	Interval time.Duration
	// InitialDelay is how long the goroutine waits after its initial
	// call, or before its first tick when no initial call is made,
	// before it starts the ticker: the delay is a phase every later
	// cycle keeps. The initial call itself runs at once, so the first
	// data does not wait for the phase (a poller reading the output
	// right after a start finds it). A stop during the wait ends the
	// goroutine. Zero means no wait.
	InitialDelay time.Duration
	// MaxRetries is the consecutive-failure threshold after which the
	// scheduler backs off (skipping ticks, exponentially up to
	// maxBackoffTicks) while CONTINUING to retry. Zero or negative
	// means no threshold: retry every tick forever. The scheduler
	// never shuts itself down on Execute errors (#258).
	MaxRetries int
	// Execute to be made periodically
	Execute PeriodicSchedulerCall
	// ExecuteContext replaces Execute when set. It receives the
	// scheduler's run context, which Shutdown cancels before waiting
	// for the call in flight, so a call that honours it stops early.
	ExecuteContext PeriodicSchedulerContextCall
	// CallTimeout is the deadline of each call's context. Zero means no
	// deadline. It is independent of Interval: a call running past the
	// interval but not past its deadline is not cut. The loop being
	// single-threaded, no further call starts until this one returns,
	// and the observer is told once the call has outlived the interval.
	CallTimeout time.Duration
	// Observer receives scheduling events for this scheduler. Nil
	// disables all observer notifications.
	Observer CallObserver
	// Call to always be made on start (optional)
	OnStart PeriodicSchedulerOnStart
	// Call to always be made on shutdown (optional)
	OnShutdown PeriodicSchedulerOnShutdown
}

type PeriodicScheduler interface {
	GetInterval() time.Duration
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type periodicScheduler struct {
	started bool
	logger  *logger.Logger
	config  PeriodicSchedulerConfig
	// runCtx is the scheduler's own cancellation root, derived from
	// the context Start received. It is the SINGLE termination signal
	// for everything the scheduler owns: the tick goroutine and any
	// goroutine an OnStart hook parked on the channel derived from it.
	// Cancelling it happens either because the caller cancelled its
	// context or because Shutdown ran — the scheduler itself never
	// cancels it on an Execute error (#258).
	runCtx    context.Context
	runCancel context.CancelFunc
	// inflightCancel mirrors runCancel for Shutdown to reach without the
	// lifecycle lock: Start holds that lock while the initial call runs,
	// and a Shutdown queued behind it could otherwise only cancel that
	// call by waiting for it to end.
	inflightCancel atomic.Pointer[context.CancelFunc]
	// releaseStopChannel closes the OnStart hook channel when the
	// scheduler stops before its context does.
	releaseStopChannel func()
	// done is closed by the tick goroutine on exit. Shutdown waits on
	// it so "stopped" means the goroutine is gone, not merely signalled.
	done chan struct{}
	// lifecycleMutex serializes Start/Shutdown and guards
	// started/ticker/runCtx/runCancel. It is distinct from mutex
	// (which only serializes Execute) so a Shutdown can wait for an
	// in-flight tick without lock-order inversion: the tick goroutine
	// only ever takes mutex, never lifecycleMutex.
	lifecycleMutex sync.Mutex
	mutex          sync.Mutex // Protects probe operations
}

func NewPeriodicScheduler(config PeriodicSchedulerConfig, logger *logger.Logger) PeriodicScheduler {
	return &periodicScheduler{
		started: false,
		logger:  logger,
		config:  config,
	}
}

// Start runs the scheduler until ctx is cancelled or Shutdown is
// called. A nil ctx is treated as context.Background(): the scheduler is
// created in enough places (config-change restarts, strategy
// recreation) that a nil there used to mean "no termination signal at
// all" — now it only means "no external one", and Shutdown still stops
// everything (#270, #285).
func (l *periodicScheduler) Start(ctx context.Context) error {
	l.lifecycleMutex.Lock()
	defer l.lifecycleMutex.Unlock()

	if l.started {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	l.logger.Info().Msg("Starting")
	l.started = true
	l.runCtx, l.runCancel = context.WithCancel(ctx)
	cancel := l.runCancel
	l.inflightCancel.Store(&cancel)

	if l.config.OnStart != nil {
		l.logger.Info().Msg("On start call")
		// OnStart still speaks `chan struct{}` — that is the shape a
		// listener goroutine selects on — but the channel is derived
		// from the scheduler's run context rather than handed in by
		// the caller. There is one cancellation root per scheduler and
		// it is never nil, so a hook goroutine always gets a
		// termination signal, whether the agent context is cancelled
		// or Shutdown runs first.
		stopChannel, release := lifecycle.StopChannel(l.runCtx)
		l.releaseStopChannel = release
		if err := l.config.OnStart(stopChannel); err != nil {
			return fmt.Errorf("OnStart failed: %w", err)
		}
	}

	initialCall := l.config.ExecuteOnStart
	if initialCall && l.config.FailOnStartError {
		initialCall = false
		l.logger.Info().Msg("Initial call")
		if err := l.doCall(l.runCtx); err != nil {
			return fmt.Errorf("Initial call failed: %w", err)
		}
	}

	l.run(initialCall)
	return nil
}

func (l *periodicScheduler) doCall(parent context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	ctx := parent
	if l.config.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, l.config.CallTimeout)
		defer cancel()
	}
	if l.config.Observer != nil && l.config.Interval > 0 {
		finished := make(chan struct{})
		defer close(finished)
		go l.watchOverrun(finished)
	}
	return l.invoke(ctx)
}

func (l *periodicScheduler) invoke(ctx context.Context) error {
	if l.config.ExecuteContext != nil {
		return l.config.ExecuteContext(ctx)
	}
	return l.config.Execute()
}

// watchOverrun reports a call that is still running once a full interval
// has passed since it began: it delays the next tick. The call is neither
// cut nor repeated; its own deadline, if any, is a separate matter.
func (l *periodicScheduler) watchOverrun(finished <-chan struct{}) {
	timer := time.NewTimer(l.config.Interval)
	defer timer.Stop()
	select {
	case <-finished:
	case <-timer.C:
		l.config.Observer.CallOverran()
	}
}

func (l *periodicScheduler) doCallObserved(ctx context.Context, planned time.Time, first bool) error {
	if l.config.Observer != nil {
		lag := time.Since(planned)
		if lag < 0 {
			lag = 0
		}
		l.config.Observer.CallStarted(lag)
		defer l.config.Observer.CallEnded(first)
	}
	return l.doCall(ctx)
}

// run starts the goroutine that makes the initial call, when asked, and
// then one call per interval. The initial call runs there rather than in
// Start: a probe whose first collection hangs on an unreachable target
// would otherwise hold Start, and with it the sensor lock and every
// service started after the sensor.
func (l *periodicScheduler) run(initialCall bool) {
	if l.config.Interval == 0 {
		l.logger.Info().Msg("No interval set")
		if !initialCall {
			return
		}
	}

	// done and the run context are captured here, not read from the
	// struct inside the goroutine: a Shutdown/Start restart cycle
	// replaces those fields, and a previous goroutine reading them
	// would race.
	runCtx := l.runCtx
	done := make(chan struct{})
	l.done = done

	go func(runCtx context.Context, done chan struct{}) {
		defer close(done)
		goroutineStart := time.Now()
		stopped := false
		// Recover from any panic in Execute so a buggy probe collector
		// does not silently kill the scheduler goroutine — historically
		// caused the IBM i probe to stop running cycles after a few
		// minutes with no log entry, no error counter increment, and
		// no visible signal.
		defer func() {
			if r := recover(); r != nil {
				l.logger.Error().
					Interface("panic", r).
					Msgf("scheduler goroutine PANICKED — probe is now stalled forever (restart agent to recover): %v", r)
			} else if !stopped {
				// Clean exit without a cancellation is abnormal — the
				// for-select should only exit when the run context is
				// done (caller cancellation or Shutdown).
				l.logger.Warn().Msg("scheduler goroutine exited without quit signal — probe is now stalled")
			}
		}()

		if initialCall && runCtx.Err() == nil {
			l.logger.Info().Msg("Initial call")
			if err := l.doCallObserved(runCtx, goroutineStart, true); err != nil {
				// The first collect failing is the moment an operator
				// most needs to hear about.
				l.logger.Error().Err(err).Msg("Initial call failed")
			}
		}
		if l.config.Interval == 0 {
			stopped = true
			return
		}

		if l.config.InitialDelay > 0 {
			timer := time.NewTimer(l.config.InitialDelay)
			select {
			case <-timer.C:
			case <-runCtx.Done():
				timer.Stop()
				stopped = true
				l.logger.Info().Msg("Scheduler goroutine terminating on cancellation before its first tick")
				return
			}
		}

		// The ticker starts after the initial call and the phase delay,
		// so the first tick comes one interval after both.
		ticker := time.NewTicker(l.config.Interval)
		defer ticker.Stop()
		errorCount := 0
		backoffTicks := 0  // current backoff width in ticks (0 = none)
		skipRemaining := 0 // ticks left to skip before the next attempt

		l.logger.Info().
			Dur("interval", l.config.Interval).
			Int("max_retries", l.config.MaxRetries).
			Msg("scheduler tick loop started")

		for {
			select {
			case tick := <-ticker.C:
				// Backoff: after MaxRetries consecutive failures the
				// scheduler skips ticks (exponentially, capped) instead
				// of attempting every interval — and NEVER shuts itself
				// down. A monitoring agent must not self-terminate its
				// pipelines on transient failure: the historical
				// self-Shutdown here permanently killed cloud/PRTG push
				// on the first error (MaxRetries unset = 0) and probes
				// after 3 failed collects, with no recovery path (#258).
				if skipRemaining > 0 {
					skipRemaining--
					continue
				}
				tickStarted := time.Now()
				l.logger.Debug().
					Int("error_count", errorCount).
					Msg("scheduler tick → calling doCall")
				err := l.doCallObserved(runCtx, tick, false)
				if err != nil {
					errorCount++
					if l.config.MaxRetries > 0 && errorCount >= l.config.MaxRetries {
						if backoffTicks < maxBackoffTicks {
							backoffTicks = backoffTicks*2 + 1
							if backoffTicks > maxBackoffTicks {
								backoffTicks = maxBackoffTicks
							}
						}
						skipRemaining = backoffTicks
						l.logger.Error().
							Err(err).
							Dur("duration", time.Since(tickStarted)).
							Int("error_count", errorCount).
							Int("max_retry", l.config.MaxRetries).
							Int("backoff_ticks", backoffTicks).
							Msg("Consecutive failures over threshold — backing off, will keep retrying")
					} else {
						l.logger.Warn().
							Err(err).
							Dur("duration", time.Since(tickStarted)).
							Int("error_count", errorCount).
							Int("max_retry", l.config.MaxRetries).
							Msg("doCall returned error (will retry)")
					}
				} else {
					l.logger.Debug().
						Dur("duration", time.Since(tickStarted)).
						Msg("doCall ok")
					if errorCount > 0 {
						l.logger.Info().
							Int("error_count", errorCount).
							Msg("Recovered")
					}
					errorCount = 0
					backoffTicks = 0
					skipRemaining = 0
				}
			case <-runCtx.Done():
				stopped = true
				l.logger.Info().Msg("Scheduler goroutine terminating on cancellation")
				return
			}
		}
	}(runCtx, done)
}

func (l *periodicScheduler) Shutdown(ctx context.Context) error {
	if cancel := l.inflightCancel.Load(); cancel != nil {
		(*cancel)()
	}

	l.lifecycleMutex.Lock()
	defer l.lifecycleMutex.Unlock()
	l.inflightCancel.Store(nil)

	if !l.started {
		return nil
	}

	l.logger.Debug().Msg("Shutting down")
	l.started = false

	// Cancelling the run context is the single stop signal: it ends the
	// tick goroutine and closes the channel the OnStart hook parked its
	// own goroutines on. Both happen before OnShutdown runs, so a hook
	// that tears down a listener never races the goroutine feeding it.
	if l.runCancel != nil {
		l.runCancel()
		l.runCancel = nil
		l.runCtx = nil
	}
	if l.releaseStopChannel != nil {
		l.releaseStopChannel()
		l.releaseStopChannel = nil
	}

	// Wait for the tick goroutine to actually exit, bounded by the
	// caller's budget. Without this, Shutdown returned while a tick was
	// still in flight: the next Start raced it, and a leak test could
	// never observe a settled state.
	settled := true
	if l.done != nil {
		select {
		case <-l.done:
		case <-ctx.Done():
			settled = false
			l.logger.Warn().Msg("Scheduler goroutine still running when the stop budget expired")
		}
		l.done = nil
	}

	// A call still in flight holds the call mutex: a final call would
	// wait on it past the budget.
	if l.config.ExecuteOnShutdown && settled {
		l.logger.Info().Msg("Final call")
		// The run context is already cancelled here; the final call
		// is bounded by the caller's stop budget instead.
		if err := l.doCall(ctx); err != nil {
			return fmt.Errorf("Unable to call Execute on shutdown: %w", err)
		}
	}

	if l.config.OnShutdown != nil {
		l.logger.Info().Msg("OnShutdown call")
		if err := l.config.OnShutdown(ctx); err != nil {
			return fmt.Errorf("OnShutdown failed: %w", err)
		}
	}

	l.logger.Info().Msg("Shut down")

	return nil
}

func (l *periodicScheduler) GetInterval() time.Duration {
	return l.config.Interval
}
