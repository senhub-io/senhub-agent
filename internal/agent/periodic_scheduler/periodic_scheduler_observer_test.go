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

type fakeObserver struct {
	mu      sync.Mutex
	started []time.Duration
	ended   []bool
	skipped []int
}

func (f *fakeObserver) CallStarted(lag time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, lag)
}

func (f *fakeObserver) CallEnded(first bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, first)
}

func (f *fakeObserver) TicksSkipped(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skipped = append(f.skipped, n)
}

func (f *fakeObserver) snapshot() ([]time.Duration, []bool, []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	started := append([]time.Duration(nil), f.started...)
	ended := append([]bool(nil), f.ended...)
	skipped := append([]int(nil), f.skipped...)
	return started, ended, skipped
}

func waitForAtomicCalls(t *testing.T, calls *atomic.Int32, n int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < n && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if calls.Load() < n {
		t.Fatalf("calls = %d, want >= %d", calls.Load(), n)
	}
}

func waitForObserverStarted(t *testing.T, obs *fakeObserver, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		started, _, _ := obs.snapshot()
		if len(started) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observer CallStarted count = %d, want >= %d", len(started), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitForObserverEnded(t *testing.T, obs *fakeObserver, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, ended, _ := obs.snapshot()
		if len(ended) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observer CallEnded count = %d, want >= %d", len(ended), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestPeriodicSchedulerObserverInitialCallAndTicks(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       20 * time.Millisecond,
		Execute: func() error {
			calls.Add(1)
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	waitForAtomicCalls(t, &calls, 3)
	waitForObserverStarted(t, obs, 2)
	waitForObserverEnded(t, obs, 2)

	started, ended, _ := obs.snapshot()
	if len(started) < 2 {
		t.Fatalf("CallStarted count = %d, want >= 2", len(started))
	}
	if len(ended) != len(started) {
		t.Fatalf("CallStarted count = %d, CallEnded count = %d", len(started), len(ended))
	}
	if !ended[0] {
		t.Errorf("first CallEnded = false, want true for initial call")
	}
	trueCount := 0
	for _, first := range ended {
		if first {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Errorf("initial CallEnded(true) count = %d, want 1", trueCount)
	}
	if len(ended) >= 2 && ended[1] {
		t.Errorf("second CallEnded = true, want false for tick call")
	}
}

func TestPeriodicSchedulerObserverLag(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		InitialDelay:   40 * time.Millisecond,
		Interval:       40 * time.Millisecond,
		Execute: func() error {
			n := calls.Add(1) - 1
			if n == 1 {
				time.Sleep(120 * time.Millisecond)
			}
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	waitForAtomicCalls(t, &calls, 3)
	waitForObserverStarted(t, obs, 3)

	started, _, _ := obs.snapshot()
	if len(started) < 3 {
		t.Fatalf("CallStarted count = %d, want >= 3", len(started))
	}
	if started[0] > 30*time.Millisecond {
		t.Errorf("initial call lag = %s; InitialDelay must not be counted as lag", started[0])
	}
	if started[2] < 40*time.Millisecond {
		t.Errorf("tick after overrun lag = %s, want >= 40ms", started[2])
	}
}

func TestPeriodicSchedulerObserverTicksSkipped(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       30 * time.Millisecond,
		Execute: func() error {
			n := calls.Add(1) - 1
			if n == 1 {
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

	waitForAtomicCalls(t, &calls, 4)

	_, _, skipped := obs.snapshot()
	total := 0
	for _, n := range skipped {
		total += n
	}
	if total < 2 {
		t.Errorf("TicksSkipped total = %d, want >= 2", total)
	}
}

func TestPeriodicSchedulerObserverNoSkipsWhenFast(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       40 * time.Millisecond,
		Execute: func() error {
			calls.Add(1)
			return nil
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	time.Sleep(250 * time.Millisecond)
	_ = s.Shutdown(context.Background())

	_, _, skipped := obs.snapshot()
	if len(skipped) != 0 {
		t.Errorf("TicksSkipped = %v, want none", skipped)
	}
}

func TestPeriodicSchedulerObserverNilObserver(t *testing.T) {
	logger := zerolog.New(io.Discard)
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       20 * time.Millisecond,
		Execute: func() error {
			calls.Add(1)
			return nil
		},
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	waitForAtomicCalls(t, &calls, 3)
}

func TestPeriodicSchedulerObserverPanics(t *testing.T) {
	logger := zerolog.New(io.Discard)
	obs := &fakeObserver{}
	var calls atomic.Int32

	s := NewPeriodicScheduler(PeriodicSchedulerConfig{
		ExecuteOnStart: true,
		Interval:       20 * time.Millisecond,
		Execute: func() error {
			calls.Add(1)
			panic("boom")
		},
		Observer: obs,
	}, &logger)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		started, ended, _ := obs.snapshot()
		if len(started) > 0 && len(ended) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("observer not paired: CallStarted=%d CallEnded=%d", len(started), len(ended))
		}
		time.Sleep(2 * time.Millisecond)
	}

	started, ended, _ := obs.snapshot()
	if len(started) != len(ended) {
		t.Errorf("CallStarted count = %d, CallEnded count = %d", len(started), len(ended))
	}
	if calls.Load() == 0 {
		t.Error("Execute was not called")
	}
	_ = s.Shutdown(context.Background())
}
