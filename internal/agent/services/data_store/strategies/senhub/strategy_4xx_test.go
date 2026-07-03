package senhub

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// newTestStrategy builds a strategy whose server points at the given URL.
func newTestStrategy(t *testing.T, url string) *SyncStrategySenhub {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	return &SyncStrategySenhub{
		buffer: NewBuffer(),
		logger: logger.NewModuleLogger(baseLogger, "strategy.senhub.test"),
		server: server.NewServer("test-key", url, baseLogger),
	}
}

func sampleData() []datapoint.DataPoint {
	return []datapoint.DataPoint{
		{Name: "test.metric", Value: 1, Timestamp: time.Now()},
		{Name: "test.metric", Value: 2, Timestamp: time.Now()},
	}
}

// TestDoSync_PermanentClientErrorDropsBatch asserts a permanent 4xx
// (422/400) drops the batch after a single attempt: the buffer is left
// empty (no AbortSync re-prepend), so the scheduler never retries it.
func TestDoSync_PermanentClientErrorDropsBatch(t *testing.T) {
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(status)
			}))
			defer srv.Close()

			s := newTestStrategy(t, srv.URL)
			if err := s.buffer.Append(sampleData()); err != nil {
				t.Fatalf("Append() error: %v", err)
			}

			if err := s.doSync(); err != nil {
				t.Fatalf("doSync() should not surface a permanent 4xx as error, got: %v", err)
			}

			if got := atomic.LoadInt32(&hits); got != 1 {
				t.Errorf("intake hit %d times, want exactly 1 (no retry loop)", got)
			}

			if remaining := s.buffer.Sync(); len(remaining) != 0 {
				t.Errorf("batch not dropped: buffer still holds %d points after permanent 4xx", len(remaining))
			}
		})
	}
}

// TestDoSync_RetryableStatusKeepsBatch asserts that a retryable status
// (503) leaves the batch in the buffer via AbortSync so the next
// scheduler tick retries it.
func TestDoSync_RetryableStatusKeepsBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := newTestStrategy(t, srv.URL)
	if err := s.buffer.Append(sampleData()); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	if err := s.doSync(); err == nil {
		t.Fatalf("doSync() should surface a retryable status as error, got nil")
	}

	if remaining := s.buffer.Sync(); len(remaining) != 2 {
		t.Errorf("batch not retained for retry: buffer holds %d points, want 2", len(remaining))
	}
}

// TestIsPermanentClientStatus pins the classification: which statuses
// drop the batch and which stay on the retry path.
func TestIsPermanentClientStatus(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},           // 400
		{http.StatusUnauthorized, false},        // 401 retryable (auth blip / key rotation)
		{http.StatusForbidden, false},           // 403 retryable (auth blip / key rotation)
		{http.StatusNotFound, true},             // 404
		{http.StatusUnprocessableEntity, true},  // 422
		{http.StatusRequestTimeout, false},      // 408 retryable
		{http.StatusTooManyRequests, false},     // 429 retryable
		{http.StatusInternalServerError, false}, // 500 retryable
		{http.StatusServiceUnavailable, false},  // 503 retryable
		{http.StatusOK, false},                  // 200 not an error
	}
	for _, c := range cases {
		if got := isPermanentClientStatus(c.status); got != c.permanent {
			t.Errorf("isPermanentClientStatus(%d) = %v, want %v", c.status, got, c.permanent)
		}
	}
}
