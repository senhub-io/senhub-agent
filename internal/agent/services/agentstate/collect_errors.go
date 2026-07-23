package agentstate

import "sync"

// collectErrorKey labels one probe-collection error series. Both fields are
// bounded on purpose so the counter's cardinality stays small:
//   - probe:  the probe TYPE (cpu, redfish, mysql, …) — a compile-time-bounded
//     set from the registry, NOT the per-instance probe name (which grows with
//     configuration and would need pruning). Attributing a spike to a probe
//     type is the actionable signal without unbounded cardinality.
//   - reason: a small fixed enum — "collect" (Probe.Collect returned an error),
//     "timeout" (that error was a deadline/timeout) or "route" (routing the
//     collected data to the strategies failed). Never a raw error string.
type collectErrorKey struct {
	Probe  string
	Reason string
}

// collectErrors counts probe-collection errors keyed by (probe type, reason).
// Replaces the former single lifetime counter so a spike can be attributed to
// a probe and a failure cause without log correlation (#646).
//
// Lives in agentstate (not the probes package) so the agentmetrics builder can
// read it without importing probes — the same import-cycle reason the rest of
// this package exists for.
var collectErrors = struct {
	mu sync.RWMutex
	m  map[collectErrorKey]uint64
}{m: map[collectErrorKey]uint64{}}

// IncrementCollectErrors records one probe-collection error for (probe, reason).
// Called from ProbePoller: on Probe.Collect() failure (reason "collect" or
// "timeout") and on a routing failure in the callback path (reason "route").
func IncrementCollectErrors(probe, reason string) {
	if probe == "" {
		probe = "unknown"
	}
	if reason == "" {
		reason = "collect"
	}
	key := collectErrorKey{Probe: probe, Reason: reason}
	collectErrors.mu.Lock()
	collectErrors.m[key]++
	collectErrors.mu.Unlock()
}

// GetCollectErrorsByLabel returns a snapshot copy of the per-(probe,reason)
// error counters. Safe for the caller to mutate.
func GetCollectErrorsByLabel() map[collectErrorKey]uint64 {
	collectErrors.mu.RLock()
	defer collectErrors.mu.RUnlock()
	out := make(map[collectErrorKey]uint64, len(collectErrors.m))
	for k, v := range collectErrors.m {
		out[k] = v
	}
	return out
}

// GetCollectErrorsTotal returns the lifetime collect-error count across every
// (probe, reason). Kept as a convenience for callers that want the aggregate
// without pivoting on labels.
func GetCollectErrorsTotal() uint64 {
	collectErrors.mu.RLock()
	defer collectErrors.mu.RUnlock()
	var total uint64
	for _, v := range collectErrors.m {
		total += v
	}
	return total
}

// ResetCollectErrorsForTest clears the counters so tests in other packages
// don't observe state leaked by prior runs in the same `go test` process.
// Production code must never call this.
func ResetCollectErrorsForTest() {
	collectErrors.mu.Lock()
	collectErrors.m = map[collectErrorKey]uint64{}
	collectErrors.mu.Unlock()
}
