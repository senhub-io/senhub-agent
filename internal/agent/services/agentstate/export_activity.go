package agentstate

import (
	"sync"
	"time"
)

// Export activity is what the outputs page shows per strategy: when the
// last delivery succeeded, when the last one failed and why. The
// counters next door say how often; this says when, which is what an
// operator reads to decide whether the sink is alive right now.

// ExportActivity is the per-strategy delivery snapshot.
type ExportActivity struct {
	LastSuccess time.Time
	LastFailure time.Time
	LastError   string
	Successes   uint64
	Failures    uint64
}

var exportActivity = struct {
	mu sync.Mutex
	m  map[string]*ExportActivity
}{m: map[string]*ExportActivity{}}

func activityFor(strategy string) *ExportActivity {
	if strategy == "" {
		strategy = "unknown"
	}
	a, ok := exportActivity.m[strategy]
	if !ok {
		a = &ExportActivity{}
		exportActivity.m[strategy] = a
	}
	return a
}

// RecordExportSuccess notes one delivered batch. The first success after
// a failure is a transition worth an event; repeated successes are not.
func RecordExportSuccess(strategy string) {
	exportActivity.mu.Lock()
	a := activityFor(strategy)
	recovered := a.LastFailure.After(a.LastSuccess) && !a.LastFailure.IsZero()
	a.LastSuccess = time.Now()
	a.Successes++
	exportActivity.mu.Unlock()
	if recovered {
		RecordEvent(EventInfo, EventKindOutput, strategy, "export recovered")
	}
}

// RecordExportFailure notes one failed delivery with its (already
// redacted) reason. The first failure after a success is an event.
func RecordExportFailure(strategy, reason string) {
	exportActivity.mu.Lock()
	a := activityFor(strategy)
	first := !a.LastSuccess.Before(a.LastFailure) || a.Failures == 0
	a.LastFailure = time.Now()
	a.LastError = reason
	a.Failures++
	exportActivity.mu.Unlock()
	if first {
		RecordEvent(EventError, EventKindOutput, strategy, "export failed: "+reason)
	}
}

// GetExportActivity returns a copy of the snapshot for one strategy; the
// zero value when it never exported.
func GetExportActivity(strategy string) ExportActivity {
	exportActivity.mu.Lock()
	defer exportActivity.mu.Unlock()
	if a, ok := exportActivity.m[strategy]; ok {
		return *a
	}
	return ExportActivity{}
}

// ResetExportActivityForTest clears the snapshots.
func ResetExportActivityForTest() {
	exportActivity.mu.Lock()
	exportActivity.m = map[string]*ExportActivity{}
	exportActivity.mu.Unlock()
}
