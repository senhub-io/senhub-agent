package agent

import (
	"context"
	"errors"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// First orchestration tests of the agent lifecycle — seeded by #265.

type fakeService struct {
	name     string
	startErr error
	events   *[]string
}

func (f *fakeService) GetName() string { return f.name }
func (f *fakeService) Start(chan struct{}) error {
	*f.events = append(*f.events, "start:"+f.name)
	return f.startErr
}
func (f *fakeService) Shutdown(context.Context) error {
	*f.events = append(*f.events, "stop:"+f.name)
	return nil
}

func newLifecycleAgent() (agent, *[]string) {
	events := &[]string{}
	return agent{
		startedServices: &[]Service{},
		messageChannel:  make(chan struct{}),
		logger:          logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}),
	}, events
}

// TestShutdown_ReverseStartOrder pins #265 (A6): services shut down in
// REVERSE start order — producers (sensors) before consumers
// (DataStore) — so the final flush still has a live pipeline.
// Start-order shutdown closed the store first and lost every datapoint
// emitted during a clean stop.
func TestShutdown_ReverseStartOrder(t *testing.T) {
	a, events := newLifecycleAgent()
	services := []Service{
		&fakeService{name: "config", events: events},
		&fakeService{name: "store", events: events},
		&fakeService{name: "sensors", events: events},
	}
	if err := a.startServices(services); err != nil {
		t.Fatalf("startServices: %v", err)
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	want := []string{
		"start:config", "start:store", "start:sensors",
		"stop:sensors", "stop:store", "stop:config",
	}
	if len(*events) != len(want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
	for i := range want {
		if (*events)[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (full: %v)", i, (*events)[i], want[i], *events)
		}
	}
}

// TestStartServices_PropagatesFailure pins #265 (A8): a service that
// fails to start surfaces as an error to the caller (and from there a
// non-zero process exit) — the historical SIGINT-to-self path exited 0
// and turned permanent misconfigurations into infinite restart loops.
func TestStartServices_PropagatesFailure(t *testing.T) {
	a, events := newLifecycleAgent()
	boom := errors.New("bind: address already in use")
	services := []Service{
		&fakeService{name: "ok", events: events},
		&fakeService{name: "broken", startErr: boom, events: events},
	}

	err := a.startServices(services)
	if err == nil {
		t.Fatal("startServices returned nil for a failed service")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error chain lost the cause: %v", err)
	}
	// The healthy service is still recorded for shutdown.
	if got := len(*a.startedServices); got != 1 {
		t.Errorf("startedServices = %d, want 1", got)
	}
}
