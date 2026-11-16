package event

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/pushqueue"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

// dedupIntake is a fake intake that reports 202 Accepted with a JSON body
// indicating 1 inserted and 2 duplicates, simulating a deduplicating server.
type dedupIntake struct {
	attempts atomic.Int32
}

func (d *dedupIntake) Get(string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (d *dedupIntake) Post(string, any) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (d *dedupIntake) PostStream(string, string) (*http.Response, error) {
	d.attempts.Add(1)
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Body:       io.NopCloser(strings.NewReader(`{"status":"accepted","count":1,"duplicates":2}`)),
	}, nil
}

// TestIntakeDuplicatesReplyIsSuccess verifies that a 202 reply with
// count<sent is treated as success: no error, no requeue, no second send.
func TestIntakeDuplicatesReplyIsSuccess(t *testing.T) {
	intake := &dedupIntake{}

	s, err := NewEventSyncStrategy(stubAgentConfig{}, configuration.StorageConfigParams{
		"server_url": "http://intake.invalid",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.server = intake
	s.failedEvents = pushqueue.New[eventtypes.EventDataPoint]("event", 50)
	s.syncTriggerSize = 1 << 30
	s.syncTriggerBytes = 1 << 30
	s.retryAttempts = 3
	s.retryDelay = 0

	enqueueEvents(t, s, 3)

	if err := s.doSync(); err != nil {
		t.Fatalf("first doSync returned error: %v", err)
	}
	if s.failedEvents.Len() != 0 {
		t.Fatalf("failedEvents Len = %d, want 0", s.failedEvents.Len())
	}
	if got := intake.attempts.Load(); got != 1 {
		t.Fatalf("PostStream called %d times, want 1", got)
	}

	if err := s.doSync(); err != nil {
		t.Fatalf("second doSync returned error: %v", err)
	}
	if got := intake.attempts.Load(); got != 1 {
		t.Fatalf("PostStream called %d times after second doSync, want 1", got)
	}
}
