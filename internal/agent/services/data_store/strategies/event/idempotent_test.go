package event

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

// lossyIntake simulates a server whose response is lost after the data has
// been inserted. It deduplicates incoming events by their "senhub_event_id"
// field, mirroring idempotent server-side handling.
type lossyIntake struct {
	mu             sync.Mutex
	loseFirst      int
	callCount      int
	rawInsertions  int
	inserted       map[string]eventtypes.EventDataPoint
	receivedBodies [][]map[string]any
}

func newLossyIntake(loseFirst int) *lossyIntake {
	return &lossyIntake{
		loseFirst: loseFirst,
		inserted:  make(map[string]eventtypes.EventDataPoint),
	}
}

func (f *lossyIntake) Get(string) (*http.Response, error) {
	return f.respond()
}

func (f *lossyIntake) Post(string, any) (*http.Response, error) {
	return f.respond()
}

func (f *lossyIntake) PostStream(_ string, body string) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callCount++
	var events []map[string]any
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, fmt.Errorf("decoding body: %w", err)
	}
	f.receivedBodies = append(f.receivedBodies, events)

	for _, ev := range events {
		f.rawInsertions++
		id, _ := ev["senhub_event_id"].(string)
		f.inserted[id] = eventtypes.EventDataPoint(ev)
	}

	if f.callCount <= f.loseFirst {
		return nil, errors.New("connection reset")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (f *lossyIntake) respond() (*http.Response, error) {
	// Not used for Get/Post in these tests
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// newLossyIntakeStrategy builds a strategy with a lossyIntake, similar to
// newOutageStrategy but for lossyIntake.
func newLossyIntakeStrategy(t *testing.T, intake *lossyIntake, backlogCap int) *EventSyncStrategy {
	t.Helper()

	s, err := NewEventSyncStrategy(stubAgentConfig{}, configuration.StorageConfigParams{
		"server_url": "http://intake.invalid",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.server = intake
	s.failedEvents = pushqueue.New[eventtypes.EventDataPoint]("event", backlogCap)
	s.syncTriggerSize = 1 << 30
	s.syncTriggerBytes = 1 << 30
	s.retryAttempts = 1
	s.retryDelay = 0
	s.firstRetryDelay = 0
	s.firstRetryJitter = 0
	return s
}

func TestEventIDSetOnceAndKeptAcrossResend(t *testing.T) {
	intake := newLossyIntake(1) // lose first response
	s := newLossyIntakeStrategy(t, intake, 50)

	enqueueEvents(t, s, 3)

	// First doSync fails (response lost), batch requeued.
	if err := s.doSync(context.Background()); err == nil {
		t.Fatal("first doSync should fail because response is lost")
	}
	if s.failedEvents.Len() != 3 {
		t.Fatalf("failedEvents len = %d, want 3", s.failedEvents.Len())
	}

	// Second doSync succeeds.
	if err := s.doSync(context.Background()); err != nil {
		t.Fatalf("second doSync should succeed, got: %v", err)
	}

	// Check both bodies had the same three distinct, non-empty IDs.
	if len(intake.receivedBodies) != 2 {
		t.Fatalf("intake received %d bodies, want 2", len(intake.receivedBodies))
	}
	first := intake.receivedBodies[0]
	second := intake.receivedBodies[1]
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("bodies have lengths %d and %d, want 3 each", len(first), len(second))
	}

	ids := make(map[string]bool)
	for i := 0; i < 3; i++ {
		id1, ok1 := first[i]["senhub_event_id"].(string)
		id2, ok2 := second[i]["senhub_event_id"].(string)
		if !ok1 || !ok2 || id1 == "" || id2 == "" {
			t.Fatalf("event %d missing or empty ID in one of the bodies", i)
		}
		if id1 != id2 {
			t.Errorf("event %d ID changed between attempts: %q vs %q", i, id1, id2)
		}
		if ids[id1] {
			t.Errorf("duplicate ID %q seen", id1)
		}
		ids[id1] = true
	}

	// Deduplicated insertions must be exactly 3.
	if len(intake.inserted) != 3 {
		t.Errorf("deduplicated insertions = %d, want 3", len(intake.inserted))
	}
	// Raw insertions count every event in every body: 6 total.
	if intake.rawInsertions != 6 {
		t.Errorf("raw insertions = %d, want 6", intake.rawInsertions)
	}
}

func TestEventIDKeptWhenAlreadySet(t *testing.T) {
	intake := newLossyIntake(0)
	s := newLossyIntakeStrategy(t, intake, 10)

	evt := eventtypes.EventDataPoint{
		"timestamp":       time.Unix(1_700_000_000, 0),
		"host":            "idempotent-test-host",
		"severity":        "info",
		"message":         "event with fixed id",
		"senhub_event_id": "fixed",
	}
	s.enqueue(evt)

	// Read the event from the buffer.
	select {
	case got := <-s.buffer:
		id, ok := got["senhub_event_id"].(string)
		if !ok || id != "fixed" {
			t.Errorf("ID = %v, want 'fixed'", got["senhub_event_id"])
		}
	default:
		t.Fatal("buffer empty; event was not enqueued")
	}
}

func TestEventIDsDistinctPerEvent(t *testing.T) {
	intake := newLossyIntake(0)
	s := newLossyIntakeStrategy(t, intake, 1000)

	const n = 100
	enqueueEvents(t, s, n)

	// Collect IDs from buffer.
	ids := make(map[string]bool)
	for i := 0; i < n; i++ {
		select {
		case evt := <-s.buffer:
			id, ok := evt["senhub_event_id"].(string)
			if !ok || id == "" {
				t.Fatalf("event %d missing or empty ID", i)
			}
			if len(id) != 32 {
				t.Errorf("event %d ID length = %d, want 32", i, len(id))
			}
			if _, err := hex.DecodeString(id); err != nil {
				t.Errorf("event %d ID is not valid hex: %v", i, err)
			}
			if ids[id] {
				t.Errorf("duplicate ID %q", id)
			}
			ids[id] = true
		default:
			t.Fatalf("buffer drained after %d events, want %d", i, n)
		}
	}
	if len(ids) != n {
		t.Errorf("distinct ID count = %d, want %d", len(ids), n)
	}
}
