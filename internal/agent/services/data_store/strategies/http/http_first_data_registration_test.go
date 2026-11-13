package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

type probesConfig struct {
	configuration.AgentConfiguration
	probes []configuration.ProbeConfig
}

func (c *probesConfig) GetConfiguration() configuration.ConfigurationData {
	return configuration.ConfigurationData{Probes: c.probes}
}

func boolPtr(b bool) *bool { return &b }

type deadlineWriter struct {
	http.ResponseWriter
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error { return nil }

func newFirstDataProbesConfig(probes ...configuration.ProbeConfig) *probesConfig {
	return &probesConfig{
		AgentConfiguration: configuration.NewAgentConfiguration(firstWaitKey, "http://test-server", createTestLoggerForAPI()),
		probes:             probes,
	}
}

func TestPRTGFirstData_ProbeRegistersAfterTheReading(t *testing.T) {
	api, cache, registry := firstWaitHarness(t, time.Second)
	agentstate.SetActiveProbeNames(nil)
	t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
	api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu"})
	api.strategy.firstDataWait = 5 * time.Second

	go func() {
		time.Sleep(300 * time.Millisecond)
		agentstate.SetActiveProbeNames(map[string]string{"cpu-wait": "probe-id-wait"})
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
	if elapsed < 550*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("answered after %v, want about 600ms (registration at 300ms, data at 600ms)", elapsed)
	}
}

func TestPRTGFirstData_ConfiguredProbeNeverRegistersAnswersNoChannel(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	agentstate.SetActiveProbeNames(nil)
	t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
	api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu"})
	api.strategy.firstDataWait = 400 * time.Millisecond

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if n := channelCount(t, w); n != 0 {
		t.Fatalf("channels = %d, want 0", n)
	}
	if elapsed < 350*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want about the 400ms wait", elapsed)
	}
}

func TestPRTGFirstData_UnconfiguredNameAnswersAtOnce(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		api, _, _ := firstWaitHarness(t, time.Second)
		agentstate.SetActiveProbeNames(nil)
		t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
		api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "some-other", Type: "cpu"})
		api.strategy.firstDataWait = 5 * time.Second

		req := httptest.NewRequest("GET", "/x", nil)
		req = mux.SetURLVars(req, map[string]string{"agentkey": firstWaitKey, "probe": "ghost"})

		w := httptest.NewRecorder()
		begin := time.Now()
		api.HandlePRTGMetricsGET(w, req)

		if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
			t.Fatalf("answered after %v for an unconfigured probe, want immediate", elapsed)
		}
	})

	t.Run("disabled configured", func(t *testing.T) {
		api, _, _ := firstWaitHarness(t, time.Second)
		agentstate.SetActiveProbeNames(nil)
		t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
		api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu", Enabled: boolPtr(false)})
		api.strategy.firstDataWait = 5 * time.Second

		w := httptest.NewRecorder()
		begin := time.Now()
		api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))

		if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
			t.Fatalf("answered after %v for a disabled configured probe, want immediate", elapsed)
		}
	})
}

