package entity

import (
	"context"
	"testing"
	"time"
)

// The OTLP output subscribes when it starts, which can be just after the
// detector's first cycle: an agent started with both, or an output enabled
// from the console. Waiting for the next re-emission left the host entity
// unknown downstream for up to two cycles (ten minutes by default).
func TestALateSubscriberReceivesTheCurrentStateAtOnce(t *testing.T) {
	resetEventChannelForTest()
	t.Cleanup(resetEventChannelForTest)

	d := NewDetector(
		func() (HostIdentity, error) { return HostIdentity{ID: "h-1", Name: "web", OSType: "linux"}, nil },
		func() AgentIdentity { return AgentIdentity{InstanceID: "agent-1"} },
		time.Hour,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A subscriber present from the start sees the first cycle.
	early := SubscribeEvents(64)
	go d.Run(ctx)
	waitForHost(t, early, "the first cycle")

	late := SubscribeEvents(64)
	waitForHost(t, late, "a subscriber arriving after the first cycle")
}

func waitForHost(t *testing.T, ch <-chan Event, who string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == EntityState && ev.Entity != nil && ev.Entity.Type == "host" {
				return
			}
		case <-deadline:
			t.Fatalf("%s: no host entity received", who)
		}
	}
}
