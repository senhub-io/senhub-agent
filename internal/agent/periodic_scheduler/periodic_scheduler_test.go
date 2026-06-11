package periodic_scheduler

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNewPeriodicScheduler(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name   string
		config PeriodicSchedulerConfig
	}{
		{
			name:   "Valid PeriodicScheduler",
			config: PeriodicSchedulerConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = NewPeriodicScheduler(tt.config, &logger)
		})
	}
}

func TestPeriodicScheduler_Start(t *testing.T) {
	logger := zerolog.New(os.Stderr)

	t.Run("Start", func(t *testing.T) {
		quitChannel := make(chan struct{})
		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{}, &logger)
		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}
	})

	t.Run("Start should call OnStart", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := false

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			OnStart: func(quitChannel chan struct{}) error {
				called = true
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}
		if !called {
			t.Errorf("PeriodicScheduler.Start() should call OnStart")
		}
	})

	t.Run("Start should report OnStart failure", func(t *testing.T) {
		quitChannel := make(chan struct{})

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			OnStart: func(quitChannel chan struct{}) error {
				return fmt.Errorf("Failure message")
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err == nil {
			t.Errorf("PeriodicScheduler.Start() should report OnStart error")
		}

		if err.Error() != "OnStart failed: Failure message" {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}
	})

	t.Run("Start should call Execute if ExecuteOnStart is true", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := false

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnStart: true,
			Execute: func() error {
				called = true
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		if !called {
			t.Errorf("PeriodicScheduler.Start() should call Execute")
		}
	})

	t.Run("Start should call Execute only once if ExecuteOnStart is true", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := 0

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnStart: true,
			Execute: func() error {
				called += 1
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		if called != 1 {
			t.Errorf("PeriodicScheduler.Start() should call Execute only once")
		}
	})

	t.Run("Start should NOT call Execute if ExecuteOnStart is false", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := false

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnStart: false,
			Execute: func() error {
				called = true
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		if called {
			t.Errorf("PeriodicScheduler.Start() should NOT call Execute")
		}
	})

	t.Run(
		"Start should call Execute and OnStart only once if called several times",
		func(t *testing.T) {
			quitChannel := make(chan struct{})
			onStartCalled := 0
			executeCalled := 0

			periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
				ExecuteOnStart: true,
				Execute: func() error {
					executeCalled += 1
					return nil
				},
				OnStart: func(quitChannel chan struct{}) error {
					onStartCalled += 1
					return nil
				},
			}, &logger)

			err := periodicScheduler.Start(quitChannel)
			if err != nil {
				t.Errorf("PeriodicScheduler.Start() error = %v", err)
			}

			err = periodicScheduler.Start(quitChannel)
			if err != nil {
				t.Errorf("PeriodicScheduler.Start() second call error = %v", err)
			}

			if executeCalled != 1 {
				t.Errorf("PeriodicScheduler.Start() should call Execute only once")
			}
			if onStartCalled != 1 {
				t.Errorf("PeriodicScheduler.Start() should call OnStart only once")
			}
		})

	t.Run("Start should be callabale after Shutdown", func(t *testing.T) {
		quitChannel := make(chan struct{})
		onStartCalled := 0
		executeCalled := 0

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnStart: true,
			Execute: func() error {
				executeCalled += 1
				return nil
			},
			OnStart: func(quitChannel chan struct{}) error {
				onStartCalled += 1
				return nil
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		err = periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() after Shutdown error = %v", err)
		}

		if executeCalled != 2 {
			t.Errorf("PeriodicScheduler.Start() should call Execute after Shutdown")
		}
		if onStartCalled != 2 {
			t.Errorf("PeriodicScheduler.Start() should call OnStart after Shutdown")
		}
	})

	t.Run("Start should call Execute periodically", func(t *testing.T) {
		quitChannel := make(chan struct{})
		var called int64

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnStart: false,
			Interval:       10 * time.Millisecond,
			Execute: func() error {
				atomic.AddInt64(&called, 1)
				return nil
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		// Wait until Execute has fired at least 3 times, then stop the
		// scheduler so no further tick can land. The assertion is
		// `>= 3`, not `== 3`: the ticker runs at 10ms and the polling
		// loop checks every 1ms, so on a loaded runner the count can
		// advance past 3 between the loop exit and the read — the
		// test's intent is "Execute is called periodically", not
		// "exactly 3 times". Pinning it to an exact value made the
		// test flaky on the Windows CI runner.
		for atomic.LoadInt64(&called) < 3 {
			time.Sleep(1 * time.Millisecond)
		}
		if err := periodicScheduler.Shutdown(context.Background()); err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		callCount := atomic.LoadInt64(&called)
		if callCount < 3 {
			t.Errorf("PeriodicScheduler.Start() should call Execute periodically, got %d (want >= 3)", callCount)
		}
	})
}

