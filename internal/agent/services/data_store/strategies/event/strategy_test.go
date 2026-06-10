package event

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	eventFormatter "senhub-agent.go/internal/agent/formats/event"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

// First tests of the event strategy — seeded by #261, which found two
// verified correctness bugs in untested code: a config-load panic and
// an infinite busy-spin in the batch drain loop.

type stubAgentConfig struct{}

func (stubAgentConfig) GetAuthenticationKey() string     { return "test-key" }
func (stubAgentConfig) GetServerUrl() string             { return "http://unused" }
func (stubAgentConfig) GetGlobalTags() map[string]string { return nil }

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// TestNewEventSyncStrategy_InvalidServerURL pins #261 (1): a missing
// or non-string server_url is a clean constructor error — the
// unchecked type assertion used to panic the agent at config load.
func TestNewEventSyncStrategy_InvalidServerURL(t *testing.T) {
	cases := map[string]configuration.StorageConfigParams{
		"missing":    {},
		"non-string": {"server_url": 42},
		"empty":      {"server_url": ""},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			strategy, err := NewEventSyncStrategy(stubAgentConfig{}, params, testBaseLogger())
			if err == nil {
				t.Fatal("expected a configuration error, got nil")
			}
			if strategy != nil {
				t.Error("expected nil strategy on configuration error")
			}
			if !strings.Contains(err.Error(), "server_url") {
				t.Errorf("error should name the offending parameter: %v", err)
			}
		})
	}
}

func TestNewEventSyncStrategy_Valid(t *testing.T) {
	strategy, err := NewEventSyncStrategy(stubAgentConfig{},
		configuration.StorageConfigParams{"server_url": "http://127.0.0.1:9"}, testBaseLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy == nil {
		t.Fatal("nil strategy")
	}
}

// TestDoSync_OversizedBatchTerminates pins #261 (2): once a batch
// exceeds syncTriggerBytes, the drain loop used to re-receive and put
// back the same event forever (unlabeled break) — doSync never
// returned, syncInProgress stayed true, the pipeline was dead. The
// loop must terminate, send what fits, and a subsequent sync must
// drain the remainder.
func TestDoSync_OversizedBatchTerminates(t *testing.T) {
	received := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	baseLogger := testBaseLogger()
	s := &EventSyncStrategy{
		buffer:           make(chan eventtypes.EventDataPoint, 16),
		server:           server.NewServer("test-key", srv.URL, baseLogger),
		logger:           logger.NewModuleLogger(baseLogger, "strategy.event.test"),
		formatter:        eventFormatter.NewFormatter(),
		syncTriggerSize:  100,
		syncTriggerBytes: 64, // tiny: the second event always overflows
	}
	s.currentSize.Store(0)
	s.syncInProgress.Store(false)

	// Three events of ~40 bytes each: batch #1 fits one event, the
	// second put-back used to spin forever.
	for i := 0; i < 3; i++ {
		s.buffer <- eventtypes.EventDataPoint{"message": strings.Repeat("x", 30), "n": i}
	}
	s.currentSize.Store(150)

	done := make(chan error, 1)
	go func() { done <- s.doSync() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("doSync: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("doSync did not return — oversized batch busy-spin (#261)")
	}

	// Recovery: subsequent syncs drain the remainder.
	for i := 0; i < 2; i++ {
		select {
		case err := <-runSync(s):
			if err != nil {
				t.Fatalf("recovery doSync: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("recovery doSync did not return")
		}
	}

	if len(s.buffer) != 0 {
		t.Errorf("buffer not drained after recovery syncs: %d left", len(s.buffer))
	}
	if received < 2 {
		t.Errorf("server received %d batches, expected at least 2", received)
	}
	if got := s.currentSize.Load(); got > 30 {
		t.Errorf("currentSize = %d after draining; drained bytes must be subtracted (#261 adjacent)", got)
	}
}

func runSync(s *EventSyncStrategy) chan error {
	done := make(chan error, 1)
	go func() { done <- s.doSync() }()
	return done
}
