package otlp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionWorkerStartIsIdempotent(t *testing.T) {
	var w subscriptionWorker[int]
	var subscribes atomic.Int32
	ch := make(chan int)
	sub := func() <-chan int { subscribes.Add(1); return ch }
	run := func(ctx context.Context, _ <-chan int) { <-ctx.Done() }

	w.start(sub, run)
	w.start(sub, run)
	if got := subscribes.Load(); got != 1 {
		t.Fatalf("subscribe called %d times, want 1", got)
	}
	if !w.stop(context.Background(), func(<-chan int) {}) {
		t.Fatal("stop of a running worker returned false")
	}
}

func TestSubscriptionWorkerStopWithoutStart(t *testing.T) {
	var w subscriptionWorker[int]
	called := false
	if w.stop(context.Background(), func(<-chan int) { called = true }) {
		t.Fatal("stop without start returned true")
	}
	if called {
		t.Fatal("unsubscribe called without start")
	}
}

func TestSubscriptionWorkerStopCancelsUnsubscribesAndWaits(t *testing.T) {
	var w subscriptionWorker[int]
	ch := make(chan int)
	var exited, unsubscribed atomic.Bool
	w.start(
		func() <-chan int { return ch },
		func(ctx context.Context, _ <-chan int) { <-ctx.Done(); exited.Store(true) },
	)
	if !w.stop(context.Background(), func(got <-chan int) {
		if got != (<-chan int)(ch) {
			t.Error("unsubscribe received a different channel")
		}
		unsubscribed.Store(true)
	}) {
		t.Fatal("stop returned false")
	}
	if !unsubscribed.Load() || !exited.Load() {
		t.Fatalf("unsubscribed=%v exited=%v, want both true", unsubscribed.Load(), exited.Load())
	}
	if w.stop(context.Background(), func(<-chan int) {}) {
		t.Fatal("second stop returned true")
	}
}

func TestSubscriptionWorkerStopHonorsCallerDeadline(t *testing.T) {
	var w subscriptionWorker[int]
	release := make(chan struct{})
	defer close(release)
	w.start(
		func() <-chan int { return make(chan int) },
		func(context.Context, <-chan int) { <-release },
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	begin := time.Now()
	w.stop(ctx, func(<-chan int) {})
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("stop blocked %v despite a 50ms deadline", elapsed)
	}
}
