package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// newMixedCaseHarness wires the minimal strategy surface the PRTG GET
// handler touches: cache, transformer registry, format converter and
// metrics processor, on top of the auth-only manager from
// createTestAPIManager.
func newMixedCaseHarness(t *testing.T, agentKey string) (*APIManager, *MetricCache, *transformers.TransformerRegistry) {
	t.Helper()

	agentConfig := newMockConfigWithLicense(agentKey, "")
	apiManager := createTestAPIManager(agentConfig)

	baseLogger := createTestLoggerForAPI()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test.prtg.mixedcase")
	registry := transformers.NewTransformerRegistry(baseLogger)
	cache := NewMetricCache(time.Minute, moduleLogger)

	strategy := apiManager.strategy
	strategy.cache = cache
	strategy.transformerRegistry = registry
	strategy.formatConverter = NewFormatConverter(registry, moduleLogger, cache)
	strategy.metricsProcessor = NewMetricsProcessor(cache, strategy.formatConverter, nil, moduleLogger)

	return apiManager, cache, registry
}

// TestHandlePRTGMetricsGET_MixedCaseProbeName is the regression test for
// #625 (tracked by #647): a probe instance configured with an uppercase
// letter in its name ("CPU-Main") returned an EMPTY PRTG result because
// the cache probe index was keyed on the verbatim name while the GET
// handler looked it up case-folded. The fix (5585d408) keys and reads the
// index case-folded; this test fails if either side regresses.
func TestHandlePRTGMetricsGET_MixedCaseProbeName(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, cache, registry := newMixedCaseHarness(t, agentKey)

	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{{
		Name:      "cpu_usage_total",
		Timestamp: time.Now(),
		Value:     42,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "CPU-Main"},
			{Key: "probe_type", Value: "cpu"},
		},
	}}, registry)

	// Whatever casing the client uses in the GET path, the probe must
	// resolve to the same cache entry.
	for _, probeInPath := range []string{"CPU-Main", "cpu-main", "CPU-MAIN"} {
		t.Run(probeInPath, func(t *testing.T) {
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/api/%s/prtg/metrics/%s", agentKey, probeInPath), nil)
			req = mux.SetURLVars(req, map[string]string{
				"agentkey": agentKey,
				"probe":    probeInPath,
			})
			w := httptest.NewRecorder()

			apiManager.HandlePRTGMetricsGET(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var response PRTGResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode PRTG response: %v", err)
			}
			if len(response.PRTG.Result) == 0 {
				t.Fatalf("PRTG result is empty for probe path %q — mixed-case "+
					"probe name no longer resolves its cache entry (#625 regression)", probeInPath)
			}
		})
	}
}
