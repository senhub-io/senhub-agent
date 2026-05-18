package periodic_scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

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
	// Number of retries on error
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
	mutex       sync.Mutex // Protects probe operations
}

func NewPeriodicScheduler(config PeriodicSchedulerConfig, logger *logger.Logger) PeriodicScheduler {
	return &periodicScheduler{
		started: false,
		logger:  logger,
		config:  config,
	}
}

func (l *periodicScheduler) Start(quitChannel chan struct{}) error {
	if l.started {
		return nil
	}

	l.logger.Info().Msg("Starting")
	l.started = true
	l.quitChannel = quitChannel

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

	go func(ticker *time.Ticker) {
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
			} else {
				// Clean exit without quit signal is also abnormal —
				// the for-select should only exit via quitChannel.
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
				tickStarted := time.Now()
				l.logger.Info().
					Int("error_count", errorCount).
					Msg("scheduler tick → calling doCall")
				err := l.doCall()
				if err != nil {
					errorCount++
					if errorCount < l.config.MaxRetries {
						l.logger.Warn().
							Err(err).
							Dur("duration", time.Since(tickStarted)).
							Int("error_count", errorCount).
							Int("max_retry", l.config.MaxRetries).
							Msg("doCall returned error (will retry)")
					} else {
						l.logger.Error().
							Err(err).
							Dur("duration", time.Since(tickStarted)).
							Int("error_count", errorCount).
							Int("max_retry", l.config.MaxRetries).
							Msg("Max retries reached, shutting down")
						if err := l.Shutdown(context.Background()); err != nil {
							l.logger.Error().Err(err).Msg("Failed to shutdown scheduler")
						}
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
				}
			case <-l.quitChannel:
				l.logger.Info().Msg("Scheduler goroutine terminating on quit signal")
				return
			}
		}
	}(l.ticker)

	return nil
}

func (l *periodicScheduler) Shutdown(ctx context.Context) error {
	if !l.started {
		return nil
	}

	l.logger.Debug().Msg("Shutting down")
	l.started = false

	if l.ticker != nil {
		l.ticker.Stop()
		l.ticker = nil
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
