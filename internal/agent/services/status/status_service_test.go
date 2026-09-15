package status

import (
	"context"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestService(t *testing.T) *StatusService {
	t.Helper()
	return NewStatusService(logger.NewLogger(&cliArgs.ParsedArgs{}), "0.5.5", "abcdef1234567890")
}

// fakeCache is a CacheStatisticsProvider under the test's control, so
// the status calculations can be exercised without a running HTTP
// strategy and a populated metric cache.
type fakeCache struct {
	probes  map[string]ProbeStatistics
	entries int
}

func (f *fakeCache) GetProbeStatistics() map[string]ProbeStatistics { return f.probes }
func (f *fakeCache) GetTotalEntries() int                           { return f.entries }
func (f *fakeCache) GetCacheInfo() CacheInfo {
	return CacheInfo{TotalEntries: f.entries, RetentionMinutes: 5}
}
func (f *fakeCache) GetHealthMetrics() map[string]interface{} { return nil }

// TestCalculateProbeStatuses pins how a probe's raw statistics become
// the status an operator reads. The precedence matters: a probe that is
// active AND carries an error reads "error", because "active" would tell
// an operator the probe is fine when its last cycle failed.
func TestCalculateProbeStatuses(t *testing.T) {
	s := newTestService(t)
	s.SetCacheProvider(&fakeCache{probes: map[string]ProbeStatistics{
		"cpu":   {Name: "cpu", IsActive: true, MetricsCount: 12},
		"mysql": {Name: "mysql", IsActive: false},
		"redis": {Name: "redis", IsActive: true, LastError: "dial tcp: connection refused"},
	}})

	byName := map[string]ProbeStatus{}
	for _, p := range s.calculateProbeStatuses() {
		byName[p.Name] = p
	}

	if len(byName) != 3 {
		t.Fatalf("got %d probe statuses, want 3", len(byName))
	}
	if got := byName["cpu"].Status; got != "active" {
		t.Errorf("cpu status = %q, want active", got)
	}
	if got := byName["cpu"].MetricsCount; got != 12 {
		t.Errorf("cpu metrics count = %d, want 12", got)
	}
	if got := byName["mysql"].Status; got != "inactive" {
		t.Errorf("mysql status = %q, want inactive", got)
	}
	if got := byName["redis"].Status; got != "error" {
		t.Errorf("redis status = %q, want error — an active probe whose last cycle failed is not fine", got)
	}
	if got := byName["redis"].LastError; got == "" {
		t.Error("redis last error was dropped; an operator needs the reason, not just the state")
	}
}

// TestCalculateProbeStatusesWithoutCacheProvider covers the boot window
// and the `agent status` path where no HTTP strategy is configured: the
// service must report an empty list rather than dereference a nil
// provider.
func TestCalculateProbeStatusesWithoutCacheProvider(t *testing.T) {
	s := newTestService(t)
	if got := s.calculateProbeStatuses(); len(got) != 0 {
		t.Errorf("got %d probe statuses with no cache provider, want none", len(got))
	}
}

// TestCalculateHealthInfo pins the health ladder, including its sharp
// edge: the "unhealthy" threshold is `errorCount >= len(probes)/2` with
// INTEGER division, so with three probes one failure is already
// unhealthy (3/2 == 1). That is deliberate for a monitoring agent — a
// third of collection being down is not "degraded" — but it is not what
// "half" reads like, so it is pinned here rather than left to be
// rediscovered.
func TestCalculateHealthInfo(t *testing.T) {
	cases := []struct {
		name        string
		probes      map[string]ProbeStatistics
		wantStatus  string
		wantMessage string
	}{
		{
			name: "every probe collecting",
			probes: map[string]ProbeStatistics{
				"cpu": {Name: "cpu", IsActive: true},
				"mem": {Name: "mem", IsActive: true},
			},
			wantStatus: "healthy",
		},
		{
			name: "one error in four is degraded",
			probes: map[string]ProbeStatistics{
				"cpu":   {Name: "cpu", IsActive: true},
				"mem":   {Name: "mem", IsActive: true},
				"disk":  {Name: "disk", IsActive: true},
				"mysql": {Name: "mysql", IsActive: true, LastError: "boom"},
			},
			wantStatus:  "degraded",
			wantMessage: "1 probe(s) have errors",
		},
		{
			name: "one error in three is already unhealthy (integer division)",
			probes: map[string]ProbeStatistics{
				"cpu":   {Name: "cpu", IsActive: true},
				"mem":   {Name: "mem", IsActive: true},
				"mysql": {Name: "mysql", IsActive: true, LastError: "boom"},
			},
			wantStatus:  "unhealthy",
			wantMessage: "1 of 3 probes have errors",
		},
		{
			name: "everything failing is unhealthy",
			probes: map[string]ProbeStatistics{
				"cpu":   {Name: "cpu", IsActive: true, LastError: "boom"},
				"mysql": {Name: "mysql", IsActive: true, LastError: "boom"},
			},
			wantStatus:  "unhealthy",
			wantMessage: "2 of 2 probes have errors",
		},
		{
			name:       "no probes configured is not a failure",
			probes:     map[string]ProbeStatistics{},
			wantStatus: "healthy",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestService(t)
			s.SetCacheProvider(&fakeCache{probes: c.probes})

			got := s.calculateHealthInfo()
			if got.Status != c.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, c.wantStatus)
			}
			if c.wantMessage != "" && got.Message != c.wantMessage {
				t.Errorf("message = %q, want %q", got.Message, c.wantMessage)
			}
			if got.Timestamp.IsZero() {
				t.Error("health info carries no timestamp")
			}
		})
	}
}

