package otlp

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// memoryState describes the current pressure level reported by the
// background poller. atomic.Int32 so the hot path (upsert) reads it
// without locking. The poller writes; upsert reads.
type memoryState int32

const (
	memoryOK   memoryState = 0
	memorySoft memoryState = 1 // refuse new series, keep existing
	memoryHard memoryState = 2 // refuse all writes + force GC
)

// memoryLimiter polls runtime.MemStats.HeapAlloc and exposes a state
// flag (ok / soft / hard) that the metric_store consults on the
// upsert hot path. Modelled on the OTel Collector memory_limiter
// processor, but scoped to the OTLP strategy's metric store rather
// than the whole agent — that's where the unbounded cardinality
// risk actually lives in senhub-agent.
//
// Why HeapAlloc rather than RSS:
//   - HeapAlloc is the Go-heap component, what we control through
//     drop policies (RSS includes Go runtime overhead + cgo + JVM
//     etc., not directly steerable from this code path).
//   - Cross-platform without sycall.Getrusage tricks.
//   - The agent's typical RSS / HeapAlloc ratio is ~2× — a 400 MiB
//     HeapAlloc threshold maps to ~800 MiB RSS, which is the
//     systemd MemoryMax we ship with.
//
// Lifecycle: start() launches the poll goroutine; stop() closes the
// quit channel and the goroutine exits at the next poll boundary.
// Safe to call stop() multiple times.
type memoryLimiter struct {
	softLimit     uint64 // bytes; 0 disables
	hardLimit     uint64 // bytes; 0 disables
	checkInterval time.Duration

	state atomic.Int32 // current memoryState

	mu        sync.Mutex
	started   bool
	stopOnce  bool
	stopCh    chan struct{}
	stoppedCh chan struct{} // closed when the poll loop has fully exited
}

// newMemoryLimiter returns a configured limiter. softLimit/hardLimit
// are in bytes; pass 0 to disable. checkInterval is the poll cadence.
// Reasonable defaults are 200 MiB soft, 400 MiB hard, 5 s interval.
func newMemoryLimiter(softLimit, hardLimit uint64, checkInterval time.Duration) *memoryLimiter {
	if checkInterval <= 0 {
		checkInterval = 5 * time.Second
	}
	return &memoryLimiter{
		softLimit:     softLimit,
		hardLimit:     hardLimit,
		checkInterval: checkInterval,
		stopCh:        make(chan struct{}),
		stoppedCh:     make(chan struct{}),
	}
}

// enabled reports whether the limiter has at least one threshold set.
// A zero-config limiter is effectively a no-op that always reports OK.
func (m *memoryLimiter) enabled() bool {
	if m == nil {
		return false
	}
	return m.softLimit > 0 || m.hardLimit > 0
}

// currentState returns the latest poll result. Lock-free.
func (m *memoryLimiter) currentState() memoryState {
	if m == nil {
		return memoryOK
	}
	return memoryState(m.state.Load())
}

// start launches the background poller. Returns immediately. The
// poller runs until stop() is called or ctx is cancelled. Calling
// start twice is a no-op (the second call returns immediately).
func (m *memoryLimiter) start(ctx context.Context) {
	if !m.enabled() {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.runLoop(ctx)
}

// stop signals the poller to exit and waits for it. Idempotent and
// safe to call even when start() was never called (returns
// immediately in that case — there's no goroutine to wait for).
func (m *memoryLimiter) stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopOnce {
		m.mu.Unlock()
		return
	}
	m.stopOnce = true
	wasStarted := m.started
	m.mu.Unlock()

	close(m.stopCh)
	if wasStarted {
		<-m.stoppedCh
	}
}

// runLoop is the poll goroutine body. Reads MemStats every
// checkInterval and updates the state flag. On hard-limit hit it
// forces a GC pass in case the spike is reclaimable.
func (m *memoryLimiter) runLoop(ctx context.Context) {
	defer close(m.stoppedCh)

	// First read before the first sleep so the limiter has accurate
	// state immediately rather than during the warm-up period.
	m.pollAndUpdate()

	t := time.NewTicker(m.checkInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			m.pollAndUpdate()
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// pollAndUpdate reads MemStats, picks the new state, stores it. If
// the new state is hard, also kick a GC — best-effort attempt to
// drop us back below the soft threshold before the next probe write.
func (m *memoryLimiter) pollAndUpdate() {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)

	next := memoryOK
	if m.hardLimit > 0 && s.HeapAlloc >= m.hardLimit {
		next = memoryHard
	} else if m.softLimit > 0 && s.HeapAlloc >= m.softLimit {
		next = memorySoft
	}

	prev := memoryState(m.state.Swap(int32(next)))
	if next == memoryHard {
		// Force GC to try to clear the pressure for the next tick.
		// Cheap when there's nothing to collect; expensive when there
		// is — but the alternative is OOM-kill, which is worse.
		runtime.GC()
	}

	_ = prev // hook point for future logging on transitions
}
