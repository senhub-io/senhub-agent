package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/services/logger"
)

// fakeService implements Service with configurable start/shutdown behaviour.
// shutdownOrder is a pointer shared across all fakes so callers can observe
// the global teardown sequence without coordination overhead.
type fakeService struct {
	name          string
	startErr      error
	shutdownErr   error
	startedCtx    context.Context
	shutdownOrder *[]string
}

func (f *fakeService) GetName() string { return f.name }

func (f *fakeService) Start(ctx context.Context) error {
	f.startedCtx = ctx
	return f.startErr
}

func (f *fakeService) Shutdown(_ context.Context) error {
	*f.shutdownOrder = append(*f.shutdownOrder, f.name)
	return f.shutdownErr
}

// noopLogger returns a logger that discards all output. It mirrors the
// pattern used in data_store_test.go: NewLogger with an empty ParsedArgs
// (no log path configured → the logger falls back to stderr but produces
// no file I/O, and test output is suppressed by the zerolog level).
func noopLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

// TestShutdown_ReverseOrder verifies that Shutdown tears down services in
// the reverse of start order — sensors before data store before config.
// This ensures producers are stopped before the consumer (the store)
// closes its strategies, preventing loss of the final collection cycle.
func TestShutdown_ReverseOrder(t *testing.T) {
	order := &[]string{}
	svcA := &fakeService{name: "A", shutdownOrder: order}
	svcB := &fakeService{name: "B", shutdownOrder: order}
	svcC := &fakeService{name: "C", shutdownOrder: order}

	sup := lifecycle.NewSupervisor(noopLogger())
	if errs := sup.Start(context.Background(), svcA, svcB, svcC); len(errs) > 0 {
		t.Fatalf("unexpected start errors: %v", errs)
	}

	a := agent{
		supervisor: sup,
		logger:     noopLogger(),
		exitFn:     func(int) { t.Fatal("exitFn called unexpectedly") },
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	want := []string{"C", "B", "A"}
	if !reflect.DeepEqual(*order, want) {
		t.Errorf("shutdown order = %v, want %v", *order, want)
	}
}

// TestStart_PartialFailure_PropagatesExitCode verifies that when one service
// fails to start the injected exitFn is called with code 1, and that the
// failing service is NOT shut down afterwards — only the ones that came up.
func TestStart_PartialFailure_PropagatesExitCode(t *testing.T) {
	order := &[]string{}
	exitCode := -1

	svcA := &fakeService{name: "A", shutdownOrder: order}
	svcB := &fakeService{name: "B", startErr: errors.New("boom"), shutdownOrder: order}
	svcC := &fakeService{name: "C", shutdownOrder: order}

	sup := lifecycle.NewSupervisor(noopLogger())
	a := agent{
		supervisor: sup,
		logger:     noopLogger(),
		exitFn:     func(code int) { exitCode = code },
	}

	if errs := sup.Start(context.Background(), svcA, svcB, svcC); len(errs) > 0 {
		a.handleStartError()
	}

	if exitCode != 1 {
		t.Errorf("expected exit code 1 on start failure, got %d", exitCode)
	}

	// A failing Start does not abort the sequence: C still comes up, and
	// only B is absent from the teardown.
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
	want := []string{"C", "A"}
	if !reflect.DeepEqual(*order, want) {
		t.Errorf("shutdown order = %v, want %v", *order, want)
	}
}

// TestLifecycle_AllGreen exercises the happy path: three services start in
// order and Shutdown walks them in reverse (sensors → datastore → localcfg).
func TestLifecycle_AllGreen(t *testing.T) {
	order := &[]string{}
	svcA := &fakeService{name: "localcfg", shutdownOrder: order}
	svcB := &fakeService{name: "datastore", shutdownOrder: order}
	svcC := &fakeService{name: "sensors", shutdownOrder: order}

	sup := lifecycle.NewSupervisor(noopLogger())
	a := agent{
		supervisor: sup,
		logger:     noopLogger(),
		exitFn:     func(int) { t.Fatal("exitFn called unexpectedly") },
	}

	if errs := sup.Start(context.Background(), svcA, svcB, svcC); len(errs) > 0 {
		t.Fatalf("unexpected start errors: %v", errs)
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	want := []string{"sensors", "datastore", "localcfg"}
	if !reflect.DeepEqual(*order, want) {
		t.Errorf("shutdown order = %v, want %v", *order, want)
	}
}

// TestShutdown_CancelsRunContextBeforeStopping pins the contract every
// service depends on: cancellation reaches the service BEFORE its
// Shutdown is called, so a goroutine parked on the run context is
// already unwinding while the service drains.
func TestShutdown_CancelsRunContextBeforeStopping(t *testing.T) {
	order := &[]string{}
	svc := &fakeService{name: "svc", shutdownOrder: order}

	sup := lifecycle.NewSupervisor(noopLogger())
	if errs := sup.Start(context.Background(), svc); len(errs) > 0 {
		t.Fatalf("unexpected start errors: %v", errs)
	}

	if svc.startedCtx == nil {
		t.Fatal("service was not given a run context")
	}
	select {
	case <-svc.startedCtx.Done():
		t.Fatal("run context was already cancelled while the service was running")
	default:
	}

	if errs := sup.Shutdown(context.Background()); len(errs) > 0 {
		t.Fatalf("unexpected shutdown errors: %v", errs)
	}

	select {
	case <-svc.startedCtx.Done():
	default:
		t.Fatal("run context was not cancelled by Shutdown")
	}
}

// TestShutdown_PerServiceBudget verifies the budget is per service, not
// shared: a service that burns its whole allowance must not leave the
// next one with an already-expired context. This is exactly the failure
// the single 5s global budget produced (#285).
func TestShutdown_PerServiceBudget(t *testing.T) {
	slow := &budgetedFake{fakeService: fakeService{name: "slow", shutdownOrder: &[]string{}}, budget: 40 * time.Millisecond}
	fast := &budgetedFake{fakeService: fakeService{name: "fast", shutdownOrder: &[]string{}}, budget: 40 * time.Millisecond}

	sup := lifecycle.NewSupervisor(noopLogger())
	if errs := sup.Start(context.Background(), slow, fast); len(errs) > 0 {
		t.Fatalf("unexpected start errors: %v", errs)
	}

	// The parent carries the sum of both budgets, the way app/cli.go
	// sizes it from Agent.StopBudget.
	total := lifecycle.TotalStopBudget(slow, fast)
	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()

	if errs := sup.Shutdown(ctx); len(errs) > 0 {
		t.Fatalf("unexpected shutdown errors: %v", errs)
	}

	// fast is shut down last (reverse order puts it first, then slow);
	// what matters is that BOTH saw a live context, not an expired one.
	for _, f := range []*budgetedFake{slow, fast} {
		if f.shutdownCtxErr != nil {
			t.Errorf("%s received an already-expired shutdown context: %v", f.name, f.shutdownCtxErr)
		}
	}
}

type budgetedFake struct {
	fakeService
	budget         time.Duration
	shutdownCtxErr error
}

func (b *budgetedFake) StopBudget() time.Duration { return b.budget }

func (b *budgetedFake) Shutdown(ctx context.Context) error {
	b.shutdownCtxErr = ctx.Err()
	return b.fakeService.Shutdown(ctx)
}
