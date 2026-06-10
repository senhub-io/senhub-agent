package entity

import (
	"sync"
	"sync/atomic"
)

// eventChannelState is the agent's process-lifetime entity-event fan-out:
// detectors publish Events, sinks (today the OTLP strategy's entity pump)
// subscribe. Mirrors the agentstate log channel — one package-level value,
// non-blocking publish with drop-oldest under backpressure.
type eventChannelState struct {
	mu      sync.RWMutex
	subs    []chan Event
	dropped atomic.Uint64
}

var eventCh = &eventChannelState{}

// SubscribeEvents returns a channel that receives Events published via
// PublishEvent. buf sets the receive buffer; if the consumer falls behind
// and the buffer fills, events are dropped (oldest-first) and the global
// drop counter is bumped. Callers must drain the channel and call
// UnsubscribeEvents when done.
func SubscribeEvents(buf int) <-chan Event {
	if buf <= 0 {
		buf = 256
	}
	ch := make(chan Event, buf)
	eventCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]chan Event, len(eventCh.subs), len(eventCh.subs)+1)
	copy(next, eventCh.subs)
	eventCh.subs = append(next, ch)
	eventCh.mu.Unlock()
	return ch
}

// UnsubscribeEvents disconnects a previously-subscribed channel. The
// channel is NOT closed: PublishEvent snapshots the subscriber list
// under RLock and sends after releasing it, so a close here could
// interleave into a send-on-closed-channel panic (#262). Consumers
// exit via their own context (both pumps cancel before
// unsubscribing); the orphaned channel is garbage-collected.
func UnsubscribeEvents(ch <-chan Event) {
	eventCh.mu.Lock()
	defer eventCh.mu.Unlock()
	for i, sub := range eventCh.subs {
		if (<-chan Event)(sub) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]chan Event, 0, len(eventCh.subs)-1)
			next = append(next, eventCh.subs[:i]...)
			next = append(next, eventCh.subs[i+1:]...)
			eventCh.subs = next
			return
		}
	}
}

// PublishEvent fans an Event out to every subscriber. Non-blocking: a full
// subscriber buffer gets one stale event dropped before retrying, then the
// new event is dropped for that subscriber only if still full. Producers
// never block — emission is best-effort under backpressure, and the
// at-least-once/idempotent contract means a dropped heartbeat is recovered
// by the next one.
func PublishEvent(ev Event) {
	eventCh.mu.RLock()
	subs := eventCh.subs
	eventCh.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
				eventCh.dropped.Add(1)
			default:
			}
			select {
			case ch <- ev:
			default:
				eventCh.dropped.Add(1)
			}
		}
	}
}

// GetDroppedEntityEventsTotal returns the lifetime count of entity events
// dropped due to subscriber backpressure.
func GetDroppedEntityEventsTotal() uint64 {
	return eventCh.dropped.Load()
}

// resetEventChannelForTest clears subscribers and the drop counter.
// Test-only, keeps package state from leaking across cases.
func resetEventChannelForTest() {
	eventCh.mu.Lock()
	eventCh.subs = nil
	eventCh.dropped.Store(0)
	eventCh.mu.Unlock()
}
