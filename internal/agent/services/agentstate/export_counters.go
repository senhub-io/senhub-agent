package agentstate

import "sync"

// Send failures live here, next to the buffer-drop counters, so the
// exposition bridges can read both without importing the data store.
//
// A drop counter alone does not describe an outage: a sink whose
// backlog is still under the cap sheds nothing while failing every
// send, so `push.buffer.dropped` stays flat and the only trace is a log
// line. Counting the failures themselves is what makes "this sink has
// not delivered anything for ten minutes" visible, and the reason label
// is what says whether an operator has to act (#287).

// Send-failure reasons. They mirror the exporterrors taxonomy, because
// that is the taxonomy the export paths already branch on:
//
//   - transport: the far end was unreachable or answered in a way a
//     later attempt may not repeat. The batch was kept.
//   - validation: the far end rejected the payload on its merits. The
//     batch was dropped; resending would be rejected again.
//   - configuration: the request could not be built from the configured
//     parameters. The batch was dropped and stays undeliverable until an
//     operator edits the config.
const (
	ExportFailureTransport     = "transport"
	ExportFailureValidation    = "validation"
	ExportFailureConfiguration = "configuration"
)

type exportFailureKey struct {
	strategy string
	reason   string
}

var exportSendFailed = struct {
	mu sync.Mutex
	m  map[exportFailureKey]uint64
}{m: map[exportFailureKey]uint64{}}

// IncrementExportSendFailed records one failed delivery attempt by the
// named strategy. One increment per attempt, not per datapoint: the
// batch size is already visible through the buffer depth, and counting
// per item would make a single large batch look like an outage.
func IncrementExportSendFailed(strategy, reason string) {
	if strategy == "" {
		strategy = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	exportSendFailed.mu.Lock()
	exportSendFailed.m[exportFailureKey{strategy, reason}]++
	exportSendFailed.mu.Unlock()
}

// ExportSendFailure is one row of the send-failure counter.
type ExportSendFailure struct {
	Strategy string
	Reason   string
	Count    uint64
}

// GetExportSendFailed returns a snapshot of the counter. A strategy that
// never failed has no row — counter semantics: absence means zero.
func GetExportSendFailed() []ExportSendFailure {
	exportSendFailed.mu.Lock()
	defer exportSendFailed.mu.Unlock()
	out := make([]ExportSendFailure, 0, len(exportSendFailed.m))
	for k, v := range exportSendFailed.m {
		out = append(out, ExportSendFailure{Strategy: k.strategy, Reason: k.reason, Count: v})
	}
	return out
}

// ResetExportSendFailedForTest clears the counter. Test-only.
func ResetExportSendFailedForTest() {
	exportSendFailed.mu.Lock()
	exportSendFailed.m = map[exportFailureKey]uint64{}
	exportSendFailed.mu.Unlock()
}
