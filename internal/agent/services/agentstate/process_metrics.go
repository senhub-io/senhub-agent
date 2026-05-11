package agentstate

import (
	"runtime"
	"runtime/metrics"
)

// ProcessSnapshot is a frozen-in-time view of the agent process's
// own resource usage. Populated by GetProcessSnapshot at scrape time
// and emitted as senhub.agent.process.* metrics by the Prometheus
// exposition bridge.
//
// Values come from two sources:
//   - Go runtime/metrics + runtime package (cross-OS, cheap to read)
//   - OS-specific helpers (build-tagged) for RSS and open FDs that
//     Go cannot observe from its own runtime.
//
// Designed to mirror the canonical self-monitoring metrics shipped
// by other Go observability agents (Grafana Alloy, Prometheus
// itself, otelcol). Operators recognize the names instantly.
type ProcessSnapshot struct {
	// CPUSecondsTotal is the cumulative CPU time the process has
	// consumed since startup, in seconds. Cross-OS via runtime/metrics
	// `/cpu/classes/total:cpu-seconds` which sums user + system time.
	// Always non-decreasing — a Prometheus counter.
	CPUSecondsTotal float64

	// ResidentMemoryBytes is the OS-reported resident set size for
	// the process. Linux: `VmRSS` from /proc/self/status. Windows:
	// `WorkingSetSize` from GetProcessMemoryInfo. Other OSes return 0.
	ResidentMemoryBytes uint64

	// HeapBytes is the Go runtime's view of currently-allocated heap
	// memory (objects only — excludes spans, stack, GC metadata).
	// Useful for spotting heap leaks: if HeapBytes grows monotonically
	// while ResidentMemoryBytes stays flat-ish, the issue is in Go
	// alloc patterns; both growing together points to OS-side bloat.
	HeapBytes uint64

	// Goroutines is `runtime.NumGoroutine()`. A continuously-growing
	// value over hours typically indicates a goroutine leak — the
	// canonical alert pattern Grafana Alloy / Prometheus expose.
	Goroutines int

	// GCCyclesTotal is the cumulative number of GC cycles the
	// runtime has executed. From runtime/metrics
	// `/gc/cycles/total:gc-cycles`. Used to compute GC rate per
	// second via rate() at scrape time.
	GCCyclesTotal uint64

	// OpenFDs is the count of open file descriptors / handles.
	// Linux: entries under /proc/self/fd. Windows:
	// GetProcessHandleCount. Other OSes return 0.
	//
	// A monotonically growing value with no expected workload
	// reason is a classic fd leak — easy to alert on.
	OpenFDs int
}

// runtime/metrics sample slots, allocated once at package init to
// avoid heap allocation on every scrape. The package documents that
// the Sample slice can be reused as long as Names are unchanged.
var processMetricSamples = []metrics.Sample{
	{Name: "/cpu/classes/total:cpu-seconds"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/gc/cycles/total:gc-cycles"},
}

// GetProcessSnapshot captures the current process resource usage.
// Safe to call concurrently — runtime/metrics.Read and the OS
// helpers are thread-safe individually, and we don't mutate any
// shared state.
func GetProcessSnapshot() ProcessSnapshot {
	metrics.Read(processMetricSamples)

	snap := ProcessSnapshot{
		Goroutines:          runtime.NumGoroutine(),
		ResidentMemoryBytes: getResidentMemory(),
		OpenFDs:             getOpenFDs(),
	}

	// runtime/metrics Sample values are tagged by Kind; we read the
	// right field per known metric Name. If a metric is unsupported
	// on this Go version (KindBad), the field stays at its zero
	// value — graceful degradation.
	for _, s := range processMetricSamples {
		switch s.Name {
		case "/cpu/classes/total:cpu-seconds":
			if s.Value.Kind() == metrics.KindFloat64 {
				snap.CPUSecondsTotal = s.Value.Float64()
			}
		case "/memory/classes/heap/objects:bytes":
			if s.Value.Kind() == metrics.KindUint64 {
				snap.HeapBytes = s.Value.Uint64()
			}
		case "/gc/cycles/total:gc-cycles":
			if s.Value.Kind() == metrics.KindUint64 {
				snap.GCCyclesTotal = s.Value.Uint64()
			}
		}
	}

	return snap
}
