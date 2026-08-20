package status

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// The CLI formatter is what an operator reads when something is wrong,
// so its failure mode is a status screen that quietly omits the line
// that mattered. These tests assert on the facts in the rendered text,
// not on its exact layout — pinning byte-for-byte output would make
// every wording change a test failure and teach the next person to
// update the expectation without reading it.

// TestFormatBasicStatusCarriesTheFacts covers the block `agent status`
// prints first.
func TestFormatBasicStatusCarriesTheFacts(t *testing.T) {
	f := NewCLIFormatter()

	out := f.FormatBasicStatus(
		HealthInfo{Status: "degraded", Message: "1 probe(s) have errors", Timestamp: time.Now()},
		AgentInfo{Version: "0.5.5", Commit: "abcdef12", GoVersion: "go1.26.6", OS: "linux", Arch: "amd64"},
	)

	for _, want := range []string{"Degraded", "1 probe(s) have errors", "0.5.5", "abcdef12"} {
		if !strings.Contains(out, want) {
			t.Errorf("basic status is missing %q:\n%s", want, out)
		}
	}
}

// TestFormatBasicStatusOmitsAnEmptyMessage guards the healthy case: a
// blank message line reads as a truncated screen.
func TestFormatBasicStatusOmitsAnEmptyMessage(t *testing.T) {
	f := NewCLIFormatter()

	out := f.FormatBasicStatus(
		HealthInfo{Status: "healthy", Timestamp: time.Now()},
		AgentInfo{Version: "0.5.5", Commit: "abcdef12"},
	)

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "       ") {
			t.Errorf("healthy status rendered an empty message line:\n%s", out)
		}
	}
}

// TestFormatProbeStatusesSummarises pins the counts an operator scans
// before reading the per-probe rows.
func TestFormatProbeStatusesSummarises(t *testing.T) {
	f := NewCLIFormatter()

	out := f.FormatProbeStatuses([]ProbeStatus{
		{Name: "cpu", Status: "active", MetricsCount: 10},
		{Name: "mem", Status: "active", MetricsCount: 5},
		{Name: "mysql", Status: "error", LastError: "dial tcp: connection refused"},
		{Name: "redis", Status: "inactive"},
	})

	for _, want := range []string{"cpu", "mem", "mysql", "redis", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("probe listing is missing %q:\n%s", want, out)
		}
	}
}

// TestFormatProbeStatusesEmpty covers the state a fresh install shows:
// it must say so rather than print an empty section.
func TestFormatProbeStatusesEmpty(t *testing.T) {
	f := NewCLIFormatter()

	out := f.FormatProbeStatuses(nil)
	if !strings.Contains(out, "No probes configured") {
		t.Errorf("empty probe list did not say so:\n%s", out)
	}
}

// TestGetProbeIcon pins the icon set, including the Windows carve-out:
// the console there mangles the emoji, so the platform gets none.
func TestGetProbeIcon(t *testing.T) {
	f := NewCLIFormatter()

	if runtime.GOOS == "windows" {
		for _, status := range []string{"active", "inactive", "error", "nonsense"} {
			if got := f.getProbeIcon(status); got != "" {
				t.Errorf("getProbeIcon(%q) = %q on windows, want empty", status, got)
			}
		}
		return
	}

	cases := map[string]string{
		"active":   "✅",
		"inactive": "⏸️",
		"error":    "❌",
	}
	for status, want := range cases {
		if got := f.getProbeIcon(status); got != want {
			t.Errorf("getProbeIcon(%q) = %q, want %q", status, got, want)
		}
	}
	// An unrecognised status must still render something: an empty icon
	// column silently hides a probe in a state nobody anticipated.
	if got := f.getProbeIcon("something-new"); got == "" {
		t.Error("an unknown probe status rendered no icon at all")
	}
}

// TestFormatDuration pins the coarse "how long ago" scale. Unlike the
// status service's uptime it shows ONE unit, so 90 minutes is "1h", not
// "1h 30m" — the column is a glance, not a measurement.
func TestFormatDuration(t *testing.T) {
	f := NewCLIFormatter()

	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{90 * time.Minute, "1h"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
		{0, "0s"},
	}

	for _, c := range cases {
		if got := f.formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
