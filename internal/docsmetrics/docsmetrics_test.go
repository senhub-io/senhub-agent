package docsmetrics

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func docsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no compile-time path for this package")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..",
		"docs", "user-guide", "docs", "probes"))
}

// TestProbePagesNameOnlyMetricsThatExist is the lie detector.
//
// Measured when this was written: 36 pages printed 160 metric names that no
// definition declares — redis.keys.evicted for redis.evicted_keys,
// citrix.sessions.connected for senhub.citrix.sessions.count, and whole
// tables of metrics no probe has ever collected. A reader who builds an
// alert rule on one of those gets an empty series and no way to tell a wrong
// name from a probe that collected nothing.
func TestProbePagesNameOnlyMetricsThatExist(t *testing.T) {
	unknown, err := Audit(docsDir(t))
	if err != nil {
		t.Fatalf("auditing the probe pages: %v", err)
	}
	if len(unknown) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d metric names are printed on a page but declared by no definition:\n", len(unknown))
	page := ""
	for _, u := range unknown {
		if u.Page != page {
			page = u.Page
			fmt.Fprintf(&b, "\n  %s\n", page)
		}
		fmt.Fprintf(&b, "    %s\n", u.Name)
	}
	t.Error(b.String())
}
