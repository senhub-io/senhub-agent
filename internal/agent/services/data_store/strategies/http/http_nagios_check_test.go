package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func newNagiosCheckTestServer(t *testing.T, cpuUsage float64) (http.Handler, string) {
	t.Helper()
	const key = "nagios-check-key"
	base := createIntegrationTestConfig()
	agentConfig := configuration.NewAgentConfiguration(key, "http://test-server.com", base.Logger)
	params := map[string]interface{}{"endpoints": []interface{}{"nagios"}}
	strategy := NewHTTPSyncStrategy(agentConfig, params, base.Logger).(*HTTPSyncStrategy)

	now := time.Now()
	cpu := []tags.Tag{{Key: "probe_name", Value: "cpu"}, {Key: "probe_type", Value: "cpu"}}
	if err := strategy.AddDataPoints([]datapoint.DataPoint{
		{Name: "cpu_usage_total", Value: cpuUsage, Timestamp: now, Tags: cpu},
		{Name: "cpu_system", Value: float64(5), Timestamp: now, Tags: cpu},
		{Name: "cpu_user", Value: float64(10), Timestamp: now, Tags: cpu},
	}); err != nil {
		t.Fatal(err)
	}
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