// TestFormatUptime pins the ladder an operator reads on `agent status`.
// The units are cumulative-with-remainder, not absolute: 25 hours is
// "1d 1h", not "25h".
func TestFormatUptime(t *testing.T) {
	s := newTestService(t)

	cases := []struct {
		uptime time.Duration
		want   string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m 30s"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h 2m 3s"},
		{25*time.Hour + time.Minute, "1d 1h 1m 0s"},
		{0, "0s"},
	}

	for _, c := range cases {
		if got := s.formatUptime(c.uptime); got != c.want {
			t.Errorf("formatUptime(%s) = %q, want %q", c.uptime, got, c.want)
		}
	}
}

// TestFormatVersionAndCommit covers the two fallbacks a build without
// ldflags injection hits: an unlabelled binary must read "unknown"
// rather than an empty field an operator cannot interpret.
func TestFormatVersionAndCommit(t *testing.T) {
	s := newTestService(t)

	if got := s.formatVersion(""); got != "unknown" {
		t.Errorf("formatVersion(\"\") = %q, want unknown", got)
	}
	if got := s.formatVersion("0.5.5"); got != "0.5.5" {
		t.Errorf("formatVersion = %q, want 0.5.5", got)
	}
	if got := s.formatCommitHash(""); got != "unknown" {
		t.Errorf("formatCommitHash(\"\") = %q, want unknown", got)
	}
	if got := s.formatCommitHash("abcdef1234567890"); got != "abcdef12" {
		t.Errorf("formatCommitHash truncation = %q, want abcdef12", got)
	}
	if got := s.formatCommitHash("abc"); got != "abc" {
		t.Errorf("formatCommitHash(short) = %q, want abc unchanged", got)
	}
}

// TestGetSystemStatusIsSelfConsistent exercises the whole assembly the
// `agent status` command and the /info endpoint both render, so a field
// that stops being populated is caught here rather than by an operator
// reading a blank line.
func TestGetSystemStatusIsSelfConsistent(t *testing.T) {
	s := newTestService(t)
	s.SetCacheProvider(&fakeCache{
		probes:  map[string]ProbeStatistics{"cpu": {Name: "cpu", IsActive: true, MetricsCount: 3}},
		entries: 42,
	})

	got := s.GetSystemStatus()

	if got.Health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", got.Health.Status)
	}
	if len(got.Probes) != 1 {
		t.Errorf("probes = %d, want 1", len(got.Probes))
	}
	if got.Agent.Version != "0.5.5" {
		t.Errorf("agent version = %q, want 0.5.5", got.Agent.Version)
	}
	if got.Agent.Commit != "abcdef12" {
		t.Errorf("agent commit = %q, want the truncated hash", got.Agent.Commit)
	}
	if got.Agent.GoVersion == "" || got.Agent.OS == "" || got.Agent.Arch == "" {
		t.Errorf("build identity incomplete: %+v", got.Agent)
	}
	if got.Performance.Uptime == "" || !strings.HasSuffix(got.Performance.Uptime, "s") {
		t.Errorf("uptime = %q, want a formatted duration", got.Performance.Uptime)
	}
	if got.Performance.CacheEntries != 42 {
		t.Errorf("cache entries = %d, want 42", got.Performance.CacheEntries)
	}
	if got.Performance.Goroutines <= 0 {
		t.Errorf("goroutines = %d, want a positive count", got.Performance.Goroutines)
	}
	if got.Connection.Source != "local_config" {
		t.Errorf("connection source = %q, want local_config", got.Connection.Source)
	}
}

// TestShutdownIsQuiet documents that the status service holds nothing
// that needs draining: it reads state others own. A future version that
// grows a background loop has to change this test, which is the point.
func TestShutdownIsQuiet(t *testing.T) {
	s := newTestService(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
