package entity

import (
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// eventChannelState is the agent's process-lifetime entity-event fan-out:
// detectors publish Events, sinks (today the OTLP strategy's entity pump)
// subscribe. One package-level value; a publish waits, within a bound, for
// room in a subscriber's buffer (see PublishEvent).
type eventChannelState struct {
	mu      sync.RWMutex
	subs    []*subscriber
	dropped atomic.Uint64
	// joined are signalled, without blocking, each time a subscriber
	// arrives, so a running detector can send it the current state.
	joined []chan struct{}
}

var eventCh = &eventChannelState{}

// subscriber is one receiver. stalled marks a receiver that let a publish
// wait out publishWait: later events skip the wait until it takes one
// again, so a stuck receiver costs one wait, not one per event.
type subscriber struct {
	ch      chan Event
	stalled atomic.Bool
}

// publishWait bounds how long a publish waits for room in a subscriber's
// buffer. The detector publishes a whole cycle at once, far more events than
// the default buffer holds on a large fleet; the receiver drains in
// microseconds, so waiting delivers them where dropping lost them.
var publishWait = time.Second

// SubscribeEvents returns a channel that receives Events published via
// PublishEvent. buf sets the receive buffer; a publish that finds it full
// waits for room (see PublishEvent). Callers must drain the channel and
// call UnsubscribeEvents when done.
func SubscribeEvents(buf int) <-chan Event {
	if buf <= 0 {
		buf = 256
	}
	ch := make(chan Event, buf)
	eventCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]*subscriber, len(eventCh.subs), len(eventCh.subs)+1)
	copy(next, eventCh.subs)
	eventCh.subs = append(next, &subscriber{ch: ch})
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
		if (<-chan Event)(sub.ch) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]*subscriber, 0, len(eventCh.subs)-1)
			next = append(next, eventCh.subs[:i]...)
			next = append(next, eventCh.subs[i+1:]...)
			eventCh.subs = next
			return
		}
	}
}

// PublishEvent fans an Event out to every subscriber. When a subscriber's
// buffer is full it waits up to publishWait for room, then drops the event
// for that subscriber, counts it, and stops waiting on that subscriber until
// it accepts one again.
//
// It used to drop at once, oldest first. The detector publishes a whole
// cycle in one burst and marks each event published, so a dropped heartbeat
// was not retried before the next refresh window, and the burst dropped the
// same leading entities every time: on a 40-device SNMP fleet the first
// devices went unannounced for five to ten cycles in a row and expired in
// the consumer, cascading their interfaces (#993). The drop was counted but
// the count was exported nowhere.
func PublishEvent(ev Event) {
	eventCh.mu.RLock()
	subs := eventCh.subs
	eventCh.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub.ch <- ev:
			sub.stalled.Store(false)
			continue
		default:
		}
		if !sub.stalled.Load() {
			t := time.NewTimer(publishWait)
			select {
			case sub.ch <- ev:
				t.Stop()
				continue
			case <-t.C:
				sub.stalled.Store(true)
			}
		}
		eventCh.dropped.Add(1)
		agentstate.IncrementOTLPDropped("entity_queue_full")
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
