package agentstate

import "sync"

// pushBufferDropped counts datapoints dropped by the bounded push
// buffers (senhub cloud, PRTG push), labelled by strategy. Before the
// cap, an intake outage grew these buffers until OOM (#267).
var pushBufferDropped = struct {
	mu sync.Mutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// IncrementPushBufferDropped records n datapoints dropped by the named
// strategy's push buffer because the cap was reached.
func IncrementPushBufferDropped(strategy string, n int) {
	if n <= 0 {
		return
	}
	pushBufferDropped.mu.Lock()
	pushBufferDropped.m[strategy] += uint64(n)
	pushBufferDropped.mu.Unlock()
}

// GetPushBufferDropped returns a copy of the per-strategy drop counts.
func GetPushBufferDropped() map[string]uint64 {
	pushBufferDropped.mu.Lock()
	defer pushBufferDropped.mu.Unlock()
	out := make(map[string]uint64, len(pushBufferDropped.m))
	for k, v := range pushBufferDropped.m {
		out[k] = v
	}
	return out
}

// ResetPushBufferDroppedForTest clears the counters. Test-only.
func ResetPushBufferDroppedForTest() {
	pushBufferDropped.mu.Lock()
	pushBufferDropped.m = map[string]uint64{}
	pushBufferDropped.mu.Unlock()
}
