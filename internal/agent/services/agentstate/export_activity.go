package agentstate

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// printable drops the control characters a receiver's binary error body
// can carry (a protobuf status echoed back), so the reason reads as text
// on the console and in the journal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
}

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
	reason = printable(reason)
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

// PruneExportActivity drops the activity of strategies that are no
// longer configured. Without it, deleting a failing output would leave
// the console header saying "output failing" until the agent restarts.
func PruneExportActivity(configured []string) {
	keep := make(map[string]bool, len(configured))
	for _, name := range configured {
		keep[name] = true
	}
	exportActivity.mu.Lock()
	for key := range exportActivity.m {
		if !keep[strings.SplitN(key, "/", 2)[0]] {
			delete(exportActivity.m, key)
		}
	}
	exportActivity.mu.Unlock()
}

// FailingExports lists the strategies whose last delivery failed after
// their last success, sorted. A signal recorded as "<strategy>/<signal>"
// counts for its strategy.
func FailingExports() []string {
	exportActivity.mu.Lock()
	defer exportActivity.mu.Unlock()
	seen := map[string]bool{}
	for name, a := range exportActivity.m {
		if !a.LastFailure.IsZero() && a.LastFailure.After(a.LastSuccess) {
			seen[strings.SplitN(name, "/", 2)[0]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GetExportActivities returns the snapshots recorded for a strategy and
// its signals ("<strategy>" and "<strategy>/<signal>"), keyed as stored.
func GetExportActivities(strategy string) map[string]ExportActivity {
	exportActivity.mu.Lock()
	defer exportActivity.mu.Unlock()
	out := map[string]ExportActivity{}
	for name, a := range exportActivity.m {
		if name == strategy || strings.HasPrefix(name, strategy+"/") {
			out[name] = *a
		}
	}
	return out
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
