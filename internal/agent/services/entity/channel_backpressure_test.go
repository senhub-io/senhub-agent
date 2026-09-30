package entity

import (
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// The detector publishes a whole cycle in one burst. A 40-device SNMP
// fleet is several hundred events, more than the default 256-slot buffer;
// dropping on a full buffer lost the leading devices on every burst and
// they expired in the consumer (#993). A receiver that keeps draining gets
// every event.
func TestABurstLargerThanTheBufferIsDeliveredWhole(t *testing.T) {
	resetEventChannelForTest()
	defer resetEventChannelForTest()

	const burst = 1000
	ch := SubscribeEvents(16)
	defer UnsubscribeEvents(ch)

	var (
		mu  sync.Mutex
		got int
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
			mu.Lock()
			got++
			n := got
			mu.Unlock()
			time.Sleep(10 * time.Microsecond) // a receiver slower than the publisher
			if n == burst {
				return
			}
		}
	}()
	for i := 0; i < burst; i++ {
		PublishEvent(Event{Kind: EntityState, Entity: &Entity{Type: "network.device", ID: map[string]any{"network.device.id": i}}})
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	mu.Lock()
	defer mu.Unlock()

	if got != burst {
		t.Fatalf("delivered %d of %d", got, burst)
	}
	if n := GetDroppedEntityEventsTotal(); n != 0 {
		t.Fatalf("dropped %d events from a receiver that kept draining", n)
	}
}

// A receiver that stops draining costs one bounded wait, not one per
// event, and every event it misses is counted where an operator sees it:
// senhub.agent.otlp.dropped{reason="entity_queue_full"}.
func TestAStuckReceiverCostsOneWaitAndIsCounted(t *testing.T) {
	resetEventChannelForTest()
	defer resetEventChannelForTest()
	defer func(d time.Duration) { publishWait = d }(publishWait)
	publishWait = 50 * time.Millisecond

	ch := SubscribeEvents(1)
	defer UnsubscribeEvents(ch)
	before := agentstate.GetOTLPDroppedByReason()["entity_queue_full"]

	start := time.Now()
	for i := 0; i < 101; i++ {
		PublishEvent(Event{Kind: EntityState, Entity: &Entity{Type: "host", ID: map[string]any{"host.id": i}}})
	}
	if elapsed := time.Since(start); elapsed > 20*publishWait {
		t.Fatalf("100 events to a stuck receiver took %v; it must wait once, not per event", elapsed)
	}
	if n := GetDroppedEntityEventsTotal(); n != 100 {
		t.Fatalf("dropped %d, want 100 (one fits the buffer)", n)
	}
	if after := agentstate.GetOTLPDroppedByReason()["entity_queue_full"]; after-before != 100 {
		t.Fatalf("entity_queue_full counted %d, want 100", after-before)
	}

	// Once it drains again it gets events again, with no drop.
	<-ch
	PublishEvent(Event{Kind: EntityState, Entity: &Entity{Type: "host", ID: map[string]any{"host.id": "back"}}})
	if ev := <-ch; ev.Entity.ID["host.id"] != "back" {
		t.Fatalf("received %v after draining", ev.Entity.ID)
	}
}
