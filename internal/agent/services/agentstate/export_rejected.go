package agentstate

import "sync"

// Records the consumer refused sit next to the send-failure counters,
// and they are NOT the same event.
//
// A send failure means the batch never landed. A rejection means it
// landed, the far end answered OK, and it kept only part of what it was
// given — an OTLP partial success. From the exporter's point of view the
// export succeeded, so nothing on the failure path moves and the loss is
// invisible from the producing host: the rejection surfaces one hop
// away, in the consumer's own journal. A production fan-out lost six
// entity records per batch for thirty-seven minutes that way (#819).
//
// The counter is per record, not per batch, because the number of
// records refused is the size of the loss, and a batch carrying one bad
// record is a different incident from a batch where everything was
// refused.

type exportRejectedKey struct {
	strategy string
	signal   string
}

var exportRejected = struct {
	mu sync.Mutex
	m  map[exportRejectedKey]uint64
}{m: map[exportRejectedKey]uint64{}}

// IncrementExportRejected records n records refused by the consumer of
// the named strategy, on the named signal ("metrics", "logs", "traces").
// A non-positive n adds nothing: a partial success may carry a message
// with no count, and inventing one would overstate the loss.
func IncrementExportRejected(strategy, signal string, n int64) {
	if n <= 0 {
		return
	}
	if strategy == "" {
		strategy = "unknown"
	}
	if signal == "" {
		signal = "unknown"
	}
	exportRejected.mu.Lock()
	exportRejected.m[exportRejectedKey{strategy, signal}] += uint64(n)
	exportRejected.mu.Unlock()
}

// ExportRejection is one row of the rejection counter.
type ExportRejection struct {
	Strategy string
	Signal   string
	Count    uint64
}

// GetExportRejected returns a snapshot of the counter. A strategy whose
// records were never refused has no row — counter semantics: absence
// means zero.
func GetExportRejected() []ExportRejection {
	exportRejected.mu.Lock()
	defer exportRejected.mu.Unlock()
	out := make([]ExportRejection, 0, len(exportRejected.m))
	for k, v := range exportRejected.m {
		out = append(out, ExportRejection{Strategy: k.strategy, Signal: k.signal, Count: v})
	}
	return out
}

// ResetExportRejectedForTest clears the counter. Test-only.
func ResetExportRejectedForTest() {
	exportRejected.mu.Lock()
	exportRejected.m = map[exportRejectedKey]uint64{}
	exportRejected.mu.Unlock()
}
