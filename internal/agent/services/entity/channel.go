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
	// joined are signalled, without blocking, each time a subscriber
	// arrives, so a running detector can send it the current state.
	joined []chan struct{}
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
	joined := eventCh.joined
	eventCh.mu.Unlock()
	for _, j := range joined {
		select {
		case j <- struct{}{}:
		default:
		}
	}
	return ch
}

// NotifyOnSubscribe returns a channel signalled each time a subscriber
// arrives, and a function that stops the notifications. A subscriber that
// arrives after the detector's first cycle would otherwise wait for the
// next re-emission, up to two cycles, before it saw an unchanged entity.
func NotifyOnSubscribe() (<-chan struct{}, func()) {
	j := make(chan struct{}, 1)
	eventCh.mu.Lock()
	eventCh.joined = append(append([]chan struct{}{}, eventCh.joined...), j)
	eventCh.mu.Unlock()
	return j, func() {
		eventCh.mu.Lock()
		defer eventCh.mu.Unlock()
		next := make([]chan struct{}, 0, len(eventCh.joined))
		for _, x := range eventCh.joined {
			if x != j {
				next = append(next, x)
			}
		}
		eventCh.joined = next
	}
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
	eventCh.joined = nil
	eventCh.dropped.Store(0)
	eventCh.mu.Unlock()
}
