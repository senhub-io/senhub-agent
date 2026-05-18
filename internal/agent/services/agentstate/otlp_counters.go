package agentstate

import (
	"sync"
	"sync/atomic"
	"time"
)

// OTLP push counters live here (not in the otlp strategy package) so
// the Prometheus exposition bridge can read them without creating an
// import cycle between the http strategy and the otlp strategy.
//
// All counters are monotonic uint64s incremented via atomic ops on
// the hot path (every push tick / every log record emitted). Reads
// are non-blocking via Load — safe from any goroutine.

var (
	otlpMetricsPushed         atomic.Uint64
	otlpLogsPushed            atomic.Uint64
	otlpExportErrors          atomic.Uint64
	otlpStoreSize             atomic.Int64 // last reported gauge
	otlpLastExportDurationNs  atomic.Int64 // duration of the last successful export
	otlpExportDurationCountNs atomic.Uint64
	otlpExportDurationCalls   atomic.Uint64

	otlpCheckpointSize           atomic.Int64  // bytes of the last saved file
	otlpCheckpointLastSaveNanos  atomic.Int64  // unix-nanos of the most recent successful save (0 = never)
	otlpCheckpointRestoredCount  atomic.Int64  // entries restored at boot (0 = no restore happened)
	otlpCheckpointRestoredAtNs   atomic.Int64  // unix-nanos of the boot restore (0 = none)

	otlpSubBatchCount atomic.Int32 // number of sub-batches produced by the last push (1 if single-batch path)
)

