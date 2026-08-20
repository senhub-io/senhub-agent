// Package pushqueue is the bounded backlog every push sink keeps
// between a collection cycle and a successful delivery.
//
// There is exactly one behaviour worth sharing here, and it is the one
// an outage exposes: a sink that cannot deliver must stop growing.
// Before the cap, an intake outage grew the backlog until the process
// was killed — every failed send re-queued the whole batch while
// collection kept appending (#267). When the cap is reached the OLDEST
// items go first: in a monitoring stream the freshest data is the part
// still worth shipping, and a backlog that cannot be delivered anyway is
// not worth an OOM.
//
// The queue is generic because the three push sinks carry different
// payloads — datapoints for the cloud and PRTG sinks, formatted events
// for the event sink — but keep the same backlog semantics. Before this
// package the two metric sinks each carried a verbatim copy of it and
// the event sink carried none at all, so its retry backlog was the one
// place the OOM class was still live (#287).
package pushqueue

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// DefaultMaxItems bounds a push backlog. 100k datapoints is hours of a
// typical agent's volume — long enough to ride out a real outage, small
// enough that the agent is not the thing that falls over.
const DefaultMaxItems = 100000

// Bounded is a FIFO backlog capped at MaxItems, safe for concurrent use.
// The zero value is not usable; build one with New.
type Bounded[T any] struct {
	// strategy names the sink in the drop counter, so an operator can
	// tell which sink is shedding.
	strategy string
	maxItems int

	mu    sync.Mutex
	items []T
}

// New returns a backlog for the named strategy, capped at maxItems.
// A maxItems of zero or less means unbounded — only appropriate for
// tests, since an unbounded backlog is the defect this package exists
// to prevent.
func New[T any](strategy string, maxItems int) *Bounded[T] {
	return &Bounded[T]{strategy: strategy, maxItems: maxItems}
}

// NewDefault returns a backlog for the named strategy at DefaultMaxItems.
func NewDefault[T any](strategy string) *Bounded[T] {
	return New[T](strategy, DefaultMaxItems)
}

// Append adds newly produced items to the tail.
func (b *Bounded[T]) Append(newItems []T) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, newItems...)
	b.trimToCap()
	return nil
}

// Sync takes the whole backlog and empties the queue. The caller now
// owns the batch: deliver it, or hand it back with AbortSync.
func (b *Bounded[T]) Sync() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.items
	b.items = nil
	return items
}

// AbortSync returns an undelivered batch to the HEAD of the queue, so
// the order in which items were produced survives the retry. The cap
// then trims from the oldest end, which is the same end the batch was
// just put back on — an outage long enough to fill the queue sheds the
// oldest backlog, not the newest collection.
func (b *Bounded[T]) AbortSync(failed []T) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(failed, b.items...)
	b.trimToCap()
	return nil
}

// Len reports the current backlog depth. Used by the sinks to expose
// how far behind they are, and by tests to assert the cap holds.
func (b *Bounded[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// trimToCap drops the oldest items past the cap and records the loss.
// Callers hold the mutex.
func (b *Bounded[T]) trimToCap() {
	if b.maxItems <= 0 || len(b.items) <= b.maxItems {
		return
	}
	dropped := len(b.items) - b.maxItems
	// Re-slice rather than copy. The dropped items stay reachable
	// through the backing array until the next append reallocates, but
	// that window is bounded and self-clearing: each trim shrinks the
	// slice's remaining capacity, so sustained pressure forces a
	// reallocation regularly. Copying instead would mean allocating and
	// moving the whole capped backlog on every append once the cap is
	// reached — paying a large, certain cost to avoid a small, temporary
	// one.
	b.items = b.items[dropped:]
	agentstate.IncrementPushBufferDropped(b.strategy, dropped)
}
