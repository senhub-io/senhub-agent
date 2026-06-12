package agentstate

import "sync"

// httpCacheDropped counts datapoints refused by the http strategy's
// MetricCache, keyed by reason — same shape as otlpDropped so the
// exposition bridge can pivot on the reason attribute. The only reason
// emitted today is "http_cache_cap" (cardinality cap reached); keep the
// set small and stable.
//
// Lives here (not in the http strategy) for the same reason as the OTLP
// counters: the agentmetrics builder must read it without importing the
// strategy package.
var httpCacheDropped = struct {
	mu sync.RWMutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// IncrementHTTPCacheDropped records one datapoint refused by the http
// MetricCache, labelled by reason. Surfaces as the
// `senhub.agent.cache.dropped` counter.
func IncrementHTTPCacheDropped(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	httpCacheDropped.mu.Lock()
	httpCacheDropped.m[reason]++
	httpCacheDropped.mu.Unlock()
}

// GetHTTPCacheDroppedByReason returns a snapshot copy of the per-reason
// drop counters. The map is safe to mutate by the caller.
func GetHTTPCacheDroppedByReason() map[string]uint64 {
	httpCacheDropped.mu.RLock()
	defer httpCacheDropped.mu.RUnlock()
	out := make(map[string]uint64, len(httpCacheDropped.m))
	for k, v := range httpCacheDropped.m {
		out[k] = v
	}
	return out
}

// ResetHTTPCacheDroppedForTest clears the per-reason drop counters so
// tests in other packages don't observe state leaked by prior runs in
// the same `go test` process. Production code must never call this.
func ResetHTTPCacheDroppedForTest() {
	httpCacheDropped.mu.Lock()
	httpCacheDropped.m = map[string]uint64{}
	httpCacheDropped.mu.Unlock()
}
