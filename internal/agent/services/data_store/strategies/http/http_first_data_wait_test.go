package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

const firstWaitKey = "test-agent-key"

func firstWaitHarness(t *testing.T, startedAgo time.Duration) (*APIManager, *MetricCache, *transformers.TransformerRegistry) {
	t.Helper()
	api, cache, registry := newMixedCaseHarness(t, firstWaitKey)
	api.strategy.startTime = time.Now().Add(-startedAgo)
	api.strategy.firstDataWait = 200 * time.Millisecond
	agentstate.SetActiveProbeNames(map[string]string{"cpu-wait": "probe-id-wait"})
	t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
	return api, cache, registry
}

func addFirstWaitData(cache *MetricCache, registry *transformers.TransformerRegistry) {
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{{
		Name:      "cpu_usage_total",
		Timestamp: time.Now(),
		Value:     42,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu-wait"},
			{Key: "probe_type", Value: "cpu"},
		},
	}}, registry)
}

func firstWaitRequest(ctx context.Context) *http.Request {
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/prtg/metrics/cpu-wait", firstWaitKey), nil).WithContext(ctx)
	return mux.SetURLVars(req, map[string]string{"agentkey": firstWaitKey, "probe": "cpu-wait"})
}

func channelCount(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp PRTGResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return len(resp.PRTG.Result)
}

func TestPRTGFirstData_WaitsThenAnswersWithChannels(t *testing.T) {
	api, cache, registry := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second

	go func() {
		time.Sleep(300 * time.Millisecond)
		addFirstWaitData(cache, registry)
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if n := channelCount(t, w); n == 0 {
		t.Fatal("response has no channel although data landed during the wait")
	}
	if elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want about 300ms (returned on data, not at the wait limit)", elapsed)
	}
}

func TestPRTGFirstData_NoDataAnswersNoChannelAfterWait(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if n := channelCount(t, w); n != 0 {
		t.Fatalf("channels = %d, want 0", n)
	}
	if elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want about the 200ms wait", elapsed)
	}
}

func TestPRTGFirstData_OutsideStartWindowAnswersAtOnce(t *testing.T) {
	api, _, _ := firstWaitHarness(t, 3*time.Minute)
	api.strategy.firstDataWait = 5 * time.Second

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))

	if n := channelCount(t, w); n != 0 {
		t.Fatalf("channels = %d, want 0", n)
	}
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("answered after %v, want immediate outside the start window", elapsed)
	}
}

func TestPRTGFirstData_UnknownProbeAnswersAtOnce(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second
	req := httptest.NewRequest("GET", "/x", nil)
	req = mux.SetURLVars(req, map[string]string{"agentkey": firstWaitKey, "probe": "not-configured"})

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, req)

	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("answered after %v for a probe the agent does not run", elapsed)
	}
}

func TestPRTGFirstData_ClientGoneReturnsPromptly(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(ctx))

	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("handler took %v after the client went away", elapsed)
	}
}

func TestNagiosFirstData_WaitsThenAnswers(t *testing.T) {
	api, cache, registry := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second
	api.strategy.nagiosManager = NewNagiosManager(api.strategy, api.logger)

	go func() {
		time.Sleep(300 * time.Millisecond)
		addFirstWaitData(cache, registry)
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.strategy.nagiosManager.HandleNagiosMetricsGET(w, firstWaitRequest(context.Background()))

	if body := w.Body.String(); strings.Contains(body, "has not completed a cycle") || strings.Contains(body, "No metrics available") {
		t.Fatalf("answered with the no-data summary: %q", body)
	}
	if elapsed := time.Since(begin); elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want about 300ms", elapsed)
	}
}

func TestPRTGFirstData_EventDrivenProbeAnswersAtOnce(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second
	agentstate.SetActiveProbes([]string{"probe-id-wait"})
	t.Cleanup(func() { agentstate.SetActiveProbes(nil) })
	agentstate.MarkProbeEventDriven("probe-id-wait")

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))

	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("answered after %v for an event-driven probe, want immediate", elapsed)
	}
}

func TestPRTGFirstData_FirstCycleWithNoPointsAnswersAtOnce(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second
	agentstate.SetActiveProbes([]string{"probe-id-wait"})
	t.Cleanup(func() { agentstate.SetActiveProbes(nil) })
	agentstate.RecordProbeCycle("probe-id-wait", 0)

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))

	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("answered after %v although the first collection finished empty", elapsed)
	}
}

func TestPRTGFirstData_WaitEndsWhenFirstCycleFinishesEmpty(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	api.strategy.firstDataWait = 5 * time.Second
	agentstate.SetActiveProbes([]string{"probe-id-wait"})
	t.Cleanup(func() { agentstate.SetActiveProbes(nil) })

	go func() {
		time.Sleep(300 * time.Millisecond)
		agentstate.RecordProbeCycle("probe-id-wait", 0)
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want about 300ms (when the first cycle ended, not at the wait limit)", elapsed)
	}
}
