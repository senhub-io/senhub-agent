package agentmetrics

import (
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// AgentMetricsSnapshot is a frozen-in-time view of the agent's own
// operational state. The bridge in package http populates this from the
// running strategy (cache stats, uptime, build info) and passes it to
// BuildAgentRecords, which converts each field into an otelmapper.OtelRecord ready
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

// BuildAgentRecords produces the otelmapper.OtelRecord set for the agent's own
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
func BuildAgentRecords(snap AgentMetricsSnapshot) []otelmapper.OtelRecord {
	uptime := time.Since(snap.StartTime).Seconds()

	records := []otelmapper.OtelRecord{
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

	// HTTP requests by endpoint — one otelmapper.OtelRecord per (route template) pair.
	// Endpoint label keeps cardinality bounded to the number of registered routes.
	for endpoint, count := range snap.HTTPRequestsByEndpoint {
		records = append(records, otelmapper.OtelRecord{
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

	// OTLP push self-metrics — counters about the OTLP strategy's
	// own activity. Per IMPLEMENTATION-PLAN §7, exposed on the
	// Prometheus path only (not pushed via OTLP itself) to avoid
	// feedback loops where a failing OTLP export emits metrics that
	// also need to go through OTLP.
	//
	// Always emitted, even when the OTLP strategy is not configured:
	// counters at 0 communicate "no activity" clearly, and the
	// dashboards using these metrics handle the all-zero case
	// gracefully.
	records = append(records,
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.metrics.pushed",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPMetricsPushedTotal()),
			Description: "Cumulative count of metric records successfully pushed via OTLP/gRPC.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.logs.pushed",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPLogsPushedTotal()),
			Description: "Cumulative count of log records emitted via OTLP/gRPC.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.export.errors",
			Unit:        "{error}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPExportErrorsTotal()),
			Description: "Cumulative count of OTLP exports that failed after retries were exhausted.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.dropped_log_records",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetDroppedLogRecordsTotal()),
			Description: "Cumulative count of log records dropped due to subscriber backpressure on the agent log channel.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.buffer.fill_ratio",
			Unit:        "1",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       agentstate.LogChannelFillRatio(),
			Description: "Highest fill ratio (0..1) across all active log channel subscriptions at scrape time. Approaches 1 means the consumer is falling behind producers.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.store_size",
			Unit:        "{series}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPStoreSize()),
			Description: "Number of distinct series held in the OTLP strategy's last-writer-wins metric store at the last push.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.export.duration",
			Unit:        "s",
			Type:        "gauge",
			Attributes:  map[string]string{"window": "last"},
			Value:       agentstate.GetOTLPLastExportDuration().Seconds(),
			Description: "Wall-clock duration of the most recent successful OTLP metrics export.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.export.duration",
			Unit:        "s",
			Type:        "gauge",
			Attributes:  map[string]string{"window": "mean"},
			Value:       agentstate.GetOTLPMeanExportDuration().Seconds(),
			Description: "All-time mean of successful OTLP metrics export durations.",
		},
	)

	// Per-reason drop counters — emitted as a single OTel metric with
	// `reason` attribute. Operators alert on this rising. Today the only
	// reason emitted is `store_cap` (cardinality cap on the metric store);
	// future reasons will be added without changing this metric shape.
	for reason, n := range agentstate.GetOTLPDroppedByReason() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.dropped",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{"reason": reason},
			Value:       float64(n),
			Description: "Cumulative count of OTLP records dropped before export, by reason (store_cap, queue_full, …).",
		})
	}

	// Checkpoint self-metrics. These are emitted regardless of whether
	// persistence is enabled — the size+age gauges read 0 when disabled
	// (distinguishable from "never saved" only by also checking the
	// errors counter for nonzero values, which always start at 0
	// regardless). Operators rely on these to detect a stuck
	// checkpoint (size_bytes flat for hours, errors rising).
	records = append(records,
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.checkpoint.size",
			Unit:        "By",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPCheckpointSize()),
			Description: "Size in bytes of the most recently written OTLP checkpoint file. 0 when persistence disabled or no save has succeeded yet.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.checkpoint.last_save_age",
			Unit:        "s",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       agentstate.GetOTLPCheckpointLastSaveAge().Seconds(),
			Description: "Seconds since the most recent successful OTLP checkpoint save. 0 when persistence disabled or no save has succeeded yet.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.checkpoint.restored_entries",
			Unit:        "{series}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPCheckpointRestoredCount()),
			Description: "Number of entries restored from the OTLP checkpoint at the last agent boot. 0 means no restore (no file present, persistence disabled, or fresh install).",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.parallel.sub_batches",
			Unit:        "{batch}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPSubBatchCount()),
			Description: "Number of sub-batches the most recent OTLP push fanned out across. 1 means the single-batch path (max_concurrent_exports=1 or cycle below threshold); >1 means per-probe parallel export fired.",
		},
	)
	for stage, n := range agentstate.GetOTLPCheckpointErrorsByStage() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.checkpoint.errors",
			Unit:        "{error}",
			Type:        "counter",
			Attributes:  map[string]string{"stage": stage},
			Value:       float64(n),
			Description: "Cumulative count of OTLP checkpoint operation failures, by stage (read, parse, version_mismatch, mkdir, create_tmp, encode, fsync, close, rename).",
		})
	}

	// Process self-monitoring — calqued on Grafana Alloy's resources
	// mixin so operators recognize the names. Captured at scrape time
	// via agentstate.GetProcessSnapshot() (cross-OS, ~µs cost).
	proc := agentstate.GetProcessSnapshot()
	records = append(records,
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.cpu.time",
			Unit:        "s",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       proc.CPUSecondsTotal,
			Description: "Cumulative CPU time consumed by the agent process since startup, in seconds.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.memory.resident",
			Unit:        "By",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(proc.ResidentMemoryBytes),
			Description: "Resident set size of the agent process, in bytes (OS-reported VmRSS / WorkingSetSize). 0 if the running OS exposes no such counter.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.memory.heap",
			Unit:        "By",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(proc.HeapBytes),
			Description: "Go runtime heap memory currently allocated for objects, in bytes. Useful with process.memory.resident to spot heap-vs-OS memory leaks.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.goroutines",
			Unit:        "{goroutine}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(proc.Goroutines),
			Description: "Number of goroutines currently running in the agent process. Monotonic growth typically indicates a goroutine leak.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.gc.cycles",
			Unit:        "{cycle}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(proc.GCCyclesTotal),
			Description: "Cumulative number of Go garbage collection cycles. Rate over time exposes GC pressure.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.process.open_fds",
			Unit:        "{fd}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(proc.OpenFDs),
			Description: "Open file descriptors / Windows handles for the agent process. 0 if the running OS exposes no such counter.",
		},
	)

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
		records = append(records, otelmapper.OtelRecord{
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
