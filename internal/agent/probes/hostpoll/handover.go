package hostpoll

import (
	"sync"
	"time"
)

const (
	// PrimeWindow is the shortest span a first rate covers. A shorter one
	// rounds a mode that uses little time to zero: the kernel counts CPU
	// time in ticks of 10 ms, so one second on an idle two-CPU host holds
	// about 200 ticks and a mode at 0.1 % gets none.
	PrimeWindow = 5 * time.Second
	// maxHandoverAge is the oldest reading a new collector diffs against;
	// past it, a first rate would average over a span far longer than the
	// probe's cycle.
	maxHandoverAge = 15 * time.Minute
)

// Handover keeps the last two readings of host-wide cumulative counters.
// A probe restarts with a new collector whenever its configuration
// changes, a remotely set interval included; the new collector diffs its
// first reading against one its predecessor took, instead of priming over
// a short window. The counters belong to the host, not to the probe, so
// any earlier reading of the same host is a valid base.
type Handover[T any] struct {
	mu       sync.Mutex
	readings [2]handoverReading[T]
}

type handoverReading[T any] struct {
	value T
	at    time.Time
	set   bool
}

// Store records a reading taken at at.
func (h *Handover[T]) Store(value T, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readings[1] = h.readings[0]
	h.readings[0] = handoverReading[T]{value: value, at: at, set: true}
}

// Base returns the reading a first rate taken at now should diff against:
// the newest one at least PrimeWindow old, else the newest one, provided
// it is no older than maxHandoverAge and not in the future. ok is false
// when there is none; the caller then primes from its own reading.
func (h *Handover[T]) Base(now time.Time) (value T, at time.Time, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	usable := func(r handoverReading[T]) bool {
		age := now.Sub(r.at)
		return r.set && age >= 0 && age <= maxHandoverAge
	}
	for _, r := range h.readings {
		if usable(r) && now.Sub(r.at) >= PrimeWindow {
			return r.value, r.at, true
		}
	}
	if r := h.readings[0]; usable(r) {
		return r.value, r.at, true
	}
	return value, at, false
}
