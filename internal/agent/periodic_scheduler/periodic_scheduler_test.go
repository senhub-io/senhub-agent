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
	t.Run("Retry should call Execute until MaxRetries and then Shutdown", func(t *testing.T) {
		logger := zerolog.New(os.Stderr)
		quitChannel := make(chan struct{})
		var called int64
		var shutdownCalled int64

		periodicScheduler := NewPeriodicScheduler(PeriodicSchedulerConfig{
			Interval:   10 * time.Millisecond,
			MaxRetries: 3,
			Execute: func() error {
				atomic.AddInt64(&called, 1)
				return fmt.Errorf("Error")
			},
			OnShutdown: func(context.Context) error {
				atomic.AddInt64(&shutdownCalled, 1)
				return nil
			},
		}, &logger)

		err := periodicScheduler.Start(quitChannel)
		if err != nil {
			t.Errorf("PeriodicScheduler.Start() error = %v", err)
		}

		for atomic.LoadInt64(&shutdownCalled) == 0 {
			time.Sleep(1 * time.Millisecond)
		}

		callCount := atomic.LoadInt64(&called)
		if callCount != 3 {
			t.Errorf("PeriodicScheduler.Start() should call Execute until MaxRetries %d", callCount)
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
