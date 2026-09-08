package agentstate

import (
	"sync"
	"time"
)

// Recent events live here so the console can show the last things that
// happened to the agent without reading its journal: a probe that
// started failing, an output that could not start, a save from the
// console, a configuration warning. The buffer is small and in memory;
// it is a glance, not a log.

// Event is one transition worth telling the operator about.
type Event struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`   // "info", "warn" or "error"
	Kind    string    `json:"kind"`    // "probe", "output", "config" or "console"
	Subject string    `json:"subject"` // the probe or output name
	Message string    `json:"message"`
}

const (
	EventInfo  = "info"
	EventWarn  = "warn"
	EventError = "error"

	EventKindProbe   = "probe"
	EventKindOutput  = "output"
	EventKindConfig  = "config"
	EventKindConsole = "console"
)

const eventBufferSize = 50

var events = struct {
	mu   sync.Mutex
	ring []Event
	next int
	full bool
}{ring: make([]Event, eventBufferSize)}

// RecordEvent appends one event, evicting the oldest past the buffer
// size. A caller reporting the same state twice should not call this:
// the buffer is for transitions.
func RecordEvent(level, kind, subject, message string) {
	if level == "" {
		level = EventInfo
	}
	events.mu.Lock()
	events.ring[events.next] = Event{Time: time.Now(), Level: level, Kind: kind, Subject: subject, Message: message}
	events.next = (events.next + 1) % len(events.ring)
	if events.next == 0 {
		events.full = true
	}
	events.mu.Unlock()
}

// GetEvents returns the buffered events, newest first.
func GetEvents() []Event {
	events.mu.Lock()
	defer events.mu.Unlock()
	n := events.next
	if events.full {
		n = len(events.ring)
	}
	out := make([]Event, 0, n)
	for i := 1; i <= n; i++ {
		idx := (events.next - i + len(events.ring)) % len(events.ring)
		out = append(out, events.ring[idx])
	}
	return out
}

// ResetEventsForTest clears the buffer.
func ResetEventsForTest() {
	events.mu.Lock()
	events.ring = make([]Event, eventBufferSize)
	events.next = 0
	events.full = false
	events.mu.Unlock()
}
