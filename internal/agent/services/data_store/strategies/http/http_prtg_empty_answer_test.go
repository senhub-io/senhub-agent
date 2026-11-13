package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// boundFirstDataWait makes waitForFirstData give up at once, so a probe
// without data cannot hold these tests.
func boundFirstDataWait(apiManager *APIManager) {
	apiManager.strategy.firstDataWindow = time.Nanosecond
	apiManager.strategy.firstDataWait = time.Millisecond
}

// addCPUData lands one collectable point for the "cpu-main" probe.
func addCPUData(t *testing.T, cache *MetricCache, registry *transformers.TransformerRegistry) {
	t.Helper()
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{{
		Name:      "cpu_usage_total",
		Timestamp: time.Now(),
		Value:     42,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu-main"},
			{Key: "probe_type", Value: "cpu"},
		},
	}}, registry)
}

// getPRTG runs the GET handler the way the router would.
func getPRTG(t *testing.T, apiManager *APIManager, agentKey, probe string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET",
		fmt.Sprintf("/api/%s/prtg/metrics/%s", agentKey, probe), nil)
	req = mux.SetURLVars(req, map[string]string{
		"agentkey": agentKey,
		"probe":    probe,
	})
	w := httptest.NewRecorder()
	apiManager.HandlePRTGMetricsGET(w, req)
	return w
}

// decodePRTGErrorForm asserts the body is the PRTG error answer (error
// flag, text, no result key) and returns the text.
func decodePRTGErrorForm(t *testing.T, body string) string {
	t.Helper()

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	rawPRTG, ok := top["prtg"]
	if !ok {
		t.Fatalf("response has no prtg key: %s", body)
	}
	var prtg map[string]json.RawMessage
	if err := json.Unmarshal(rawPRTG, &prtg); err != nil {
		t.Fatalf("decode prtg object: %v", err)
	}
	if _, ok := prtg["result"]; ok {
		t.Fatalf("error form carries a result key: %s", body)
	}
	var errorFlag int
	if err := json.Unmarshal(prtg["error"], &errorFlag); err != nil {
		t.Fatalf("decode error flag: %v", err)
	}
	if errorFlag != 1 {
		t.Fatalf("error = %d, want 1", errorFlag)
	}
	var text string
	if err := json.Unmarshal(prtg["text"], &text); err != nil {
		t.Fatalf("decode error text: %v", err)
	}
	return text
}

func TestHandlePRTGMetricsGET_NoDataYetAnswersErrorForm(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, _, _ := newMixedCaseHarness(t, agentKey)
	boundFirstDataWait(apiManager)

	w := getPRTG(t, apiManager, agentKey, "netscaler")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	text := decodePRTGErrorForm(t, w.Body.String())
	if !strings.Contains(text, "netscaler") ||
		!strings.Contains(text, "waiting for the first collection") {
		t.Fatalf("text = %q, want the first-collection message naming the probe", text)
	}
}

func TestHandlePRTGMetricsGET_FilterDropsEveryChannel(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, cache, registry := newMixedCaseHarness(t, agentKey)
	boundFirstDataWait(apiManager)
	addCPUData(t, cache, registry)

	// The state a request filter leaves when it excludes every channel:
	// the probe stays indexed (its first-data signal stays closed) but
	// no series survives to become a channel.
	cache.mu.Lock()
	for tsKey := range cache.probeIndex["cpu-main"] {
		delete(cache.timeSeries, tsKey)
	}
	cache.mu.Unlock()

	w := getPRTG(t, apiManager, agentKey, "cpu-main")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	text := decodePRTGErrorForm(t, w.Body.String())
	if !strings.Contains(text, "no channel of probe cpu-main matches the request filter") {
		t.Fatalf("text = %q, want the filter message naming the probe", text)
	}
}

func TestHandlePRTGMetricsGET_WithDataStillAnswersChannels(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, cache, registry := newMixedCaseHarness(t, agentKey)
	boundFirstDataWait(apiManager)
	addCPUData(t, cache, registry)

	w := getPRTG(t, apiManager, agentKey, "cpu-main")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var prtg map[string]json.RawMessage
	if err := json.Unmarshal(top["prtg"], &prtg); err != nil {
		t.Fatalf("decode prtg object: %v", err)
	}
	if _, ok := prtg["error"]; ok {
		t.Fatalf("channel answer carries an error key: %s", w.Body.String())
	}
	var channels []PRTGChannel
	if err := json.Unmarshal(prtg["result"], &channels); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(channels) == 0 {
		t.Fatalf("result carries no channel: %s", w.Body.String())
	}
}

func TestHandlePRTGMetrics_NoDataYetAnswersErrorForm(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, _, _ := newMixedCaseHarness(t, agentKey)
	boundFirstDataWait(apiManager)

	req := httptest.NewRequest("POST",
		fmt.Sprintf("/api/%s/prtg/metrics", agentKey),
		strings.NewReader(`{"probe":"netscaler","target":"10.0.0.1"}`))
	req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})
	w := httptest.NewRecorder()

	apiManager.HandlePRTGMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	text := decodePRTGErrorForm(t, w.Body.String())
	if !strings.Contains(text, "netscaler") ||
		!strings.Contains(text, "waiting for the first collection") {
		t.Fatalf("text = %q, want the first-collection message naming the probe", text)
	}
}

// TestHandlePRTGMetricsGET_UnknownNamesRecordNothing guards the cache against
// names taken from requests: a sensor polling a misspelled probe gets the
// error form at every scan, and must not grow the first-data map each time.
func TestHandlePRTGMetricsGET_UnknownNamesRecordNothing(t *testing.T) {
	const agentKey = "test-agent-key"
	apiManager, cache, _ := newMixedCaseHarness(t, agentKey)
	boundFirstDataWait(apiManager)

	cache.mu.RLock()
	before := len(cache.firstData)
	cache.mu.RUnlock()

	for i := 0; i < 50; i++ {
		w := getPRTG(t, apiManager, agentKey, fmt.Sprintf("no-such-probe-%d", i))
		decodePRTGErrorForm(t, w.Body.String())
	}

	cache.mu.RLock()
	after := len(cache.firstData)
	cache.mu.RUnlock()
	if after != before {
		t.Fatalf("first-data map grew from %d to %d entries for names no probe carries", before, after)
	}
}
