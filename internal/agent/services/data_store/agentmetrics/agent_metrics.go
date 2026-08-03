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
		// Read directly from agentstate (like the OTLP counters below)
		// rather than through the snapshot, so every exposition bridge
		// picks it up without plumbing changes. Nonzero means a probe is
		// shipping datapoints with no transformer YAML — no unit
		// injection, no corrections.
		{
			Name:        "senhub.agent.transformer.fallback",
			Unit:        "{datapoint}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetTransformerFallbacksTotal()),
			Description: "Cumulative count of datapoints processed without a transformer YAML definition (legacy fallback: no unit injection, no unit corrections).",
		},
	}

	// Probe collection errors — one record per (probe, reason). Emitted
	// only once an error has occurred, same emitted-when-touched shape as
	// the other per-reason counters below (#646). Both labels are bounded:
	// `probe` is the probe TYPE (registry-bounded), `reason` a fixed enum
	// (collect / timeout / route). Sum across labels for the old total.
	for key, n := range agentstate.GetCollectErrorsByLabel() {
		records = append(records, otelmapper.OtelRecord{
			Name: "senhub.agent.collect.errors",
			Unit: "{error}",
			Type: "counter",
			Attributes: map[string]string{
				"probe":  key.Probe,
				"reason": key.Reason,
			},
			Value:       float64(n),
			Description: "Cumulative count of probe collection errors since agent start, by probe type and reason (collect, timeout, route).",
		})
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
			Name:        "senhub.agent.otlp.dropped_span_batches",
			Unit:        "{batch}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetDroppedSpanBatchesTotal()),
			Description: "Cumulative count of received span batches dropped due to backpressure on the agent span channel (the trace relay could not keep up).",
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

	// Self-update rejections — artifact verification refused an update
	// (signature_unavailable, signature_invalid). A nonzero value is a
	// mis-published release or an attempted supply-chain tamper (#266);
	// operators should alert on this rising.
	for reason, n := range agentstate.GetUpdateRejectedByReason() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.update.rejected",
			Unit:        "{attempt}",
			Type:        "counter",
			Attributes:  map[string]string{"reason": reason},
			Value:       float64(n),
			Description: "Cumulative count of self-update attempts refused by artifact verification, by reason.",
		})
	}

	// License rejection state — a configured licence the validator refused,
	// labelled by reason (validation_failed, binding_mismatch,
	// expired_no_grace). 1 means the agent is silently degraded to free tier
	// despite a licence being configured; 0 after a licence validates. Always
	// emitted so dashboards can alert on `senhub.agent.license.invalid > 0`
	// instead of the rejection hiding in a single log line (#486). A host with
	// no licence configured never sets this, so the gauge stays absent there.
	for reason, n := range agentstate.GetLicenseInvalidByReason() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.license.invalid",
			Unit:        "1",
			Type:        "gauge",
			Attributes:  map[string]string{"reason": reason},
			Value:       float64(n),
			Description: "1 when a configured license was rejected and the agent degraded to free tier (by reason: validation_failed, binding_mismatch, expired_no_grace); 0 once a license validates. Absent when no license is configured.",
		})
	}

	// HTTP MetricCache drop counters — same emitted-only-when-touched
	// shape as senhub.agent.otlp.dropped above. Today the only reason
	// is `http_cache_cap` (cardinality cap on the shared cache, #281).
	for reason, n := range agentstate.GetHTTPCacheDroppedByReason() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.cache.dropped",
			Unit:        "{datapoint}",
			Type:        "counter",
			Attributes:  map[string]string{"reason": reason},
			Value:       float64(n),
			Description: "Cumulative count of datapoints refused by the shared HTTP metric cache, by reason (http_cache_cap, …).",
		})
	}

	// Push-buffer drop counters (senhub cloud / PRTG push, #267) —
	// same emitted-only-when-touched shape as the cache counter above.
	for strategy, n := range agentstate.GetPushBufferDropped() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.push.buffer.dropped",
			Unit:        "{datapoint}",
			Type:        "counter",
			Attributes:  map[string]string{"strategy": strategy},
			Value:       float64(n),
			Description: "Cumulative count of oldest datapoints dropped by a bounded push buffer at its cap, by strategy.",
		})
	}

	// OTLP receiver ingest counters — items accepted per signal
	// (metrics=emitted internal datapoints after family expansion,
	// logs=records, traces=spans). Emitted only once the receiver has
	// taken traffic on a signal.
	for signal, n := range agentstate.GetOTLPReceiverIngestedBySignal() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp_receiver.ingested",
			Unit:        "{item}",
			Type:        "counter",
			Attributes:  map[string]string{"signal": signal},
			Value:       float64(n),
			Description: "Cumulative count of items accepted by the OTLP receiver, by signal (metrics: emitted internal datapoints after family expansion, e.g. a summary expands to count/sum/quantile points; logs: records; traces: spans).",
		})
	}
	// OTLP receiver drop counters — items the receiver discarded, by signal
	// and reason (no_sink: logs/traces with no export strategy to relay to;
	// unmapped: a metric with an unrecognized/unset data type).
	for key, n := range agentstate.GetOTLPReceiverDroppedBySignal() {
		records = append(records, otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp_receiver.dropped",
			Unit:        "{item}",
			Type:        "counter",
			Attributes:  map[string]string{"signal": key.Signal, "reason": key.Reason},
			Value:       float64(n),
			Description: "Cumulative count of items the OTLP receiver discarded, by signal and reason (no_sink, unmapped).",
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
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.logs.queue.records",
			Unit:        "{record}",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPLogsQueueRecords()),
			Description: "Event-log records currently held in the on-disk dead-letter queue (awaiting replay). 0 when the backend is healthy or persistence is disabled.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.logs.queue.bytes",
			Unit:        "By",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPLogsQueueBytes()),
			Description: "Bytes currently held by the on-disk logs dead-letter queue.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.logs.queued",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPLogsQueuedTotal()),
			Description: "Cumulative event-log records written to the dead-letter queue after a failed export.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.logs.replayed",
			Unit:        "{record}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPLogsReplayedTotal()),
			Description: "Cumulative event-log records re-emitted from the dead-letter queue at boot or on backend recovery.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.active_endpoint_index",
			Unit:        "1",
			Type:        "gauge",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPActiveEndpointIndex()),
			Description: "Index of the OTLP endpoint currently serving exports: 0 = primary, >0 = a configured fallback (endpoint failover, #217). Always 0 when no fallback_endpoints are configured.",
		},
		otelmapper.OtelRecord{
			Name:        "senhub.agent.otlp.endpoint_switches",
			Unit:        "{switch}",
			Type:        "counter",
			Attributes:  map[string]string{},
			Value:       float64(agentstate.GetOTLPEndpointSwitchesTotal()),
			Description: "Cumulative endpoint failover switches (the active OTLP endpoint changed). Rising means the primary is flapping.",
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