func TestPRTGFirstData_RegisteredEventDrivenEndsTheWait(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	agentstate.SetActiveProbeNames(nil)
	t.Cleanup(func() {
		agentstate.SetActiveProbeNames(nil)
		agentstate.SetActiveProbes(nil)
	})
	api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu"})
	api.strategy.firstDataWait = 5 * time.Second

	go func() {
		time.Sleep(200 * time.Millisecond)
		agentstate.MarkProbeEventDriven("probe-id-wait")
		agentstate.SetActiveProbes([]string{"probe-id-wait"})
		agentstate.SetActiveProbeNames(map[string]string{"cpu-wait": "probe-id-wait"})
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.HandlePRTGMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if elapsed > time.Second {
		t.Fatalf("answered after %v, want less than 1s for an event-driven registration", elapsed)
	}
}

func TestNagiosFirstData_ProbeRegistersAfterTheReading(t *testing.T) {
	api, cache, registry := firstWaitHarness(t, time.Second)
	agentstate.SetActiveProbeNames(nil)
	t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
	api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu"})
	api.strategy.firstDataWait = 5 * time.Second
	api.strategy.nagiosManager = NewNagiosManager(api.strategy, api.logger)

	go func() {
		time.Sleep(300 * time.Millisecond)
		agentstate.SetActiveProbeNames(map[string]string{"cpu-wait": "probe-id-wait"})
		time.Sleep(300 * time.Millisecond)
		addFirstWaitData(cache, registry)
	}()

	w := httptest.NewRecorder()
	begin := time.Now()
	api.strategy.nagiosManager.HandleNagiosMetricsGET(w, firstWaitRequest(context.Background()))
	elapsed := time.Since(begin)

	if body := w.Body.String(); strings.Contains(body, "has not completed a cycle") || strings.Contains(body, "No metrics available") {
		t.Fatalf("answered with the no-data summary: %q", body)
	}
	if elapsed < 550*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("answered after %v, want about 600ms", elapsed)
	}
}

func TestExtendWriteDeadline_UnsupportedWriterCapsTheWait(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)

	rec := httptest.NewRecorder()
	if got := api.strategy.extendWriteDeadline(rec, 30*time.Second); got != serverWriteTimeout-writeDeadlineMargin {
		t.Fatalf("extendWriteDeadline(30s) on unsupported writer = %v, want %v", got, serverWriteTimeout-writeDeadlineMargin)
	}
	if got := api.strategy.extendWriteDeadline(rec, 3*time.Second); got != 3*time.Second {
		t.Fatalf("extendWriteDeadline(3s) on unsupported writer = %v, want 3s", got)
	}

	cw := &deadlineWriter{ResponseWriter: rec}
	if got := api.strategy.extendWriteDeadline(cw, 30*time.Second); got != 30*time.Second {
		t.Fatalf("extendWriteDeadline(30s) on supported writer = %v, want 30s", got)
	}
}

func TestPRTGFirstData_NoDataOverRealServerWithServerTimeouts(t *testing.T) {
	api, _, _ := firstWaitHarness(t, time.Second)
	agentstate.SetActiveProbeNames(nil)
	t.Cleanup(func() { agentstate.SetActiveProbeNames(nil) })
	api.strategy.agentConfig = newFirstDataProbesConfig(configuration.ProbeConfig{Name: "cpu-wait", Type: "cpu"})
	api.strategy.firstDataWait = serverWriteTimeout

	router := mux.NewRouter()
	router.Use(CountRequests)
	router.Use(TraceRequests)
	router.Use(NoStore)
	router.HandleFunc("/api/{agentkey}/prtg/metrics/{probe}", api.HandlePRTGMetricsGET).Methods("GET")

	srv := (&ServerManager{strategy: api.strategy}).createHTTPServer(router)
	ts := httptest.NewUnstartedServer(router)
	ts.Config.ReadTimeout = srv.ReadTimeout
	ts.Config.WriteTimeout = srv.WriteTimeout
	ts.Config.IdleTimeout = srv.IdleTimeout
	ts.Start()
	t.Cleanup(ts.Close)

	client := &http.Client{Timeout: 30 * time.Second}
	begin := time.Now()
	resp, err := client.Get(ts.URL + "/api/" + firstWaitKey + "/prtg/metrics/cpu-wait")
	elapsed := time.Since(begin)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var prtgResp PRTGResponse
	if err := json.NewDecoder(resp.Body).Decode(&prtgResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(prtgResp.PRTG.Result) != 0 {
		t.Fatalf("result length = %d, want 0", len(prtgResp.PRTG.Result))
	}
	if elapsed < 9500*time.Millisecond {
		t.Fatalf("answered after %v, want at least 9.5s", elapsed)
	}
}
