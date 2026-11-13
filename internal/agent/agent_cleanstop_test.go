package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/lifecycle"
)

type cleanStopGuard struct {
	stopped atomic.Int32
}

func (g *cleanStopGuard) RolledBack() bool                   { return false }
func (g *cleanStopGuard) ConfirmWhenHealthy(context.Context) {}
func (g *cleanStopGuard) StoppedCleanly()                    { g.stopped.Add(1) }

func TestShutdownCallsStoppedCleanly(t *testing.T) {
	g := &cleanStopGuard{}
	a := agent{
		supervisor: lifecycle.NewSupervisor(noopLogger()),
		logger:     noopLogger(),
		boot:       &bootSlot{guard: g},
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := g.stopped.Load(); got != 1 {
		t.Fatalf("StoppedCleanly called %d times, want 1", got)
	}
}

func TestShutdownWithoutBootDoesNotPanic(t *testing.T) {
	a := agent{supervisor: lifecycle.NewSupervisor(noopLogger()), logger: noopLogger()}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	a.boot = &bootSlot{}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with an empty slot: %v", err)
	}
}
