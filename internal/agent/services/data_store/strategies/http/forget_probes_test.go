package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
)

// A probe whose configuration is removed stops running, and the data
// store tells the output so. Until then the PRTG probe list, the Nagios
// checks and the console kept listing it for as long as its live
// window: an hour and a half for an hourly probe (#997).
func TestForgetProbesDropsARemovedProbeFromThePRTGList(t *testing.T) {
	const key = "forget-probes-key"
	base := createIntegrationTestConfig()
	agentConfig := configuration.NewAgentConfiguration(key, "http://test-server.com", base.Logger)
	params := map[string]interface{}{"endpoints": []interface{}{"prtg", "nagios"}}
	strategy := NewHTTPSyncStrategy(agentConfig, params, base.Logger).(*HTTPSyncStrategy)
	strategy.NoteProbeCadence("Removed_Probe", time.Hour)
	for _, probe := range []string{"Removed_Probe", "cpu"} {
		if err := strategy.AddDataPoints(probePoints(probe, map[string]float64{"cpu_usage_total": 12})); err != nil {
			t.Fatal(err)
		}
	}

	strategy.ForgetProbes([]string{"removed_probe"})

	rec := httptest.NewRecorder()
	strategy.setupRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/"+key+"/prtg/probes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var list ProbesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Probes) != 1 || list.Probes[0].Name != "cpu" {
		t.Fatalf("PRTG probe list after the probe was retired: %+v, want only cpu", list.Probes)
	}
	if n := len(strategy.cache.GetProbeMetrics("Removed_Probe")); n != 0 {
		t.Errorf("%d values of the retired probe are still served", n)
	}
	strategy.cache.mu.RLock()
	_, cadenceKept := strategy.cache.cadences["removed_probe"]
	strategy.cache.mu.RUnlock()
	if cadenceKept {
		t.Error("the retired probe's cadence is kept")
	}
}
