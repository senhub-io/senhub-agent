package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// A poll in healthy operation is not an event: PRTG and Nagios polled
// every minute wrote an Info line each time, drowning the log.
func TestPollRequestsLogBelowInfo(t *testing.T) {
	const key = "request-log-key"
	base := createIntegrationTestConfig()
	agentConfig := configuration.NewAgentConfiguration(key, "http://test-server.com", base.Logger)
	params := map[string]interface{}{"endpoints": []interface{}{"prtg", "nagios"}}
	strategy := NewHTTPSyncStrategy(agentConfig, params, base.Logger).(*HTTPSyncStrategy)
	if err := strategy.AddDataPoints(probePoints("cpu", map[string]float64{"cpu_usage_total": 12})); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	captured := &logger.ModuleLogger{Logger: &zl}
	strategy.apiManager.logger = captured
	strategy.nagiosManager.logger = captured

	for _, path := range []string{
		"/api/" + key + "/prtg/metrics/cpu",
		"/api/" + key + "/prtg/probes",
		"/api/" + key + "/nagios/metrics/cpu",
		"/api/" + key + "/nagios/metrics",
		"/api/" + key + "/nagios/checks",
	} {
		rec := httptest.NewRecorder()
		strategy.setupRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code >= 500 {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
	}
	if buf.Len() != 0 {
		t.Errorf("a poll logged at Info or above:\n%s", buf.String())
	}
}
