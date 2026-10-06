package lifecycle

import (
	"context"
	"errors"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// Supervisor starts a fixed set of services in order and stops them in
// reverse, giving each its own stop budget.
//
// It owns the run context: Start derives a cancellable context from the
// caller's, hands it to every service, and cancels it in Shutdown before
// the first Shutdown call. Services therefore see cancellation first and
// Shutdown second — the ordering the contract promises, and the reason
// no service needs a quit channel of its own.
type Supervisor struct {
	logger *logger.ModuleLogger

	mu      sync.Mutex
	started []Service
	cancel  context.CancelFunc
}

// NewSupervisor builds a supervisor logging under the "lifecycle" module.
func NewSupervisor(baseLogger *logger.Logger) *Supervisor {
	return &Supervisor{logger: logger.NewModuleLogger(baseLogger, "lifecycle")}
}

// Start brings up services in the given order and records the ones that
// came up, so Shutdown never calls a service that failed to start.
//
// A failing service does not abort the sequence: the agent starts what
// it can and reports the failures to the caller, which decides whether
// a partial agent is worth keeping alive. The returned slice is nil when
// everything started.
func (s *Supervisor) Start(ctx context.Context, services ...Service) []error {
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	var errs []error
	for _, svc := range services {
		s.logger.Debug().Str("service", svc.GetName()).Msg("Starting service")

		if err := svc.Start(runCtx); err != nil {
			s.logger.Error().
				Str("service", svc.GetName()).
				Err(err).
				Msg("Failed to start service")
			errs = append(errs, err)
			continue
		}

		s.mu.Lock()
		s.started = append(s.started, svc)
		s.mu.Unlock()

		s.logger.Info().Str("service", svc.GetName()).Msg("Service started")
	}
	return errs
}

// Shutdown cancels the run context, then stops the started services in
// reverse order. Each gets a context bounded by its own StopBudget, so a
// slow drain is contained: it can overrun its own budget and still leave
// every later service the full allowance. The caller's ctx remains the
// outer bound — a deadline shorter than a service's budget still wins.
//
// Reverse order matters beyond symmetry: producers (probes) stop before
// the consumer (data store), so the last collected batch reaches a
// strategy that is still accepting.
func (s *Supervisor) Shutdown(ctx context.Context) []error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	services := s.started
	s.started = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var errs []error
	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		budget := stopBudget(svc)

		s.logger.Debug().
			Str("service", svc.GetName()).
			Dur("budget", budget).
			Msg("Shutting down service")

		started := time.Now()
		svcCtx, svcCancel := context.WithTimeout(ctx, budget)
		err := shutdownBounded(svcCtx, svc, s.logger, budget)
		svcCancel()

		if err != nil {
			// A backend that does not answer in time is not a failed
			// shutdown: the service stopped, within its budget, without
			// delivering what it still held. Reporting it as a failure
			// made a clean stop print "forced to shutdown with error",
			// which reads like a crash in the journal.
			if errors.Is(err, context.DeadlineExceeded) {
				s.logger.Warn().
					Str("service", svc.GetName()).
					Dur("duration", time.Since(started)).
					Err(err).
					Msg("Service stopped without delivering everything it still held: its backend did not answer in time")
				continue
			}
			s.logger.Error().
				Str("service", svc.GetName()).
				Dur("duration", time.Since(started)).
				Err(err).
				Msg("Failed to shut down service")
			errs = append(errs, err)
			continue
		}
		s.logger.Info().
			Str("service", svc.GetName()).
			Dur("duration", time.Since(started)).
			Msg("Service shut down")
	}
	return errs
}

// TotalStopBudget is the wall-clock the supervisor needs at worst to
// stop the given services: every budget spent in full, one after the
// other. The process entry point uses it to size the context it hands
// to Shutdown, so the last service in the sequence is not handed an
// already-expired deadline (the failure mode of the single global
// budget this replaces).
func TotalStopBudget(services ...Service) time.Duration {
	var total time.Duration
	for _, svc := range services {
		total += stopBudget(svc)
	}
	return total
}

// shutdownGrace is how long past its deadline a service may take to
// notice it. A Shutdown that ignores its context would otherwise hold the
// whole stop past what the service manager allows, and the process would
// be killed with nothing flushed.
const shutdownGrace = 250 * time.Millisecond

// shutdownBounded runs svc.Shutdown and stops waiting for it shortly
// after ctx is done. The abandoned call keeps running; the process is
// about to exit.
func shutdownBounded(ctx context.Context, svc Service, log *logger.ModuleLogger, budget time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- svc.Shutdown(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	select {
	case err := <-done:
		return err
	case <-time.After(shutdownGrace):
		log.Warn().
			Str("service", svc.GetName()).
			Dur("budget", budget).
			Msg("Service did not stop within its budget and was abandoned: what it still held is lost")
		return context.DeadlineExceeded
	}
}
