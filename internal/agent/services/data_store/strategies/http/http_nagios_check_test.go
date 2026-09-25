package http

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func newNagiosTestStrategy(t *testing.T, points []datapoint.DataPoint) (*HTTPSyncStrategy, string) {
	t.Helper()
	const key = "nagios-check-key"
	base := createIntegrationTestConfig()
	agentConfig := configuration.NewAgentConfiguration(key, "http://test-server.com", base.Logger)
	params := map[string]interface{}{"endpoints": []interface{}{"nagios"}}
	strategy := NewHTTPSyncStrategy(agentConfig, params, base.Logger).(*HTTPSyncStrategy)
	if err := strategy.AddDataPoints(points); err != nil {
		t.Fatal(err)
	}
	return strategy, key
}

func probePoints(probe string, values map[string]float64) []datapoint.DataPoint {
	now := time.Now()
	probeTags := []tags.Tag{{Key: "probe_name", Value: probe}, {Key: "probe_type", Value: probe}}
	points := make([]datapoint.DataPoint, 0, len(values))
	for name, value := range values {
		points = append(points, datapoint.DataPoint{Name: name, Value: value, Timestamp: now, Tags: probeTags})
	}
	return points
}

func newNagiosCheckTestServer(t *testing.T, cpuUsage float64) (http.Handler, string) {
	t.Helper()
	strategy, key := newNagiosTestStrategy(t, probePoints("cpu", map[string]float64{
		"cpu_usage_total": cpuUsage,
		"cpu_system":      5,
		"cpu_user":        10,
	}))
	return strategy.setupRoutes(), key
}

// A configured check is served in plugin format on its own route, so a
// Nagios command calls it directly; the HTTP status mirrors the
// per-probe endpoint.
func TestNagiosCheckRoute(t *testing.T) {
	cases := []struct {
		name       string
		cpuUsage   float64
		check      string
		wantStatus int
		wantPrefix string
	}{
		{"ok", 12, "cpu_detailed", http.StatusOK, "OK - "},
		{"critical", 97, "cpu_detailed", http.StatusInternalServerError, "CRITICAL - "},
		{"unknown check", 12, "no_such_check", http.StatusNotFound, "UNKNOWN - No Nagios check named no_such_check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, key := newNagiosCheckTestServer(t, tc.cpuUsage)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/"+key+"/nagios/check/"+tc.check, nil))

			body := rec.Body.String()
			if rec.Code != tc.wantStatus || !strings.HasPrefix(body, tc.wantPrefix) {
				t.Fatalf("got %d %q, want %d with prefix %q", rec.Code, body, tc.wantStatus, tc.wantPrefix)
			}
			if tc.wantStatus != http.StatusNotFound && !strings.Contains(body, "| cpu_usage_total=") {
				t.Errorf("perfdata missing the check's metric: %q", body)
			}
		})
	}
}

// A metric is read as a state only when its definition names its values
// through a lookup. A count is held against its thresholds whatever its
// name or its value, and a state takes the severity its lookup gives.
func TestNagiosStateModeFollowsTheDefinition(t *testing.T) {
	strategy, _ := newNagiosTestStrategy(t, probePoints("veeam", map[string]float64{
		"veeam_jobs_failed": 2,
		"veeam_job_status":  1,
	}))
	metrics := strategy.cache.GetProbeMetrics("veeam")

	count := strategy.metricsProcessor.ProcessNagiosMetric(
		NagiosMetric{Channel: "veeam_jobs_failed", Warning: "5", Critical: "10"}, metrics, NagiosOverrides{})
	if count.Status != 0 {
		t.Errorf("2 failed jobs against warning 5: got status %d (%s), want OK", count.Status, count.Message)
	}

	state := strategy.metricsProcessor.ProcessNagiosMetric(
		NagiosMetric{Channel: "veeam_job_status", Warning: "2", Critical: "3"}, metrics, NagiosOverrides{})
	if state.Status != 0 || !strings.Contains(strings.ToLower(state.Message), "success") {
		t.Errorf("job status 1 is Success in its lookup: got status %d (%s)", state.Status, state.Message)
	}
}

// A threshold is the last acceptable value, as a Nagios plugin reads
// it: the shipped Veeam checks set warning "0" on failure counts, which
// only makes sense if zero failures is OK.
func TestNagiosThresholdIsLastAcceptableValue(t *testing.T) {
	p := NewMetricsProcessor(nil, nil, nil, newTestLogger())
	cases := []struct {
		value, warning, critical string
		invert                   bool
		want                     int
	}{
		{"0", "0", "0", false, 0},
		{"1", "0", "0", false, 2},
		{"1", "0", "", false, 1},
		{"80", "80", "90", false, 0},
		{"80.5", "80", "90", false, 1},
		{"90.5", "80", "90", false, 2},
		{"20", "20", "10", true, 0},
		{"19", "20", "10", true, 1},
		{"9", "20", "10", true, 2},
	}
	for _, tc := range cases {
		value, _ := strconv.ParseFloat(tc.value, 64)
		if got := p.evaluateThreshold(value, tc.warning, tc.critical, tc.invert); got != tc.want {
			t.Errorf("value %s warning %q critical %q invert %v: got %d, want %d", tc.value, tc.warning, tc.critical, tc.invert, got, tc.want)
		}
	}
}
