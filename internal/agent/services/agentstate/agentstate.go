// Package agentstate exposes process-lifetime counters and snapshots
// shared between independent subsystems (probes, http strategy) without
// creating an import cycle. Read by the Prometheus exposition bridge to
// produce the senhub_agent_* self-observability metrics.
//
// Why a separate package: the `probes` package depends on `data_store`,
// and `data_store/strategies/http` depends on the `data_store` runtime,
// so importing `probes` from the http strategy creates a cycle. This
// package has no agent-internal dependencies, so anyone can import it.
package agentstate

import (
	"sync"
	"sync/atomic"
)

// The per-(probe,reason) collect-error counter lives in collect_errors.go.

// transformerFallbacks counts datapoints that went through the
// data_store unit-correction pass without a transformer YAML definition
// (legacy fallback transformer: no unit injection, no corrections).
// A nonzero, rising value means a probe is shipping unitless data.
var transformerFallbacks atomic.Uint64

// IncrementTransformerFallbacks records one datapoint processed via the
// legacy fallback transformer. Called from
// data_store.applyUnitCorrections.
func IncrementTransformerFallbacks() {
	transformerFallbacks.Add(1)
}

// GetTransformerFallbacksTotal returns the lifetime fallback count.
func GetTransformerFallbacksTotal() uint64 {
	return transformerFallbacks.Load()
}

// agentInstanceID holds the agent's own service.instance.id — the resolved
// OTLP Resource service.instance.id (defaulted from the agent key), which is
// also the identity of the agent's own service.instance entity emitted by the
// entity foundation. It is set once when entity emission starts and read by
// probe entity sources to stamp the From endpoint of their `monitors` edge,
// so a probe source reaches the agent identity without importing the OTLP
// strategy (which would create an import cycle). Lock-free: a single scalar
// written once at startup, read every detector cycle.
var agentInstanceID atomic.Pointer[string]

// SetAgentInstanceID records the agent's own service.instance.id. Called once
// by the entity-emission startup with the SAME value the foundation stamps on
// the agent's service.instance entity, so a probe's `monitors` From endpoint
// resolves exactly that node in the consumer.
func SetAgentInstanceID(id string) {
	agentInstanceID.Store(&id)
}

// GetAgentInstanceID returns the agent's own service.instance.id, or "" when
// entity emission is disabled or has not started yet. A reader MUST treat ""
// as "no agent identity available — skip the monitors edge" rather than emit
// a relation whose From endpoint cannot resolve (the consumer would buffer
// then drop it).
func GetAgentInstanceID() string {
	if p := agentInstanceID.Load(); p != nil {
		return *p
	}
	return ""
}

// probeStateMu guards activeProbeIDs and probeHealth.
var probeStateMu sync.RWMutex

// activeProbeIDs is the set of probes the Sensor reports as running. We keep
// it as a string set rather than a slice of interfaces because:
//   - it makes "is this probe still active?" a cheap O(1) lookup
//   - it lets us prune stale probeHealth entries on every Sensor sync
//   - we never need to call methods back on the probe (health is pushed in
//     by ProbePoller, not pulled from the probe)
var activeProbeIDs = map[string]struct{}{}

// probeHealthState is the last-collect outcome for a probe. unknown means
// the probe is registered but has not completed its first collect cycle yet
// (or its scheduler hasn't fired). Counted as NOT-healthy in metrics.
type probeHealthState int

const (
	probeHealthUnknown probeHealthState = iota
	probeHealthOK
	probeHealthFailed
)

// probeHealth is keyed by probe ID (the unique identifier from
// probes.GenerateProbeId). Updated by ProbePoller.collect() after each
// cycle via RecordProbeHealth, never by readers — no IsHealthy() callbacks
// invoked at scrape time (which would re-trigger Collect() and cause races).
var probeHealth = map[string]probeHealthState{}

// SetActiveProbes replaces the set of currently-running probes by their IDs.
// Called by the Sensor service after every successful configuration sync.
// Health entries for probes no longer present are pruned to keep the map
// from growing unbounded across reconfig cycles.
func SetActiveProbes(probeIDs []string) {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	newSet := make(map[string]struct{}, len(probeIDs))
	for _, id := range probeIDs {
		newSet[id] = struct{}{}
	}
	activeProbeIDs = newSet
	// Prune health entries for probes no longer active.
	for id := range probeHealth {
		if _, alive := newSet[id]; !alive {
			delete(probeHealth, id)
		}
	}
}

// RecordProbeHealth publishes a probe's current health to the shared map.
// Called by ProbePoller in two paths:
//   - scheduler-driven: after each Collect() cycle, ok = (err == nil)
//   - callback-driven (syslog/event): after each successful datapoint
//     routing, with ok = (routing err == nil)
//
// This means "healthy" reflects "the probe completed its most recent
// activity without an error". For event-driven probes whose listener
// could be silently dead (no incoming traffic, but socket still open),
// "healthy" stays true until traffic resumes and routing fails — pair
// with external probing for socket-level liveness.
//
// Replaces the prior IsHealthy()-at-scrape design which re-executed
// Collect() inline at scrape time (wasted work + races).
func RecordProbeHealth(probeID string, ok bool) {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	if ok {
		probeHealth[probeID] = probeHealthOK
	} else {
		probeHealth[probeID] = probeHealthFailed
	}
}

// GetProbeCounts returns (total, healthy) for the currently-active probes.
// Probes that have not yet run a collect cycle (state=unknown) are NOT
// counted as healthy — until they prove they can collect, they're suspect.
func GetProbeCounts() (total, healthy int) {
	probeStateMu.RLock()
	defer probeStateMu.RUnlock()
	total = len(activeProbeIDs)
	for id := range activeProbeIDs {
		if probeHealth[id] == probeHealthOK {
			healthy++
		}
	}
	return total, healthy
}
