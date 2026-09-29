package docsmetrics

import (
	"fmt"
	"os"
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

// TestProbePagesCarryTheirMetricReference keeps the generated reference equal
// to the definitions. Measured when this was written: 303 of 1314 metrics were
// named nowhere on their page, so a page could describe a third of a probe and
// read as complete.
func TestProbePagesCarryTheirMetricReference(t *testing.T) {
	res, err := Sync(docsDir(t), os.Getenv("UPDATE_DOCS") == "1")
	if err != nil {
		t.Fatalf("syncing the metric references: %v", err)
	}
	for _, p := range res.NoPage {
		t.Errorf("probe %s has a definition but no documentation page", p)
	}
	if len(res.Stale) > 0 {
		t.Errorf("%d pages no longer match their definition; run `make docs-metrics`:\n  %s",
			len(res.Stale), strings.Join(res.Stale, "\n  "))
	}
}
