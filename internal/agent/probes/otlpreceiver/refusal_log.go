package otlpreceiver

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// refusalLogKey identifies one refusal series for log rate-limiting.
type refusalLogKey struct {
	signal string
	reason string
	// remote is set for authentication refusals only, so one client's
	// failures cannot hide another's behind the shared rate limit.
	remote string
}

// maxRefusalLogKeys bounds the per-client keys: past it, authentication
// refusals share one key per signal and reason again.
const maxRefusalLogKeys = 1024

func isAuthRefusal(reason string) bool { return reason == "unauthorized" || reason == "forbidden" }

// refusalLogEntry is the rate-limit state of one (signal, reason) series:
// when the last line was emitted and how many lines were suppressed since.
type refusalLogEntry struct {
	lastLogged time.Time
	suppressed int
}

// refusalLogWindow is the minimum spacing between two log lines of the
// same (signal, reason).
const refusalLogWindow = time.Minute

// refusalLogState rate-limits refusal log lines: at most one line per
// (signal, reason) per window; lines suppressed in between are counted
// and reported as `suppressed` on the next emitted line. The clock is a
// field so tests can inject a fake one.
type refusalLogState struct {
	mu      sync.Mutex
	entries map[refusalLogKey]refusalLogEntry
	now     func() time.Time
}

// newRefusalLogState is one receiver's refusal log, on the given clock.
func newRefusalLogState(now func() time.Time) *refusalLogState {
	return &refusalLogState{entries: make(map[refusalLogKey]refusalLogEntry), now: now}
}

// log emits one Warn line for a refusal unless the same (signal, reason)
// was already logged within the window, in which case it only increments
// the suppressed counter. It never logs bodies, header values or query
// strings — only the client IP.
// warner is what the refusal log needs from a logger: the module logger in
// the server, a plain zerolog logger in tests.
type warner interface{ Warn() *zerolog.Event }

func (s *refusalLogState) log(log warner, signal string, status int, reason, remote string) {
	key := refusalLogKey{signal: signal, reason: reason}

	s.mu.Lock()
	if isAuthRefusal(reason) {
		perClient := refusalLogKey{signal: signal, reason: reason, remote: remote}
		if _, known := s.entries[perClient]; known || len(s.entries) < maxRefusalLogKeys {
			key = perClient
		}
	}
	entry := s.entries[key]
	now := s.now()
	if now.Sub(entry.lastLogged) < refusalLogWindow {
		entry.suppressed++
		s.entries[key] = entry
		s.mu.Unlock()
		return
	}
	suppressed := entry.suppressed
	entry.lastLogged = now
	entry.suppressed = 0
	s.entries[key] = entry
	s.mu.Unlock()

	ev := log.Warn().
		Str("signal", signal).
		Int("status", status).
		Str("reason", reason).
		Str("remote", remote)
	if suppressed > 0 {
		ev = ev.Int("suppressed", suppressed)
	}
	ev.Msg("OTLP/HTTP request refused")
}
