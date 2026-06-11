package periodic_scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// maxBackoffTicks caps the failure backoff: at worst the scheduler
// attempts once every (maxBackoffTicks+1) intervals. With the typical
// 30-60s sync intervals that is a few minutes between attempts —
// degraded, visible in logs and health, but never dead.
const maxBackoffTicks = 8

type PeriodicSchedulerCall func() error
type PeriodicSchedulerOnStart func(quitChannel chan struct{}) error
type PeriodicSchedulerOnShutdown func(ctx context.Context) error

type PeriodicSchedulerConfig struct {
	// Should the call be made on start
	ExecuteOnStart bool
	// Should the call be made on stop
	ExecuteOnShutdown bool
	// Should the start fail if the initial call fails
	FailOnStartError bool
	// interval between calls
	Interval time.Duration
	// MaxRetries is the consecutive-failure threshold after which the
	// scheduler backs off (skipping ticks, exponentially up to
	// maxBackoffTicks) while CONTINUING to retry. Zero or negative
	// means no threshold: retry every tick forever. The scheduler
	// never shuts itself down on Execute errors (#258).
	MaxRetries int
	// Execute to be made periodically
	Execute PeriodicSchedulerCall
	// Call to always be made on start (optional)
	OnStart PeriodicSchedulerOnStart
	// Call to always be made on shutdown (optional)
	OnShutdown PeriodicSchedulerOnShutdown
}

type PeriodicScheduler interface {
	GetInterval() time.Duration
	Start(quitChannel chan struct{}) error
	Shutdown(ctx context.Context) error
}

type periodicScheduler struct {
	started     bool
	logger      *logger.Logger
	config      PeriodicSchedulerConfig
	ticker      *time.Ticker
	quitChannel chan struct{}
	stopChannel chan struct{}
	// lifecycleMutex serializes Start/Shutdown and guards
	// started/ticker/quitChannel/stopChannel. It is distinct from
	// mutex (which only serializes Execute) so a Shutdown can wait
	// for an in-flight tick without lock-order inversion: the tick
	// goroutine only ever takes mutex, never lifecycleMutex.
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

func (l *periodicScheduler) Start(quitChannel chan struct{}) error {
	l.lifecycleMutex.Lock()
	defer l.lifecycleMutex.Unlock()

	if l.started {
		return nil
	}

	l.logger.Info().Msg("Starting")
	l.started = true
	l.quitChannel = quitChannel
	l.stopChannel = make(chan struct{})

	if l.config.OnStart != nil {
		l.logger.Info().Msg("On start call")
		if err := l.config.OnStart(quitChannel); err != nil {
			return fmt.Errorf("OnStart failed: %v", err)
		}
	}

	if l.config.ExecuteOnStart {
		l.logger.Info().Msg("Initial call")
		// Do onStart call
		if err := l.doCall(); err != nil && l.config.FailOnStartError {
			if l.config.FailOnStartError {
				return fmt.Errorf("Initial call failed: %v", err)
			}

			l.logger.Error().Err(err).Msg("Initial call failed")
		}
	}

	if err := l.setupIntervalCall(); err != nil {
		return fmt.Errorf("Unable to setup interval call %v", err)
	}

	return nil
}

func (l *periodicScheduler) doCall() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if err := l.config.Execute(); err != nil {
		return err
	}

	return nil
}

func (l *periodicScheduler) setupIntervalCall() error {
	if l.config.Interval == 0 {
		l.logger.Info().Msg("No interval set")
		return nil
	}

	l.ticker = time.NewTicker(l.config.Interval)
	errorCount := 0
	backoffTicks := 0  // current backoff width in ticks (0 = none)
	skipRemaining := 0 // ticks left to skip before the next attempt

	// quit and stop are captured here, not read from the struct inside
	// the goroutine: a Shutdown/Start restart cycle replaces those
	// fields, and a previous goroutine reading them would race.
	quit := l.quitChannel
	stop := l.stopChannel

	go func(ticker *time.Ticker, quit, stop chan struct{}) {
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
				// Clean exit without quit/stop signal is abnormal —
				// the for-select should only exit via quitChannel or
				// the Shutdown stop channel.
				l.logger.Warn().Msg("scheduler goroutine exited without quit signal — probe is now stalled")
			}
		}()

		l.logger.Info().
			Dur("interval", l.config.Interval).
			Int("max_retries", l.config.MaxRetries).
			Msg("scheduler tick loop started")

		for {
			select {
			case <-ticker.C:
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
				l.logger.Info().
					Int("error_count", errorCount).
					Msg("scheduler tick → calling doCall")
				err := l.doCall()
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
					l.logger.Info().
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
			case <-quit:
				stopped = true
				l.logger.Info().Msg("Scheduler goroutine terminating on quit signal")
				return
			case <-stop:
				stopped = true
				l.logger.Info().Msg("Scheduler goroutine terminating on shutdown")
				return
			}
		}
	}(l.ticker, quit, stop)

	return nil
}

func (l *periodicScheduler) Shutdown(ctx context.Context) error {
	l.lifecycleMutex.Lock()
	defer l.lifecycleMutex.Unlock()

	if !l.started {
		return nil
	}

	l.logger.Debug().Msg("Shutting down")
	l.started = false

	if l.ticker != nil {
		l.ticker.Stop()
		l.ticker = nil
	}

	if l.stopChannel != nil {
		close(l.stopChannel)
		l.stopChannel = nil
	}

	if l.config.ExecuteOnShutdown {
		l.logger.Info().Msg("Final call")
		if err := l.doCall(); err != nil {
			return fmt.Errorf("Unable to call Execute on shutdown: %v", err)
		}
	}

	if l.config.OnShutdown != nil {
		l.logger.Info().Msg("OnShutdown call")
		if err := l.config.OnShutdown(ctx); err != nil {
			return fmt.Errorf("OnShutdown failed: %v", err)
		}
	}

	l.logger.Info().Msg("Shut down")

	return nil
}

func (l *periodicScheduler) GetInterval() time.Duration {
	return l.config.Interval
}
