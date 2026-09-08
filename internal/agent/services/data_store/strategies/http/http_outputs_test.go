package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	_ "senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	_ "senhub-agent.go/internal/agent/services/data_store/strategies/prtg"
	"senhub-agent.go/internal/agent/services/logger"
)

// pathedConfig is the bare test configuration plus a config path, which
// the outputs handlers need to find strategies.d.
type pathedConfig struct {
	configuration.AgentConfiguration
	path string
}

func (p pathedConfig) GetConfigPath() string { return p.path }

func newOutputsTestRouter(t *testing.T) (*mux.Router, string) {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"), []byte("http:\n  port: 8080\n  endpoints: [\"web\", \"prtg\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret.SetConfigDir(dir)
	secret.SetProvider(secret.NewMemoryProvider())
	agentstate.ResetStrategyFailuresForTest()
	agentstate.ResetExportActivityForTest()

	OutputValidator = func(outputType string, params map[string]interface{}) error {
		if outputType == "otlp" {
			_, err := otlp.ParseConfig(params)
			return err
		}
		return nil
	}
	t.Cleanup(func() { OutputValidator = nil })

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	cfg := pathedConfig{AgentConfiguration: configuration.NewAgentConfiguration("test-agent-key", "", baseLogger), path: main}
	strategy, ok := NewHTTPSyncStrategy(cfg, map[string]interface{}{"endpoints": []interface{}{"web", "prtg"}}, baseLogger).(*HTTPSyncStrategy)
	if !ok {
		t.Fatal("strategy cast")
	}
	return NewHTTPHandlers(strategy).SetupRoutes(), dir
}

func doJSON(t *testing.T, router *mux.Router, method, url string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	out := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestOutputsAPI_CatalogListWriteAndState(t *testing.T) {
	router, dir := newOutputsTestRouter(t)
	base := "/api/test-agent-key"

	code, cat := doJSON(t, router, "GET", base+"/catalog/outputs", nil)
	if code != 200 {
		t.Fatalf("catalog: %d %v", code, cat)
	}
	seen := map[string]bool{}
	for _, e := range cat["outputs"].([]interface{}) {
		m := e.(map[string]interface{})
		seen[m["type"].(string)] = m["configured"].(bool)
	}
	if !seen["http"] || seen["otlp"] {
		t.Errorf("http must be configured and otlp not, got %v", seen)
	}

	code, resp := doJSON(t, router, "POST", base+"/config/outputs", map[string]interface{}{
		"type": "otlp", "params": map[string]interface{}{"endpoint": "collector:4317", "headers": map[string]interface{}{"Authorization": "Bearer t0k"}},
	})
	if code != 201 {
		t.Fatalf("create: %d %v", code, resp)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "strategies.d", "50-otlp.yaml"))
	if strings.Contains(string(raw), "t0k") {
		t.Error("the token must be sealed, not written")
	}
	if code, resp := doJSON(t, router, "POST", base+"/config/outputs", map[string]interface{}{"type": "http", "params": map[string]interface{}{}}); code != 400 || !strings.Contains(resp["error"].(string), "once") {
		t.Errorf("a second http must be refused: %d %v", code, resp)
	}
	if code, _ := doJSON(t, router, "POST", base+"/config/outputs", map[string]interface{}{"type": "otlp", "params": map[string]interface{}{"protocol": "smoke"}}); code != 400 {
		t.Errorf("a second otlp, or a bad protocol, must be refused: %d", code)
	}

	agentstate.RecordExportFailure("otlp", "connection refused")
	code, list := doJSON(t, router, "GET", base+"/config/outputs", nil)
	if code != 200 {
		t.Fatalf("list: %d %v", code, list)
	}
	states := map[string]map[string]interface{}{}
	for _, e := range list["outputs"].([]interface{}) {
		m := e.(map[string]interface{})
		states[m["name"].(string)] = m
	}
	if states["http"]["state"] != "listening" || states["http"]["readers"] == nil {
		t.Errorf("http must be listening with readers, got %v", states["http"])
	}
	if states["otlp"]["state"] != "failing" || states["otlp"]["reason"] != "connection refused" {
		t.Errorf("otlp must be failing with its reason, got %v", states["otlp"])
	}
	if hdrs := states["otlp"]["params"].(map[string]interface{})["headers"].(map[string]interface{}); strings.Contains(hdrs["Authorization"].(string), "t0k") {
		t.Errorf("the listing must not carry the token, got %v", hdrs)
	}

	code, resp = doJSON(t, router, "PUT", base+"/config/outputs/otlp", map[string]interface{}{"enabled": false, "params": map[string]interface{}{"endpoint": "collector:4318", "protocol": "http"}})
	if code != 200 || !strings.HasSuffix(resp["path"].(string), ".disabled") {
		t.Fatalf("disable: %d %v", code, resp)
	}
	_, list = doJSON(t, router, "GET", base+"/config/outputs", nil)
	for _, e := range list["outputs"].([]interface{}) {
		if m := e.(map[string]interface{}); m["name"] == "otlp" && m["state"] != "disabled" {
			t.Errorf("a disabled output must be listed as disabled, got %v", m)
		}
	}
	if code, _ := doJSON(t, router, "PUT", base+"/config/outputs/http", map[string]interface{}{"enabled": false, "params": map[string]interface{}{"port": 8080}}); code != 400 {
		t.Errorf("http must not be disabled from the console: %d", code)
	}
	if code, _ := doJSON(t, router, "DELETE", base+"/config/outputs/http", nil); code != 400 {
		t.Errorf("http must not be removed from the console: %d", code)
	}
	if code, _ := doJSON(t, router, "DELETE", base+"/config/outputs/otlp", nil); code != 200 {
		t.Errorf("delete otlp: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "strategies.d", "50-otlp.yaml.disabled")); !os.IsNotExist(err) {
		t.Error("the disabled file must be gone after delete")
	}
}

func TestOutputsAPI_ValidateAndTest(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	base := "/api/test-agent-key"

	code, resp := doJSON(t, router, "POST", base+"/config/outputs/validate", map[string]interface{}{"type": "otlp", "params": map[string]interface{}{}})
	if code != 200 || resp["valid"] != false || resp["field"] != "endpoint" {
		t.Errorf("a missing endpoint must be invalid and anchored on endpoint, got %d %v", code, resp)
	}
	code, resp = doJSON(t, router, "POST", base+"/config/outputs/validate", map[string]interface{}{"type": "otlp", "params": map[string]interface{}{"endpoint": "c:4317", "tls": map[string]interface{}{"enabled": false}}})
	if code != 200 || resp["valid"] != true {
		t.Errorf("a plain config must be valid, got %d %v", code, resp)
	}
	code, resp = doJSON(t, router, "POST", base+"/config/outputs/validate", map[string]interface{}{"type": "otlp", "params": map[string]interface{}{"endpoint": "c:4317", "colour": "red"}})
	if resp["valid"] != false || !strings.Contains(resp["errors"].([]interface{})[0].(string), "colour") {
		t.Errorf("an unknown key must be named, got %d %v", code, resp)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	code, resp = doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{"type": "prtg", "params": map[string]interface{}{"server_url": srv.URL}, "timeout": 3})
	if code != 200 || resp["valid"] != true {
		t.Errorf("a reachable PRTG url must pass, got %d %v", code, resp)
	}
	code, resp = doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{"type": "otlp", "params": map[string]interface{}{"endpoint": "127.0.0.1:1", "tls": map[string]interface{}{"enabled": false}}, "timeout": 3})
	if code != 200 || resp["valid"] != false {
		t.Errorf("a closed port must fail, got %d %v", code, resp)
	}
	steps := resp["steps"].([]interface{})
	if last := steps[len(steps)-1].(map[string]interface{}); last["name"] != "tcp" {
		t.Errorf("the failing step must be tcp, got %v", last)
	}
	code, resp = doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{"type": "http", "params": map[string]interface{}{}})
	if code != 200 || resp["valid"] != true {
		t.Errorf("the http output reports that it listens, got %d %v", code, resp)
	}
}

func TestInfoEvents(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	agentstate.ResetEventsForTest()
	agentstate.RecordEvent(agentstate.EventWarn, agentstate.EventKindProbe, "cpu", "collect failed")
	code, resp := doJSON(t, router, "GET", "/api/test-agent-key/info/events", nil)
	if code != 200 || resp["count"] != float64(1) {
		t.Fatalf("events: %d %v", code, resp)
	}
	ev := resp["events"].([]interface{})[0].(map[string]interface{})
	if ev["subject"] != "cpu" || ev["level"] != "warn" {
		t.Errorf("event wrong: %v", ev)
	}
}
