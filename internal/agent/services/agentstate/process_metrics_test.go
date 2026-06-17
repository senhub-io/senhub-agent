package agentstate

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestGetProcessSnapshot_BasicShape(t *testing.T) {
	snap := GetProcessSnapshot()

	// Goroutines is always at least 1 (the calling test).
	if snap.Goroutines < 1 {
		t.Errorf("Goroutines = %d, want >= 1", snap.Goroutines)
	}

	// HeapBytes should report non-zero — the test process has been
	// alloc'ing for a while by the time we get here.
	if snap.HeapBytes == 0 {
		t.Errorf("HeapBytes = 0, want > 0 (runtime/metrics not wired?)")
	}

	// CPUSecondsTotal should be > 0 unless the process literally just
	// started this exact instant (unlikely in a test). Allow zero
	// because some runtimes return 0 for very-early process states.
	if snap.CPUSecondsTotal < 0 {
		t.Errorf("CPUSecondsTotal = %v, must be non-negative", snap.CPUSecondsTotal)
	}

	// On Linux + Windows, ResidentMemoryBytes should be > 0 (the
	// platform-specific helper returned a real RSS). On macOS and
	// other build targets, getResidentMemory returns 0 by design;
	// accept that.
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if snap.ResidentMemoryBytes == 0 {
			t.Errorf("ResidentMemoryBytes = 0 on %s — OS helper not returning data", runtime.GOOS)
		}
		if snap.OpenFDs == 0 {
			t.Errorf("OpenFDs = 0 on %s — OS helper not returning data", runtime.GOOS)
		}
	} else {
		// On other OSes the stubs return 0 by design.
		if snap.ResidentMemoryBytes != 0 {
			t.Errorf("expected 0 ResidentMemoryBytes stub on %s, got %d", runtime.GOOS, snap.ResidentMemoryBytes)
		}
	}
}

func TestGetProcessSnapshot_CPUMonotonic(t *testing.T) {
	// CPUSecondsTotal is a counter — never decreases. Burn ~10ms of
	// CPU between two reads and confirm.
	first := GetProcessSnapshot().CPUSecondsTotal

	deadline := time.Now().Add(20 * time.Millisecond)
	var x uint64
	for time.Now().Before(deadline) {
		// Simple busy loop to consume CPU time.
		x++
	}
	// Reference x so the loop isn't dead-code-eliminated.
	_ = x

	second := GetProcessSnapshot().CPUSecondsTotal
	if second < first {
		t.Errorf("CPUSecondsTotal went backwards: first=%v second=%v", first, second)
	}
}

func TestGetProcessSnapshot_GoroutineGrowthDetectable(t *testing.T) {
	before := GetProcessSnapshot().Goroutines

	// runtime.NumGoroutine() is process-global: a background goroutine
	// from the runtime or another test exiting between the two
	// snapshots shrinks the count and flaked this test in CI ("at
	// least 8, got 7"). Spawn enough goroutines that single-digit
	// churn cannot flip the assertion, and gate on a ready barrier so
	// all of them are actually started before the second snapshot.
	const spawned = 50
	var ready sync.WaitGroup
	ready.Add(spawned)
	done := make(chan struct{})
	for i := 0; i < spawned; i++ {
		go func() { ready.Done(); <-done }()
	}
	ready.Wait()
	defer close(done)

	after := GetProcessSnapshot().Goroutines
	if after < before+spawned-5 {
		t.Errorf("expected at least %d goroutines, got %d", before+spawned-5, after)
	}
}
