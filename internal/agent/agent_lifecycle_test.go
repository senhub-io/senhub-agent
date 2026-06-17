package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// fakeService implements Service with configurable start/shutdown behaviour.
// shutdownOrder is a pointer shared across all fakes so callers can observe
// the global teardown sequence without coordination overhead.
type fakeService struct {
	name          string
	startErr      error
	shutdownErr   error
	shutdownOrder *[]string
}

func (f *fakeService) GetName() string { return f.name }

func (f *fakeService) Start(_ chan struct{}) error {
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
	services := []Service{svcA, svcB, svcC}

	a := agent{
		startedServices: &services,
		messageChannel:  make(chan struct{}),
		logger:          noopLogger(),
		exitFn:          func(int) { t.Fatal("exitFn called unexpectedly") },
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
// fails to start the injected exitFn is called with code 1, and that only
// the services which started successfully before the failure are tracked.
func TestStart_PartialFailure_PropagatesExitCode(t *testing.T) {
	order := &[]string{}
	exitCode := -1

	svcA := &fakeService{name: "A", shutdownOrder: order}
	svcB := &fakeService{name: "B", startErr: errors.New("boom"), shutdownOrder: order}
	svcC := &fakeService{name: "C", shutdownOrder: order}

	services := []Service{}
	a := agent{
		startedServices: &services,
		messageChannel:  make(chan struct{}),
		logger:          noopLogger(),
		exitFn:          func(code int) { exitCode = code },
	}

	// Mirror the loop in agent.Start to exercise handleStartError in isolation,
	// without triggering the version check or any real service bring-up.
	for _, svc := range []Service{svcA, svcB, svcC} {
		if err := svc.Start(a.messageChannel); err != nil {
			a.handleStartError()
			break
		}
		*a.startedServices = append(*a.startedServices, svc)
	}

	if exitCode != 1 {
		t.Errorf("expected exit code 1 on start failure, got %d", exitCode)
	}
	// Only A was added before B failed; C was never reached.
	if len(*a.startedServices) != 1 || (*a.startedServices)[0].GetName() != "A" {
		t.Errorf("startedServices = %v, want [A]", *a.startedServices)
	}
}

// TestLifecycle_AllGreen exercises the happy path: three services start in
// order and Shutdown walks them in reverse (sensors → datastore → localcfg).
func TestLifecycle_AllGreen(t *testing.T) {
	order := &[]string{}
	svcA := &fakeService{name: "localcfg", shutdownOrder: order}
	svcB := &fakeService{name: "datastore", shutdownOrder: order}
	svcC := &fakeService{name: "sensors", shutdownOrder: order}
	services := []Service{}

	a := agent{
		startedServices: &services,
		messageChannel:  make(chan struct{}),
		logger:          noopLogger(),
		exitFn:          func(int) { t.Fatal("exitFn called unexpectedly") },
	}

	for _, svc := range []Service{svcA, svcB, svcC} {
		if err := svc.Start(a.messageChannel); err != nil {
			t.Fatalf("unexpected start error for %s: %v", svc.GetName(), err)
		}
		*a.startedServices = append(*a.startedServices, svc)
	}

	if len(*a.startedServices) != 3 {
		t.Fatalf("want 3 started services, got %d", len(*a.startedServices))
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}

	want := []string{"sensors", "datastore", "localcfg"}
	if !reflect.DeepEqual(*order, want) {
		t.Errorf("shutdown order = %v, want %v", *order, want)
	}
}
