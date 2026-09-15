// Package lifecycle defines the single start/stop contract every agent
// service implements, and the supervisor that drives it.
//
// The contract has one rule that the rest of the agent depends on: a
// service never stops itself. It runs until the context it was started
// with is cancelled, or until Shutdown is called — whichever comes
// first. Both signals originate from the same root context owned by the
// process entry point, so the agent has exactly one cancellation root
// instead of a channel per subsystem.
package lifecycle

import (
	"context"
	"time"
)

// Service is the lifecycle contract.
//
// Start returns once the service is running; it must not block for the
// service's lifetime. Any goroutine a service spawns takes ctx (or a
// channel derived from it with StopChannel) as its termination signal.
//
// Shutdown is given a context carrying the service's own stop budget —
// not a budget shared with its siblings. It must return promptly when
// that context is done, leaving no goroutine parked.
type Service interface {
	GetName() string
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// DefaultStopBudget is the shutdown budget a service gets when it does
// not declare one. Deliberately short: a service that needs longer to
// drain has to say so, so the cost is visible at the declaration rather
// than discovered in a stop that times out.
const DefaultStopBudget = 3 * time.Second

// BudgetedService is the optional capability a service implements when
// DefaultStopBudget is not enough — a strategy that flushes a buffer to
// the network, a probe pool that closes remote connections. The budget
// is per service: the supervisor no longer lets whichever service stops
// first consume the whole process-wide allowance (#285).
type BudgetedService interface {
	Service
	StopBudget() time.Duration
}

// stopBudget resolves the budget for one service.
func stopBudget(s Service) time.Duration {
	if b, ok := s.(BudgetedService); ok {
		if d := b.StopBudget(); d > 0 {
			return d
		}
	}
	return DefaultStopBudget
}

// StopChannel adapts a context to the `chan struct{}` shape that
// listener goroutines and the probe OnStart hook still speak. The
// returned channel is closed exactly once — when ctx is done, or when
// the returned release function is called, whichever happens first.
//
// It is bidirectional rather than receive-only only because
// types.Probe.OnStart takes `chan struct{}`; nothing may send on it.
//
// Callers must always call release (defer it, or wire it into their own
// stop path) so the AfterFunc registration does not outlive the
// goroutine it was created for.
func StopChannel(ctx context.Context) (chan struct{}, func()) {
	ch := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { close(ch) })
	return ch, func() {
		// stop reports true when it prevented the AfterFunc from
		// running, which makes this goroutine the only closer.
		if stop() {
			close(ch)
		}
	}
}