// otlpCheckpointErrors is a per-stage counter (read/parse/encode/etc.)
// so operators can pinpoint which step of the save/load cycle is
// failing without parsing logs. Stages are stable enums set by the
// checkpoint package.
var otlpCheckpointErrors = struct {
	mu sync.RWMutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// otlpDropped is a string-keyed counter (one per drop reason). The
// hot path (upsert at cap) acquires the mutex only on drop, so the
// happy path remains lock-free. Reasons are stable enums set by the
// strategy; keep the set small so dashboards can pivot on them.
var otlpDropped = struct {
	mu sync.RWMutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// IncrementOTLPMetricsPushed records `n` metric records successfully
// exported in one batch. Called by the OTLP strategy after the
// exporter returns nil.
func IncrementOTLPMetricsPushed(n int) {
	if n <= 0 {
		return
	}
	otlpMetricsPushed.Add(uint64(n))
}

// IncrementOTLPLogsPushed records one log record emitted by the SDK
// Logger. Called from the OTLP logs pipeline emit path. The SDK
// itself batches downstream — we count records, not batches, to
// match what the OTLP receiver eventually sees.
func IncrementOTLPLogsPushed() {
	otlpLogsPushed.Add(1)
}

// IncrementOTLPExportErrors records one failed export (after retry
// exhaustion). Independent of which signal (metrics or logs) failed —
// the operator alerts on "any export failure". Specific signal-level
// breakdown can come later if real-world ops shows a need.
func IncrementOTLPExportErrors() {
	otlpExportErrors.Add(1)
}

// GetOTLPMetricsPushedTotal / GetOTLPLogsPushedTotal /
// GetOTLPExportErrorsTotal are scrape-time accessors. Read once per
// scrape by the Prometheus bridge.
func GetOTLPMetricsPushedTotal() uint64 { return otlpMetricsPushed.Load() }
func GetOTLPLogsPushedTotal() uint64    { return otlpLogsPushed.Load() }
func GetOTLPExportErrorsTotal() uint64  { return otlpExportErrors.Load() }

// IncrementOTLPDropped records one OTLP datapoint dropped before the
// export call, labelled by reason ("store_cap" today; future reasons
// may include "queue_full", "memory_limit", "size_limit"). Reasons
// surface as the `reason` attribute on the
// `senhub.agent.otlp.dropped` counter — keep them stable and small.
func IncrementOTLPDropped(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	otlpDropped.mu.Lock()
	otlpDropped.m[reason]++
	otlpDropped.mu.Unlock()
}

// GetOTLPDroppedByReason returns a snapshot copy of the per-reason
// drop counters. The map is safe to mutate by the caller.
func GetOTLPDroppedByReason() map[string]uint64 {
	otlpDropped.mu.RLock()
	defer otlpDropped.mu.RUnlock()
	out := make(map[string]uint64, len(otlpDropped.m))
	for k, v := range otlpDropped.m {
		out[k] = v
	}
	return out
}

// RecordOTLPStoreSize is called by the OTLP strategy after every
// snapshot to publish the current cardinality of the strategy-local
// metric store. Surfaces as the `senhub.agent.otlp.store_size` gauge.
func RecordOTLPStoreSize(n int) {
	otlpStoreSize.Store(int64(n))
}

// GetOTLPStoreSize returns the most recently reported store size.
func GetOTLPStoreSize() int64 { return otlpStoreSize.Load() }

// RecordOTLPExportDuration records the duration of one successful
// OTLP metric export. Maintains a simple running mean alongside the
// last-observed value so a single histogram-emit path can pick either.
// (We don't ship a real histogram yet — out of scope for Tier 1 — but
// the average + last is enough for operator alerts of the form
// "export is taking 30 s when it used to take 1 s".)
func RecordOTLPExportDuration(d time.Duration) {
	otlpLastExportDurationNs.Store(int64(d))
	otlpExportDurationCountNs.Add(uint64(d))
	otlpExportDurationCalls.Add(1)
}

// GetOTLPLastExportDuration returns the duration of the most recent
// successful export.
func GetOTLPLastExportDuration() time.Duration {
	return time.Duration(otlpLastExportDurationNs.Load())
}

// GetOTLPMeanExportDuration returns the all-time mean of successful
// export durations. Returns 0 before the first call.
func GetOTLPMeanExportDuration() time.Duration {
	calls := otlpExportDurationCalls.Load()
	if calls == 0 {
		return 0
	}
	total := otlpExportDurationCountNs.Load()
	return time.Duration(total / calls)
}

// IncrementOTLPCheckpointErrors records one failed checkpoint
// operation, attributed by stage ("read", "parse", "version_mismatch",
// "mkdir", "create_tmp", "encode", "fsync", "close", "rename").
// Operators alert on this rising — a stuck checkpoint means the
// agent has lost restart-resilience even though the in-memory store
// keeps working.
func IncrementOTLPCheckpointErrors(stage string) {
	if stage == "" {
		stage = "unknown"
	}
	otlpCheckpointErrors.mu.Lock()
	otlpCheckpointErrors.m[stage]++
	otlpCheckpointErrors.mu.Unlock()
}

// GetOTLPCheckpointErrorsByStage returns a snapshot map of error
// counts per stage. Safe to mutate.
func GetOTLPCheckpointErrorsByStage() map[string]uint64 {
	otlpCheckpointErrors.mu.RLock()
	defer otlpCheckpointErrors.mu.RUnlock()
	out := make(map[string]uint64, len(otlpCheckpointErrors.m))
	for k, v := range otlpCheckpointErrors.m {
		out[k] = v
	}
	return out
}

// RecordOTLPCheckpointSize updates the gauge tracking the size in
// bytes of the most recently saved checkpoint file. Surfaces as
// `senhub.agent.otlp.checkpoint.size_bytes`.
func RecordOTLPCheckpointSize(b int64) {
	if b < 0 {
		b = 0
	}
	otlpCheckpointSize.Store(b)
}

// GetOTLPCheckpointSize returns the last reported file size in bytes.
func GetOTLPCheckpointSize() int64 { return otlpCheckpointSize.Load() }

// RecordOTLPCheckpointLastSave updates the timestamp (unix-nanos) of
// the most recent successful save. Pass 0 to reset.
func RecordOTLPCheckpointLastSave(ns int64) { otlpCheckpointLastSaveNanos.Store(ns) }

// GetOTLPCheckpointLastSaveAge returns the time elapsed since the
// most recent successful save. Returns 0 when no save has occurred
// (e.g. checkpoint disabled).
func GetOTLPCheckpointLastSaveAge() time.Duration {
	ns := otlpCheckpointLastSaveNanos.Load()
	if ns == 0 {
		return 0
	}
	return time.Since(time.Unix(0, ns))
}

// RecordOTLPCheckpointRestored records the number of entries restored
// from a checkpoint at boot. Called once on Start when a checkpoint
// is found and successfully parsed.
func RecordOTLPCheckpointRestored(n int) {
	if n < 0 {
		n = 0
	}
	otlpCheckpointRestoredCount.Store(int64(n))
	otlpCheckpointRestoredAtNs.Store(time.Now().UnixNano())
}

// GetOTLPCheckpointRestoredCount returns the number of entries
// restored at the last agent boot. 0 means no restore happened
// (either no checkpoint file or a fresh start).
func GetOTLPCheckpointRestoredCount() int64 {
	return otlpCheckpointRestoredCount.Load()
}

// RecordOTLPSubBatchCount records the number of sub-batches the last
// push fanned out across. 1 means the single-batch path was taken
// (small cycle or max_concurrent_exports=1); >1 means the parallel
// per-probe split fired. Surfaces as
// `senhub.agent.otlp.parallel.sub_batches` so operators can see when
// the parallel path is active and how it scales with load.
func RecordOTLPSubBatchCount(n int) {
	if n < 1 {
		n = 1
	}
	otlpSubBatchCount.Store(int32(n))
}

// GetOTLPSubBatchCount returns the last reported sub-batch count.
func GetOTLPSubBatchCount() int32 { return otlpSubBatchCount.Load() }

// LogChannelFillRatio returns the highest fill ratio (0..1) across
// all currently-subscribed log channels. Used by the Prometheus
// bridge to expose senhub.agent.otlp.buffer.fill_ratio.
//
// Returns 0 when there are no subscribers — distinguishable from a
// non-zero "everything's full" state because no consumer means no
// production-time backpressure to worry about.
//
// Why max-across-subs rather than sum: the alert pattern is "is any
// consumer falling behind?" — one slow consumer doesn't average out
// with a fast one. Max captures the worst case.
func LogChannelFillRatio() float64 {
	logCh.mu.RLock()
	defer logCh.mu.RUnlock()
	var max float64
	for _, ch := range logCh.subs {
		c := cap(ch)
		if c == 0 {
			continue
		}
		r := float64(len(ch)) / float64(c)
		if r > max {
			max = r
		}
	}
	return max
}

// resetOTLPCountersForTest is the standard reset pattern used by
// other agentstate counters. Not exported; tests in the same
// package call it via the package-private helper.
func resetOTLPCountersForTest() {
	otlpMetricsPushed.Store(0)
	otlpLogsPushed.Store(0)
	otlpExportErrors.Store(0)
	otlpStoreSize.Store(0)
	otlpLastExportDurationNs.Store(0)
	otlpExportDurationCountNs.Store(0)
	otlpExportDurationCalls.Store(0)
	otlpDropped.mu.Lock()
	otlpDropped.m = map[string]uint64{}
	otlpDropped.mu.Unlock()
	otlpCheckpointSize.Store(0)
	otlpCheckpointLastSaveNanos.Store(0)
	otlpCheckpointRestoredCount.Store(0)
	otlpCheckpointRestoredAtNs.Store(0)
	otlpCheckpointErrors.mu.Lock()
	otlpCheckpointErrors.m = map[string]uint64{}
	otlpCheckpointErrors.mu.Unlock()
	otlpSubBatchCount.Store(0)
}
