package agentstate

import "sync"

// The OTLP receiver probe accepts external OTLP over gRPC/HTTP and either
// converts it (metrics) or relays it (logs, traces). These counters answer
// "is the receiver taking traffic, and is any of it being discarded" without
// log correlation. Kept here (not in the probe package) so the agentmetrics
// builder reads them without importing probes — the import-cycle reason the
// rest of agentstate exists.

// otlpReceiverIngested counts items accepted by the receiver, keyed by signal
// ("metrics", "logs", "traces"). The item unit is per-signal: for metrics it
// is the EMITTED internal datapoints, counted after family expansion (a
// summary expands to _count/_sum plus one point per quantile, an exponential
// histogram to its scalar aggregates), not the received OTLP points — the
// number that actually lands in the sinks and that the dropped counter
// complements. Logs count records and traces count spans, where received and
// emitted are the same thing.
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

// SeedOTLPReceiverIngested ensures the ingest counter has a zero entry for each
// given signal, so a configured-but-idle receiver is already visible on the
// pull endpoints (Prometheus/PRTG/Nagios) instead of showing nothing until its
// first datapoint. The agentmetrics builder ranges this map and only emits the
// signals it finds, so without a seed a receiver that has not yet taken traffic
// exposes no receiver telemetry at all (#688). Seeding never lowers an existing
// count — a signal that already has ingested items keeps its value.
func SeedOTLPReceiverIngested(signals ...string) {
	otlpReceiverIngested.mu.Lock()
	defer otlpReceiverIngested.mu.Unlock()
	for _, s := range signals {
		if s == "" {
			continue
		}
		if _, ok := otlpReceiverIngested.m[s]; !ok {
			otlpReceiverIngested.m[s] = 0
		}
	}
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

// OTLPReceiverCoverageKey labels one coverage series: which signal, which
// path the records came in by (uds, tcp_loopback, remote) and which
// service sent them.
type OTLPReceiverCoverageKey struct {
	Signal  string
	Origin  string
	Service string
}

// OTLPReceiverCoverage is the record count behind one key, and how many
// of those records still carried no host.id once the receiver was done
// with them.
type OTLPReceiverCoverage struct {
	Received      uint64
	WithoutHostID uint64
}

// maxCoverageServices bounds the service.name label. A relay can see many
// senders, and one misbehaving SDK that sets a random service name must
// not grow the agent's own telemetry without end; past the cap the
// records still count, under "other".
const maxCoverageServices = 200

var otlpReceiverCoverage = struct {
	mu sync.Mutex
	m  map[OTLPReceiverCoverageKey]OTLPReceiverCoverage
}{m: map[OTLPReceiverCoverageKey]OTLPReceiverCoverage{}}

// RecordOTLPReceiverCoverage counts records relayed by the receiver, and
// those that left without a host.id, so whether logs and traces can be
// joined to their host is a number rather than a hope.
func RecordOTLPReceiverCoverage(signal, origin, service string, records int, hasHostID bool) {
	if records <= 0 {
		return
	}
	if service == "" {
		service = "unknown"
	}
	otlpReceiverCoverage.mu.Lock()
	defer otlpReceiverCoverage.mu.Unlock()
	key := OTLPReceiverCoverageKey{Signal: signal, Origin: origin, Service: service}
	if _, seen := otlpReceiverCoverage.m[key]; !seen && coverageServiceCount() >= maxCoverageServices {
		key.Service = "other"
	}
	c := otlpReceiverCoverage.m[key]
	c.Received += uint64(records)
	if !hasHostID {
		c.WithoutHostID += uint64(records)
	}
	otlpReceiverCoverage.m[key] = c
}

// coverageServiceCount counts the distinct services held. Callers hold mu.
func coverageServiceCount() int {
	seen := map[string]bool{}
	for k := range otlpReceiverCoverage.m {
		seen[k.Service] = true
	}
	return len(seen)
}

// GetOTLPReceiverCoverage returns a snapshot of the coverage counters.
func GetOTLPReceiverCoverage() map[OTLPReceiverCoverageKey]OTLPReceiverCoverage {
	otlpReceiverCoverage.mu.Lock()
	defer otlpReceiverCoverage.mu.Unlock()
	out := make(map[OTLPReceiverCoverageKey]OTLPReceiverCoverage, len(otlpReceiverCoverage.m))
	for k, v := range otlpReceiverCoverage.m {
		out[k] = v
	}
	return out
}
