package event

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

// fakeIntake stands in for the cloud server. A real httptest server
// would go through the shared server package, whose transport carries
// its own retry round-tripper — several seconds of backoff per failed
// call, which is a third retry layer and not what these tests are
// about. The fake answers instantly with the status under test.
type fakeIntake struct {
	status   int
	attempts atomic.Int32
}

func (f *fakeIntake) Get(string) (*http.Response, error) { return f.respond() }

func (f *fakeIntake) Post(string, any) (*http.Response, error) { return f.respond() }

func (f *fakeIntake) PostStream(string, string) (*http.Response, error) { return f.respond() }

func (f *fakeIntake) respond() (*http.Response, error) {
	f.attempts.Add(1)
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// newOutageStrategy builds an event strategy talking to a fake intake,
// with a retry backlog small enough that an outage reaches the cap
// inside a test rather than after 100k events.
func newOutageStrategy(t *testing.T, intake *fakeIntake, backlogCap int) *EventSyncStrategy {
	t.Helper()

	s, err := NewEventSyncStrategy(stubAgentConfig{}, configuration.StorageConfigParams{
		"server_url": "http://intake.invalid",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.server = intake
	s.failedEvents = pushqueue.New[eventtypes.EventDataPoint]("event", backlogCap)
	// Disarm the auto-trigger: enqueue fires a background doSync once a
	// threshold is crossed, and that goroutine would race the cycles
	// this test drives by hand — draining the backlog mid-assertion.
	s.syncTriggerSize = 1 << 30
	s.syncTriggerBytes = 1 << 30
	// One attempt, no sleep: the in-tick retry loop is not what these
	// tests are about, and its real delays would cost minutes.
	s.retryAttempts = 1
	s.retryDelay = 0
	return s
}

// TestOutageKeepsRetryBacklogBounded is the scenario #287 exists for.
// The intake is down; every sync fails and the batch goes back on the
// retry backlog while events keep arriving. Before the cap this was a
// plain slice appended to forever — the OOM class the metric sinks
// closed in #267, still live on this sink.
func TestOutageKeepsRetryBacklogBounded(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	agentstate.ResetExportSendFailedForTest()

	intake := &fakeIntake{status: http.StatusServiceUnavailable}

	const backlogCap = 50
	s := newOutageStrategy(t, intake, backlogCap)

	for cycle := 0; cycle < 40; cycle++ {
		enqueueEvents(t, s, 10)
		// A 503 is retryable, so doSync surfaces the error and keeps the
		// batch. That is the branch under test.
		if err := s.doSync(); err == nil {
			t.Fatalf("cycle %d: doSync should surface a retryable failure", cycle)
		}
		if n := s.failedEvents.Len(); n > backlogCap {
			t.Fatalf("cycle %d: retry backlog grew past the cap: %d > %d", cycle, n, backlogCap)
		}
	}

	if n := s.failedEvents.Len(); n != backlogCap {
		t.Fatalf("retry backlog holds %d after the outage, want the cap of %d", n, backlogCap)
	}
	if dropped := agentstate.GetPushBufferDropped()["event"]; dropped == 0 {
		t.Error("the backlog shed events at its cap but the drop counter did not move")
	}

	// The send-failure counter is what makes the outage visible while
	// the backlog is still under its cap and shedding nothing.
	var transportFailures uint64
	for _, f := range agentstate.GetExportSendFailed() {
		if f.Strategy == "event" && f.Reason == agentstate.ExportFailureTransport {
			transportFailures = f.Count
		}
	}
	if transportFailures == 0 {
		t.Error("a sink that failed every send for 40 cycles recorded no transport failure")
	}

	// Recovery: the intake comes back and the surviving backlog drains
	// on the next sync.
	intake.status = http.StatusAccepted

	if err := s.doSync(); err != nil {
		t.Fatalf("doSync after recovery: %v", err)
	}
	if n := s.failedEvents.Len(); n != 0 {
		t.Errorf("retry backlog still holds %d events after recovery, want 0", n)
	}
}

// TestPermanentRejectionDropsBatch pins the other half of the split: a
// payload the intake refuses on its merits must not be re-queued. Left
// on the backlog it would pin the head forever, so the backlog never
// drains and every tick re-sends bytes already refused.
func TestPermanentRejectionDropsBatch(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	agentstate.ResetExportSendFailedForTest()

	intake := &fakeIntake{status: http.StatusUnprocessableEntity}
	s := newOutageStrategy(t, intake, 50)
	// Restore the shipped attempt count: proving the in-tick retry is
	// SKIPPED for a permanent rejection only means something if the
	// loop would otherwise have run.
	s.retryAttempts = DefaultRetryAttempts

	enqueueEvents(t, s, 5)

	if err := s.doSync(); err != nil {
		t.Fatalf("doSync should not surface a permanent rejection as an error, got: %v", err)
	}

	if got := intake.attempts.Load(); got != 1 {
		t.Errorf("intake hit %d times, want exactly 1 (no in-tick retry on a permanent rejection)", got)
	}
	if n := s.failedEvents.Len(); n != 0 {
		t.Errorf("permanently rejected batch was re-queued: backlog holds %d events", n)
	}
	if dropped := agentstate.GetPushBufferDropped()["event"]; dropped != 5 {
		t.Errorf("drop counter = %d, want 5", dropped)
	}

	var validationFailures uint64
	for _, f := range agentstate.GetExportSendFailed() {
		if f.Strategy == "event" && f.Reason == agentstate.ExportFailureValidation {
			validationFailures = f.Count
		}
	}
	if validationFailures != 1 {
		t.Errorf("validation failure counter = %d, want 1", validationFailures)
	}
}

// enqueueEvents pushes n minimal valid events onto the strategy buffer.
func enqueueEvents(t *testing.T, s *EventSyncStrategy, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		s.enqueue(eventtypes.EventDataPoint{
			"timestamp": time.Unix(1_700_000_000+int64(i), 0),
			"host":      "outage-test-host",
			"severity":  "info",
			"message":   "outage test event",
		})
	}
}
