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

// collectErrors is the lifetime-monotonic count of probe collection errors
// observed across all probe pollers since the agent started.
var collectErrors atomic.Uint64

// IncrementCollectErrors records one probe collection error.
// Called from ProbePoller.collect() when the underlying Probe.Collect()
// returns a non-nil error.
func IncrementCollectErrors() {
	collectErrors.Add(1)
}

// GetCollectErrorsTotal returns the lifetime collect-error count.
func GetCollectErrorsTotal() uint64 {
	return collectErrors.Load()
}

// ProbeHealthChecker is the minimal interface a probe must implement to
// participate in the senhub_agent_probes_healthy count. Probes that don't
// implement it are counted as healthy by default (running == nominally OK).
type ProbeHealthChecker interface {
	IsHealthy() bool
}

// activeProbesMu guards activeProbes.
var activeProbesMu sync.RWMutex

// activeProbes is the live snapshot of running probes the Sensor publishes.
// Stored as the empty interface to keep this package free of agent-internal
// imports; readers type-assert to ProbeHealthChecker for health checks.
var activeProbes []interface{}

// SetActiveProbes replaces the published list of currently-running probes.
// Called by the Sensor service after every successful configuration sync.
func SetActiveProbes(probes []interface{}) {
	activeProbesMu.Lock()
	cp := make([]interface{}, len(probes))
	copy(cp, probes)
	activeProbes = cp
	activeProbesMu.Unlock()
}

// GetProbeCounts returns (total, healthy) for the currently-active probes.
func GetProbeCounts() (total, healthy int) {
	activeProbesMu.RLock()
	defer activeProbesMu.RUnlock()
	total = len(activeProbes)
	for _, p := range activeProbes {
		if h, ok := p.(ProbeHealthChecker); ok {
			if h.IsHealthy() {
				healthy++
			}
		} else {
			healthy++
		}
	}
	return total, healthy
}
