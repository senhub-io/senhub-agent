package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

// /info/probes keeps "probes" a list of names, which the web console
// reads, and carries each probe's state in "details", including a probe
// that runs but holds no metric because its cycle fails.
func TestInfoProbesCarriesStateNextToTheNames(t *testing.T) {
	const key = "info-probes-key"
	base := createIntegrationTestConfig()
	agentConfig := configuration.NewAgentConfiguration(key, "http://test-server.com", base.Logger)
	params := map[string]interface{}{"endpoints": []interface{}{"prtg"}}
	strategy := NewHTTPSyncStrategy(agentConfig, params, base.Logger).(*HTTPSyncStrategy)
	if err := strategy.AddDataPoints(probePoints("cpu", map[string]float64{"cpu_usage_total": 12})); err != nil {
		t.Fatal(err)
	}

	agentstate.SetActiveProbes([]string{"id-cpu", "id-ft"})
	agentstate.SetActiveProbeNames(map[string]string{"cpu": "id-cpu", "filetail_a": "id-ft"})
	agentstate.RecordProbeHealth("id-cpu", true)
	agentstate.RecordProbeHealth("id-ft", false)
	agentstate.RecordProbeError("id-ft", "cannot be read: no such file")
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetActiveProbeNames(nil)
	})

	rec := httptest.NewRecorder()
	strategy.setupRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/"+key+"/info/probes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Probes  []string      `json:"probes"`
		Details []ProbeDetail `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("payload: %v\n%s", err, rec.Body.String())
	}
	if len(got.Probes) != 2 || got.Probes[0] != "cpu" || got.Probes[1] != "filetail_a" {
		t.Fatalf("probes = %v", got.Probes)
	}
	byName := map[string]ProbeDetail{}
	for _, d := range got.Details {
		byName[d.Name] = d
	}
	if d := byName["cpu"]; d.MetricsCount == 0 || d.Health != "ok" || d.LastUpdate == "" {
		t.Errorf("cpu = %+v", d)
	}
	if d := byName["filetail_a"]; d.Health != "failed" || d.LastError != "cannot be read: no such file" || d.MetricsCount != 0 {
		t.Errorf("filetail_a = %+v", d)
	}
}
