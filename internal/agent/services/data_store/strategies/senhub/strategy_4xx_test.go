package senhub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/exporterrors"
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

// TestDoSync_ConfigurationErrorDropsBatch pins the class the taxonomy
// was introduced for: an endpoint the URL parser rejects fails
// identically on every tick, so re-prepending the batch would pin it at
// the head of the buffer and stop the buffer draining for good. Only an
// operator editing the config clears it — drop the batch.
func TestDoSync_ConfigurationErrorDropsBatch(t *testing.T) {
	s := newTestStrategy(t, "://not-a-url")
	if err := s.buffer.Append(sampleData()); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	if err := s.doSync(); err != nil {
		t.Fatalf("doSync() should not surface an unrecoverable config error, got: %v", err)
	}

	if remaining := s.buffer.Sync(); len(remaining) != 0 {
		t.Errorf("batch not dropped: buffer still holds %d points after a configuration failure", len(remaining))
	}
}

// TestDoSync_TransportErrorKeepsBatch guards the other side of the same
// branch: an unreachable intake is the ordinary outage case and must
// keep the data for the next tick.
func TestDoSync_TransportErrorKeepsBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := srv.URL
	srv.Close() // nothing listens on that port any more

	s := newTestStrategy(t, closedURL)
	if err := s.buffer.Append(sampleData()); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	if err := s.doSync(); err == nil {
		t.Fatal("doSync() should surface an unreachable intake as error, got nil")
	}

	if remaining := s.buffer.Sync(); len(remaining) != 2 {
		t.Errorf("batch not retained for retry: buffer holds %d points, want 2", len(remaining))
	}
}

// TestPermanentClientErrorClassifiesAsValidation keeps the status-code
// detail reachable while the batch-level decision goes through the
// shared taxonomy.
func TestPermanentClientErrorClassifiesAsValidation(t *testing.T) {
	var err error = &permanentClientError{statusCode: http.StatusUnprocessableEntity}

	if !errors.Is(err, exporterrors.ErrValidation) {
		t.Error("permanentClientError should classify as ErrValidation")
	}
	if exporterrors.IsRetryable(err) {
		t.Error("permanentClientError must not be retryable")
	}
	var permErr *permanentClientError
	if !errors.As(err, &permErr) || permErr.statusCode != http.StatusUnprocessableEntity {
		t.Errorf("status code must stay reachable through errors.As, got %+v", permErr)
	}
}

// TestRejectionReasonReachesTheOperator is the defect #832 named: the
// intake's explanation of why it refused a batch was formatted with %v
// against an io.ReadCloser, so the log carried a pointer and never the
// reason — on exactly the failure where the reason is the only thing
// that matters.
func TestRejectionReasonReachesTheOperator(t *testing.T) {
	const reason = `{"error":"metric name too long","field":"name"}`

	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(reason))
			}))
			defer srv.Close()

			s := newTestStrategy(t, srv.URL)
			err := s.doSyncData([]SenhubDataPoint{{Name: "x", Value: 1}})
			if err == nil {
				t.Fatalf("status %d produced no error", status)
			}
			if !strings.Contains(err.Error(), "metric name too long") {
				t.Errorf("the intake's reason is missing from %q", err.Error())
			}
			if strings.Contains(err.Error(), "0xc0") || strings.Contains(err.Error(), "&{") {
				t.Errorf("the error carries a pointer rendering instead of text: %q", err.Error())
			}
		})
	}
}

// TestRejectionReasonIsBoundedAndSingleLine keeps a misbehaving endpoint
// from streaming into a log record, and keeps a multi-line body from
// breaking the structured line it is embedded in.
func TestRejectionReasonIsBoundedAndSingleLine(t *testing.T) {
	body := "first line\nsecond line\n" + strings.Repeat("A", 100_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	s := newTestStrategy(t, srv.URL)
	err := s.doSyncData([]SenhubDataPoint{{Name: "x", Value: 1}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("the reason spans multiple lines: %q", err.Error())
	}
	if len(err.Error()) > maxRejectionBodyBytes+200 {
		t.Errorf("the reason is unbounded: %d bytes", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "first line second line") {
		t.Errorf("the leading text was lost: %q", err.Error()[:80])
	}
}