func TestPeriodicScheduler_Retry(t *testing.T) {
	// The historical contract — self-Shutdown once MaxRetries
	// consecutive failures were reached — was the #258 resilience
	// inversion: one transient error permanently killed cloud/PRTG
	// push (MaxRetries unset = 0) and probes died after 3 failed
	// collects with no recovery path. The scheduler now NEVER shuts
	// itself down on Execute errors: it keeps retrying, backing off
	// (skipping ticks) once the threshold is crossed, and resets on
	// the first success.

	t.Run("MaxRetries zero keeps retrying forever, never shuts down", func(t *testing.T) {
		logger := zerolog.New(os.Stderr)
		quitChannel := make(chan struct{})
		var called int64
		var shutdownCalled int64

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			Interval: 5 * time.Millisecond,
			// MaxRetries deliberately unset (= 0): the senhub and
			// PRTG push schedulers are constructed exactly like this.
			Execute: func() error {
				atomic.AddInt64(&called, 1)
				return fmt.Errorf("transient error")
			},
			OnShutdown: func(context.Context) error {
				atomic.AddInt64(&shutdownCalled, 1)
				return nil
			},
		}, &logger)

		if err := periodicScheduler.Start(quitChannel); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer periodicScheduler.Shutdown(context.Background())

		deadline := time.Now().Add(2 * time.Second)
		for atomic.LoadInt64(&called) < 10 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}

		if got := atomic.LoadInt64(&called); got < 10 {
			t.Errorf("Execute called %d times; the first error must not stop the pipeline", got)
		}
		if atomic.LoadInt64(&shutdownCalled) != 0 {
			t.Error("scheduler shut itself down on Execute errors")
		}
	})

	t.Run("MaxRetries crossed backs off but keeps retrying, never shuts down", func(t *testing.T) {
		logger := zerolog.New(os.Stderr)
		quitChannel := make(chan struct{})
		var called int64
		var shutdownCalled int64

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			Interval:   5 * time.Millisecond,
			MaxRetries: 3,
			Execute: func() error {
				atomic.AddInt64(&called, 1)
				return fmt.Errorf("persistent error")
			},
			OnShutdown: func(context.Context) error {
				atomic.AddInt64(&shutdownCalled, 1)
				return nil
			},
		}, &logger)

		if err := periodicScheduler.Start(quitChannel); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer periodicScheduler.Shutdown(context.Background())

		// Past the old kill point (3 calls) the scheduler must still
		// attempt — backoff makes attempts sparser, not absent.
		deadline := time.Now().Add(3 * time.Second)
		for atomic.LoadInt64(&called) <= 4 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}

		if got := atomic.LoadInt64(&called); got <= 4 {
			t.Errorf("Execute called %d times; crossing MaxRetries must back off, not stop", got)
		}
		if atomic.LoadInt64(&shutdownCalled) != 0 {
			t.Error("scheduler shut itself down after MaxRetries")
		}
	})

	t.Run("success resets the failure backoff", func(t *testing.T) {
		logger := zerolog.New(os.Stderr)
		quitChannel := make(chan struct{})
		var called int64
		failures := int64(5)

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			Interval:   5 * time.Millisecond,
			MaxRetries: 2,
			Execute: func() error {
				n := atomic.AddInt64(&called, 1)
				if n <= failures {
					return fmt.Errorf("failing warmup %d", n)
				}
				return nil
			},
		}, &logger)

		if err := periodicScheduler.Start(quitChannel); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer periodicScheduler.Shutdown(context.Background())

		// After recovery the scheduler must settle back to every-tick
		// cadence: expect a healthy stream of successful calls.
		deadline := time.Now().Add(5 * time.Second)
		for atomic.LoadInt64(&called) < failures+10 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := atomic.LoadInt64(&called); got < failures+10 {
			t.Errorf("Execute called %d times; recovery must reset the backoff", got)
		}
	})
}

