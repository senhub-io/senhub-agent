package lifecycle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

type stubService struct {
	name string
	err  error
}

func (s *stubService) GetName() string                { return s.name }
func (s *stubService) Start(context.Context) error    { return nil }
func (s *stubService) Shutdown(context.Context) error { return s.err }

// A collector that does not answer in time leaves the final flush
// unfinished. The agent stopped, within its budget: reporting that as a
// failed shutdown printed "forced to shutdown with error" on a clean
// stop, which reads like a crash in the journal. Pins #861.
func TestATimedOutFlushIsNotAFailedShutdown(t *testing.T) {
	log := logger.NewLogger(&cliArgs.ParsedArgs{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := NewSupervisor(log)
	slow := &stubService{name: "DataStore", err: fmt.Errorf("metric exporter shutdown: %w", context.DeadlineExceeded)}
	s.Start(ctx, slow)
	if errs := s.Shutdown(ctx); len(errs) != 0 {
		t.Errorf("a backend that did not answer must not make the shutdown a failure, got %v", errs)
	}

	s2 := NewSupervisor(log)
	broken := &stubService{name: "DataStore", err: fmt.Errorf("the store is corrupt")}
	s2.Start(ctx, broken)
	if errs := s2.Shutdown(ctx); len(errs) != 1 {
		t.Errorf("a real failure must still be reported, got %v", errs)
	}
}
