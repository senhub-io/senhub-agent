package periodic_scheduler

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// A collection that ignores its context and outlives the interval is not
// abandoned and not run twice: it is counted once as an overrun, no second
// call starts while it runs, and the next call follows its return.
func TestCallPastTheIntervalIsAnOverrunAndNeverRunsTwice(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var calls, running, maxRunning atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       20 * time.Millisecond,
		CallTimeout:    40 * time.Millisecond,
		ExecuteContext: func(ctx context.Context) error {
			n := running.Add(1)
			defer running.Add(-1)
			if n > maxRunning.Load() {
				maxRunning.Store(n)
			}
			if calls.Add(1) == 1 {
				<-release
			}
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		unblock()
		_ = s.Shutdown(context.Background())
	}()

	time.Sleep(250 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls while the first is in flight = %d, want 1", got)
	}
	if _, _, overruns := obs.snapshot(); overruns != 1 {
		t.Fatalf("overruns = %d, want 1", overruns)
	}

	unblock()
	waitForAtomicCalls(t, &calls, 2)
	if got := maxRunning.Load(); got != 1 {
		t.Errorf("concurrent calls = %d, want 1", got)
	}
	if _, _, overruns := obs.snapshot(); overruns != 1 {
		t.Errorf("overruns after the next call = %d, want still 1", overruns)
	}
}

// A collection that honours its context returns at the deadline and is not
// an overrun.
func TestCallHonouringItsContextReturnsAtTheDeadline(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	returned := make(chan time.Duration, 1)

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       time.Hour,
		CallTimeout:    60 * time.Millisecond,
		ExecuteContext: func(ctx context.Context) error {
			began := time.Now()
			<-ctx.Done()
			select {
			case returned <- time.Since(began):
			default:
			}
			return ctx.Err()
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	select {
	case took := <-returned:
		if took < 50*time.Millisecond || took > 500*time.Millisecond {
			t.Errorf("call ran %s, want about the 60ms deadline", took)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call never reached its deadline")
	}
	time.Sleep(50 * time.Millisecond)
	if _, _, overruns := obs.snapshot(); overruns != 0 {
		t.Errorf("overruns = %d, want 0 for a call that honours its context", overruns)
	}
}

// A call longer than the interval but inside its deadline is counted as an
// overrun without being cut, and is never run twice.
func TestCallPastTheIntervalButInsideItsDeadlineIsCountedNotCut(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32
	var cut atomic.Bool

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       30 * time.Millisecond,
		CallTimeout:    5 * time.Second,
		ExecuteContext: func(ctx context.Context) error {
			if calls.Add(1) > 1 {
				return nil
			}
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				cut.Store(true)
			}
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	waitForAtomicCalls(t, &calls, 2)
	if cut.Load() {
		t.Error("the call was cancelled before its deadline")
	}
	if _, _, overruns := obs.snapshot(); overruns != 1 {
		t.Errorf("overruns = %d, want 1", overruns)
	}
}

// A call that is not cancellable (Execute) and outlives the interval is
// counted too, with no deadline set.
func TestPlainCallPastTheIntervalIsAnOverrun(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       30 * time.Millisecond,
		Execute: func() error {
			if calls.Add(1) == 1 {
				time.Sleep(150 * time.Millisecond)
			}
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	waitForAtomicCalls(t, &calls, 2)
	if _, _, overruns := obs.snapshot(); overruns != 1 {
		t.Errorf("overruns = %d, want 1", overruns)
	}
}
