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
	eventCh.subs = append(eventCh.subs, ch)
	eventCh.mu.Unlock()
	return ch
}

// UnsubscribeEvents disconnects and closes a previously-subscribed channel.
// Safe to call from a different goroutine than the consumer; the close
// unblocks any select the consumer sits in.
func UnsubscribeEvents(ch <-chan Event) {
	eventCh.mu.Lock()
	defer eventCh.mu.Unlock()
	for i, sub := range eventCh.subs {
		if (<-chan Event)(sub) == ch {
			eventCh.subs = append(eventCh.subs[:i], eventCh.subs[i+1:]...)
			close(sub)
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
	for _, sub := range eventCh.subs {
		close(sub)
	}
	eventCh.subs = nil
	eventCh.dropped.Store(0)
	eventCh.mu.Unlock()
}
