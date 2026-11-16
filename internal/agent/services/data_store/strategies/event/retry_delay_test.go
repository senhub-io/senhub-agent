package event

import (
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

// laggyIntake stores every event whose id it cannot see yet. An id becomes
// visible lag after its first insertion, as in VictoriaLogs behind the
// intake; the first response is lost after the batch was stored.
type laggyIntake struct {
	mu        sync.Mutex
	lag       time.Duration
	loseFirst int
	calls     int
	stored    int
	firstSeen map[string]time.Time
}

func newLaggyIntake(lag time.Duration, loseFirst int) *laggyIntake {
	return &laggyIntake{lag: lag, loseFirst: loseFirst, firstSeen: map[string]time.Time{}}
}

func (f *laggyIntake) Get(string) (*http.Response, error)       { return okResponse(), nil }
func (f *laggyIntake) Post(string, any) (*http.Response, error) { return okResponse(), nil }

func (f *laggyIntake) PostStream(_ string, body string) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	var events []map[string]any
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, fmt.Errorf("decoding body: %w", err)
	}
	now := time.Now()
	for _, ev := range events {
		id, _ := ev[eventIDField].(string)
		first, seen := f.firstSeen[id]
		if !seen {
			f.firstSeen[id] = now
		}
		if !seen || now.Sub(first) < f.lag {
			f.stored++
		}
	}
	if f.calls <= f.loseFirst {
		return nil, errors.New("connection reset")
	}
	return okResponse(), nil
}

func okResponse() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}
}

func TestRetryDelayForFirstRetryWaits(t *testing.T) {
	s := &EventSyncStrategy{
		firstRetryDelay:  DefaultFirstRetryDelay,
		firstRetryJitter: DefaultFirstRetryJitter,
		retryDelay:       DefaultRetryDelay,
	}
	for i := 0; i < 200; i++ {
		d := s.retryDelayFor(1, nil, nil)
		if d < 5*time.Second || d >= 7*time.Second {
			t.Fatalf("retryDelayFor(1) = %v, want in [5s, 7s)", d)
		}
	}
}

func TestFirstRetryDoesNotRaceIntakeVisibility(t *testing.T) {
	// lag sits above retry-go's own wait (retryDelay plus at most 100 ms of
	// jitter), so only the first-retry delay can clear it: the delayed case
	// fails if retryDelayFor is not wired into retry.Do.
	const lag = 200 * time.Millisecond
	const n = 3

	run := func(t *testing.T, firstDelay time.Duration) int {
		t.Helper()
		intake := newLaggyIntake(lag, 1)
		s := newLaggyStrategy(t, intake)
		s.retryAttempts = DefaultRetryAttempts
		s.retryDelay = 10 * time.Millisecond
		s.firstRetryDelay = firstDelay
		s.firstRetryJitter = 0

		enqueueEvents(t, s, n)
		if err := s.doSync(); err != nil {
			t.Fatalf("doSync: %v", err)
		}
		return intake.stored
	}

	t.Run("delayed first resend stores each event once", func(t *testing.T) {
		if got := run(t, lag+100*time.Millisecond); got != n {
			t.Fatalf("stored = %d, want %d", got, n)
		}
	})

	// Without the delay the resend lands before the ids are visible: this
	// is the duplicate the delay exists to prevent, and proves the fake bites.
	t.Run("immediate first resend duplicates", func(t *testing.T) {
		if got := run(t, 0); got <= n {
			t.Fatalf("stored = %d, want > %d", got, n)
		}
	})
}

func newLaggyStrategy(t *testing.T, intake *laggyIntake) *EventSyncStrategy {
	t.Helper()
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
	return s
}