func TestPeriodicScheduler_Shutdown(t *testing.T) {
	logger := zerolog.New(os.Stderr)

	t.Run("Stop", func(t *testing.T) {
		quitChannel := make(chan struct{})
		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{}, &logger)
		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}
	})

	t.Run("Stop should call Execute only once if ExecuteOnShutdown is true", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := 0

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnShutdown: true,
			Execute: func() error {
				called += 1
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		if called != 1 {
			t.Errorf("PeriodicScheduler.Shutdown() should call Execute only once")
		}
	})

	t.Run("Stop should NOT call Execute if ExecuteOnShutdown is false", func(t *testing.T) {
		quitChannel := make(chan struct{})
		called := false

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnShutdown: false,
			Execute: func() error {
				called = true
				return nil
			},
		}, &logger)
		err := periodicScheduler.Start(quitChannel)

		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		if called {
			t.Errorf("PeriodicScheduler.Shutdown() should NOT call Execute")
		}
	})

	t.Run("Stop should call Execute and OnStart only once if called several times", func(t *testing.T) {
		quitChannel := make(chan struct{})
		onShutdownCalled := 0
		executeCalled := 0

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnShutdown: true,
			Execute: func() error {
				executeCalled += 1
				return nil
			},
			OnShutdown: func(context.Context) error {
				onShutdownCalled += 1
				return nil
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() second call error = %v", err)
		}

		if executeCalled != 1 {
			t.Errorf("PeriodicScheduler.Shutdown() should call Execute only once")
		}
		if onShutdownCalled != 1 {
			t.Errorf("PeriodicScheduler.Shutdown() should call OnShutdown only once")
		}
	})

	t.Run("Stop should call callbacks after restarted", func(t *testing.T) {
		quitChannel := make(chan struct{})
		onShutdownCalled := 0
		executeCalled := 0

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnShutdown: true,
			Execute: func() error {
				executeCalled += 1
				return nil
			},
			OnShutdown: func(context.Context) error {
				onShutdownCalled += 1
				return nil
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}

		err = periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() after Shutdown error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err != nil {
			t.Errorf("PeriodicScheduler.Shutdown() after Start error = %v", err)
		}

		if executeCalled != 2 {
			t.Errorf("PeriodicScheduler.Shutdown() should call Execute after Start")
		}
		if onShutdownCalled != 2 {
			t.Errorf("PeriodicScheduler.Shutdown() should call OnShutdown after Start")
		}
	})

	t.Run("Stop should forward OnShutdown errors", func(t *testing.T) {
		quitChannel := make(chan struct{})

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			OnShutdown: func(context.Context) error {
				return fmt.Errorf("Error")
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err == nil {
			t.Errorf("PeriodicScheduler.Shutdown() should forward OnShutdown errors")
		}

		if err.Error() != "OnShutdown failed: Error" {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}
	})

	t.Run("Stop should forward Execute error on shutdown", func(t *testing.T) {
		quitChannel := make(chan struct{})

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			ExecuteOnShutdown: true,
			Execute: func() error {
				return fmt.Errorf("Error")
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		err = periodicScheduler.Shutdown(context.Background())
		if err == nil {
			t.Errorf("PeriodicScheduler.Shutdown() should forward Execute errors")
		}

		if err.Error() != "Unable to call Execute on shutdown: Error" {
			t.Errorf("PeriodicScheduler.Shutdown() error = %v", err)
		}
	})
}
