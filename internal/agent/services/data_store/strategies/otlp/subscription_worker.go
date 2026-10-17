package otlp

import (
	"context"
	"sync"
	"time"
)

const subscriptionStopTimeout = 5 * time.Second

// subscriptionWorker owns the lifecycle shared by every goroutine that
// drains an agentstate/entity subscription: subscribe once, run the loop
// until cancelled, then unsubscribe and wait (bounded) for the loop to exit.
type subscriptionWorker[T any] struct {
	mu         sync.Mutex
	subscribed <-chan T
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// start subscribes and launches run on its own goroutine. Idempotent: a
// second call while running does not re-subscribe.
func (w *subscriptionWorker[T]) start(subscribe func() <-chan T, run func(ctx context.Context, ch <-chan T)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.subscribed != nil {
		return
	}
	ch := subscribe()
	w.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		run(ctx, ch)
	}()
}

// stop cancels the loop, unsubscribes, and waits for the loop to exit,
// returning early when ctx is done or after subscriptionStopTimeout (the
// goroutine then exits on its own once cancelled). It reports whether a
// running worker was stopped; false means start was never called or stop
// already ran.
func (w *subscriptionWorker[T]) stop(ctx context.Context, unsubscribe func(<-chan T)) bool {
	w.mu.Lock()
	ch := w.subscribed
	cancel := w.cancel
	w.subscribed = nil
	w.cancel = nil
	w.mu.Unlock()

	if cancel == nil {
		return false
	}

	cancel()
	if ch != nil {
		unsubscribe(ch)
	}

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(subscriptionStopTimeout):
	}
	return true
}
