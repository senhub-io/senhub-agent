package agentstate

import "sync/atomic"

// SignalContext is the immutable correlation context the DataStore
// publishes for the log and trace rails (#294). It carries the
// operator-configured tags that must be stamped consistently across
// signals so a backend can join metrics, logs and traces of the same
// probe / host / tenant.
//
// It is a deliberately flat, dependency-free struct: agentstate is a
// low-level package imported everywhere, so it must not reach back into
// the configuration types. The DataStore builds a fresh SignalContext on
// every config refresh and calls SetSignalContext; publishers read the
// current snapshot lock-free.
//
// Placement policy stays with the sink, not here: this struct only
// carries the DATA. Per-probe CustomTags become log-record attributes
// (the metric router already applies them to datapoints; logs never went
// through it — #294 closes that gap). GlobalTags describe the whole agent
// and are lifted to the OTel Resource by the sink; they are NOT stamped
// as record attributes to avoid double-emitting them.
type SignalContext struct {
	// CustomTagsByProbe maps a probe instance name (LogRecord.ProducerProbeName)
	// to the operator-configured custom_tags for that probe. Applied to
	// log records as attributes. Nil/empty for probes without custom_tags.
	CustomTagsByProbe map[string]map[string]string

	// GlobalTags are the agent-level tags (tenant / site / region). The
	// sink lifts them to the Resource; kept here so a non-OTLP sink can
	// read the same source of truth instead of re-discovering it.
	GlobalTags map[string]string
}

// signalCtx holds the current SignalContext snapshot. Copy-on-write: the
// DataStore Store()s a fresh struct; readers Load() it and never mutate
// what they read.
var signalCtx atomic.Pointer[SignalContext]

// SetSignalContext installs a new correlation-context snapshot. Called by
// the DataStore on every config refresh. A nil context is treated as
// "no enrichment" by readers.
func SetSignalContext(c *SignalContext) {
	signalCtx.Store(c)
}

// customTagsForProbe returns the operator custom_tags configured for the
// named probe instance, or nil when there is no active context or the
// probe has none. The returned map must not be mutated by callers.
func customTagsForProbe(probeName string) map[string]string {
	if probeName == "" {
		return nil
	}
	c := signalCtx.Load()
	if c == nil {
		return nil
	}
	return c.CustomTagsByProbe[probeName]
}

// resetSignalContextForTest clears the snapshot so package-level state
// does not leak across test cases.
func resetSignalContextForTest() {
	signalCtx.Store(nil)
}
