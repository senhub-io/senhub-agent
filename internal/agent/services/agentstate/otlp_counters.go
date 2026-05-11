package agentstate

import "sync/atomic"

// OTLP push counters live here (not in the otlp strategy package) so
// the Prometheus exposition bridge can read them without creating an
// import cycle between the http strategy and the otlp strategy.
//
// All counters are monotonic uint64s incremented via atomic ops on
// the hot path (every push tick / every log record emitted). Reads
// are non-blocking via Load — safe from any goroutine.

var (
	otlpMetricsPushed atomic.Uint64
	otlpLogsPushed    atomic.Uint64
	otlpExportErrors  atomic.Uint64
)

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
}
