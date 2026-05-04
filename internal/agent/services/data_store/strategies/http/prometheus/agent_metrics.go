package prometheus

import (
	"time"
)

// AgentMetricsSnapshot is a frozen-in-time view of the agent's own
// operational state. The bridge in package http populates this from the
// running strategy (cache stats, uptime, build info) and passes it to
// BuildAgentRecords, which converts each field into an OtelRecord ready
// for the serializer.
//
// Per IMPLEMENTATION-PLAN §9, agent self-metrics are ALWAYS emitted when
// the prometheus endpoint is enabled — they don't carry probe_* labels and
// are not subject to expose_host_metrics filtering (those control probe
// metrics, not agent metrics).
type AgentMetricsSnapshot struct {
	// StartTime is when the agent process started. Used to compute uptime.
	StartTime time.Time

	// CacheEntries is the total number of distinct time series in the
	// shared MetricCache (from MetricCache.GetCacheInfo().TotalMetrics).
	CacheEntries int

	// ProbesActive is the number of distinct probes that have emitted at
	// least one data point in the current cache window (from cache.ProbeCount).
	ProbesActive int

	// ProbesTotal is the number of probes the agent is currently running
	// (from the probes package's published active set).
	ProbesTotal int

	// ProbesHealthy is the number of running probes whose IsHealthy() is true.
	ProbesHealthy int

	// CollectErrorsTotal is the lifetime count of probe collection errors.
	CollectErrorsTotal uint64

	// HTTPRequestsByEndpoint is a snapshot of (route template → request count).
	HTTPRequestsByEndpoint map[string]uint64

	// Build info — emitted as a gauge of value 1 with version+commit labels.
	BuildVersion string
	BuildCommit  string
}

// BuildAgentRecords produces the OtelRecord set for the agent's own
// self-observability metrics. Always emitted regardless of probe state.
//
// Metric definitions (per IMPLEMENTATION-PLAN §9):
//   - senhub.agent.uptime_seconds (gauge, s)
//   - senhub.agent.cache.entries (gauge, {entry})
//   - senhub.agent.probes.active (gauge, {probe})
//   - senhub.agent.build.info (gauge, value=1, with version/commit labels)
//
// All metrics listed in IMPLEMENTATION-PLAN §9 are now wired (collect
// errors via agentstate.IncrementCollectErrors, http requests via the
// CountRequests middleware, probes.healthy via push-based
// agentstate.RecordProbeHealth from ProbePoller.collect).
func BuildAgentRecords(snap AgentMetricsSnapshot) []OtelRecord {
	uptime := time.Since(snap.StartTime).Seconds()

	records := []OtelRecord{
		{
			Name:        "senhub.agent.uptime_seconds",
			Unit:        "s",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       uptime,
			Description: "SenHub agent uptime in seconds since process start.",
		},
		{
			Name:        "senhub.agent.cache.entries",
			Unit:        "{entry}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(snap.CacheEntries),
			Description: "Number of distinct time series held in the shared metric cache.",
		},
		{
			Name:        "senhub.agent.probes.active",
			Unit:        "{probe}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(snap.ProbesActive),
			Description: "Number of probes that have emitted at least one data point in the cache window.",
		},
		{
			Name:        "senhub.agent.probes.total",
			Unit:        "{probe}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(snap.ProbesTotal),
			Description: "Number of probes the agent is currently running (from configuration).",
		},
		{
			Name:       "senhub.agent.probes.healthy",
			Unit:       "{probe}",
			Type:       "gauge",
			Attributes: map[string]string{},
			Value:      float64(snap.ProbesHealthy),
			Description: "Number of running probes whose last collect cycle succeeded. " +
				"Probes that have not yet completed their first cycle (or whose " +
				"collection happens via a callback rather than a scheduler) are " +
				"NOT counted as healthy until they explicitly publish their state.",
		},
		{
			Name:        "senhub.agent.collect.errors",
			Unit:        "{error}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(snap.CollectErrorsTotal),
			Description: "Lifetime count of probe collection errors since agent start.",
		},
	}

	// HTTP requests by endpoint — one OtelRecord per (route template) pair.
	// Endpoint label keeps cardinality bounded to the number of registered routes.
	for endpoint, count := range snap.HTTPRequestsByEndpoint {
		records = append(records, OtelRecord{
			Name: "senhub.agent.http.requests",
			Unit: "{request}",
			Type: "counter",
			Attributes: map[string]string{
				"endpoint": endpoint,
			},
			Value:       float64(count),
			Description: "Lifetime count of HTTP requests served per route template.",
		})
	}

	// Build info — gauge of value 1 with version + commit labels. Standard
	// Prometheus pattern for static metadata that lets dashboards and
	// alerting rules join on the build dimension via group_left.
	if snap.BuildVersion != "" || snap.BuildCommit != "" {
		buildAttrs := map[string]string{}
		if snap.BuildVersion != "" {
			buildAttrs["version"] = snap.BuildVersion
		}
		if snap.BuildCommit != "" {
			buildAttrs["commit"] = snap.BuildCommit
		}
		records = append(records, OtelRecord{
			Name: "senhub.agent.build_info",
			// Empty unit: by Prometheus convention, *_info metrics carry
			// metadata via labels and have no unit suffix. Compatible with
			// node_exporter_build_info, prometheus_build_info, etc.
			Unit:        "",
			Type:        "gauge",
			Attributes:  buildAttrs,
			Value:       1,
			Description: "SenHub agent build information (gauge always 1; version and commit as labels).",
		})
	}

	return records
}
