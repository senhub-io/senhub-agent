package agentstate

import "sync"

// The OTLP receiver probe accepts external OTLP over gRPC/HTTP and either
// converts it (metrics) or relays it (logs, traces). These counters answer
// "is the receiver taking traffic, and is any of it being discarded" without
// log correlation. Kept here (not in the probe package) so the agentmetrics
// builder reads them without importing probes — the import-cycle reason the
// rest of agentstate exists.

// otlpReceiverIngested counts items accepted by the receiver, keyed by signal
// ("metrics", "logs", "traces"). The item unit is per-signal — metric
// datapoints, log records, spans — so it measures activity per signal rather
// than a cross-signal total.
var otlpReceiverIngested = struct {
	mu sync.RWMutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// otlpReceiverDropKey labels one discarded-item series. Both fields are
// bounded: signal is metrics/logs/traces, reason a small fixed enum —
// "no_sink" (logs/traces received with no export strategy to relay to) or
// "unmapped" (a metric with an unrecognized/unset data type).
type otlpReceiverDropKey struct {
	Signal string
	Reason string
}

var otlpReceiverDropped = struct {
	mu sync.RWMutex
	m  map[otlpReceiverDropKey]uint64
}{m: map[otlpReceiverDropKey]uint64{}}

// IncrementOTLPReceiverIngested records n items accepted for a signal.
func IncrementOTLPReceiverIngested(signal string, n int) {
	if signal == "" || n <= 0 {
		return
	}
	otlpReceiverIngested.mu.Lock()
	otlpReceiverIngested.m[signal] += uint64(n)
	otlpReceiverIngested.mu.Unlock()
}

// GetOTLPReceiverIngestedBySignal returns a snapshot copy of the per-signal
// ingest counters.
func GetOTLPReceiverIngestedBySignal() map[string]uint64 {
	otlpReceiverIngested.mu.RLock()
	defer otlpReceiverIngested.mu.RUnlock()
	out := make(map[string]uint64, len(otlpReceiverIngested.m))
	for k, v := range otlpReceiverIngested.m {
		out[k] = v
	}
	return out
}

// IncrementOTLPReceiverDropped records n items discarded for (signal, reason).
func IncrementOTLPReceiverDropped(signal, reason string, n int) {
	if signal == "" || reason == "" || n <= 0 {
		return
	}
	key := otlpReceiverDropKey{Signal: signal, Reason: reason}
	otlpReceiverDropped.mu.Lock()
	otlpReceiverDropped.m[key] += uint64(n)
	otlpReceiverDropped.mu.Unlock()
}

// GetOTLPReceiverDroppedBySignal returns a snapshot copy of the
// per-(signal, reason) drop counters.
func GetOTLPReceiverDroppedBySignal() map[otlpReceiverDropKey]uint64 {
	otlpReceiverDropped.mu.RLock()
	defer otlpReceiverDropped.mu.RUnlock()
	out := make(map[otlpReceiverDropKey]uint64, len(otlpReceiverDropped.m))
	for k, v := range otlpReceiverDropped.m {
		out[k] = v
	}
	return out
}

// ResetOTLPReceiverCountersForTest clears both counters. Test-only.
func ResetOTLPReceiverCountersForTest() {
	otlpReceiverIngested.mu.Lock()
	otlpReceiverIngested.m = map[string]uint64{}
	otlpReceiverIngested.mu.Unlock()
	otlpReceiverDropped.mu.Lock()
	otlpReceiverDropped.m = map[otlpReceiverDropKey]uint64{}
	otlpReceiverDropped.mu.Unlock()
}
