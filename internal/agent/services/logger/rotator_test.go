package logger

import (
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
)

// TestRotatorIsSharedPerPath pins the model: one rotator per file path,
// for the process. Two rotators writing one file fight over the rotation
// rename, and each one carries its own background mill goroutine.
func TestRotatorIsSharedPerPath(t *testing.T) {
	a := rotatorFor("/tmp/senhub-rotator-test-a.log")
	again := rotatorFor("/tmp/senhub-rotator-test-a.log")
	b := rotatorFor("/tmp/senhub-rotator-test-b.log")

	if a != again {
		t.Error("two rotators handed out for the same path — they would fight over the rotation rename")
	}
	if a == b {
		t.Error("one rotator shared across two paths — they would write each other's file")
	}
}

// TestRepeatedLoggerConstructionDoesNotLeak is the acceptance test of
// #835. Building loggers in a loop must not grow the goroutine count.
//
// The remedy the issue proposed — give Logger a Close that closes the
// rotator — does not work: lumberjack's Close closes the file but never
// closes the channel its mill goroutine ranges over, so the goroutine
// outlives Close and there is no upstream way to stop it. Sharing the
// rotator per path is what actually bounds the count.
func TestRepeatedLoggerConstructionDoesNotLeak(t *testing.T) {
	args := &cliArgs.ParsedArgs{}

	// Warm up: the first construction pays the one-time costs (rotator,
	// mill goroutine, bootstrap logger) that must NOT be counted as
	// growth.
	l := NewLogger(args)
	l.Info().Msg("warm-up")
	baseline := stableGoroutines(t)

	const constructions = 20
	for i := 0; i < constructions; i++ {
		l := NewLogger(args)
		// Write, because the mill goroutine starts on first write, not
		// on construction — a test that only constructs would pass even
		// with a rotator per logger.
		l.Info().Msg("leak probe")
	}

	after := stableGoroutines(t)

	// A per-construction leak grows with the loop count, so it blows
	// well past this; the tolerance only absorbs runtime-owned churn.
	const tolerance = 3
	if after > baseline+tolerance {
		t.Errorf("goroutine leak across %d logger constructions: baseline=%d after=%d (tolerance %d)",
			constructions, baseline, after, tolerance)
	}
}

func stableGoroutines(t *testing.T) int {
	t.Helper()

	last := runtime.NumGoroutine()
	stable := 0
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		n := runtime.NumGoroutine()
		if n == last {
			if stable++; stable >= 3 {
				return n
			}
		} else {
			stable = 0
			last = n
		}
	}
	return runtime.NumGoroutine()
}
