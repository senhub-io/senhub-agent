package wildfly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// wildflyHandler provides canned WildFly Management API responses for
// unit tests. It recognises requests by the operation+address in the
// JSON body and returns a plausible result envelope.
type wildflyHandler struct {
	// If set, all requests return HTTP 401.
	Unauthorized bool
	// If set, the handler returns this outcome string instead of "success".
	FailOutcome string
}

func (h *wildflyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Unauthorized {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Decode the incoming request body.
	var req mgmtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	outcome := "success"
	if h.FailOutcome != "" {
		outcome = h.FailOutcome
	}

	// Build a result based on the operation + address.
	var result interface{}
	switch {
	case isAddress(req.Address, "core-service", "platform-mbean") &&
		isAddress(req.Address, "type", "memory"):
		// JVM memory read-resource
		result = map[string]interface{}{
			"heap-memory-usage": map[string]interface{}{
				"used":      int64(512 * 1024 * 1024),
				"committed": int64(768 * 1024 * 1024),
				"max":       int64(1024 * 1024 * 1024),
			},
		}

	case isAddress(req.Address, "subsystem", "undertow"):
		result = map[string]interface{}{
			"request-count":  int64(1234),
			"error-count":    int64(5),
			"bytes-sent":     int64(9999999),
			"bytes-received": int64(111111),
		}

	case isAddress(req.Address, "subsystem", "transactions"):
		result = map[string]interface{}{
			"number-of-transactions":         int64(100),
			"number-of-aborted-transactions": int64(3),
		}

	case req.Operation == "read-children-names" &&
		isAddress(req.Address, "subsystem", "datasources"):
		// List of datasource names.
		result = []string{"ExampleDS"}

	case isAddress(req.Address, "subsystem", "datasources") &&
		isAddress(req.Address, "data-source", "ExampleDS"):
		result = map[string]interface{}{
			"statistics": map[string]interface{}{
				"pool": map[string]interface{}{
					"ActiveCount":    int64(4),
					"AvailableCount": int64(16),
				},
			},
		}

	default:
		// Unknown address — return an empty-but-successful result.
		result = map[string]interface{}{}
	}

	if h.FailOutcome != "" {
		result = nil
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"outcome":             outcome,
		"result":              result,
		"failure-description": "simulated failure",
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// isAddress returns true when addr contains a map entry {key: value}.
func isAddress(addr []map[string]string, key, value string) bool {
	for _, entry := range addr {
		if v, ok := entry[key]; ok && v == value {
			return true
		}
	}
	return false
}

func newTestProbe(t *testing.T, srv *httptest.Server) *WildflyProbe {
	t.Helper()
	probe, err := NewWildflyProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "admin",
		"password": "admin",
		"timeout":  5,
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewWildflyProbe: %v", err)
	}
	p := probe.(*WildflyProbe)
	p.SetName("test-wf")
	return p
}

// collectByName runs Collect and returns a map of metric name → value.
func collectByName(t *testing.T, p *WildflyProbe) map[string]float64 {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := make(map[string]float64, len(points))
	for _, dp := range points {
		out[dp.Name] = dp.Value
	}
	return out
}

// TestCollect_AllMetricsPresent verifies that a reachable WildFly
// instance produces all expected metrics including the up=1 gauge.
func TestCollect_AllMetricsPresent(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)

	required := []string{
		"senhub.wildfly.up",
		"jvm.memory.heap.used",
		"jvm.memory.heap.committed",
		"jvm.memory.heap.max",
		"wildfly.request.count",
		"wildfly.error.count",
		"wildfly.bytes.sent",
		"wildfly.bytes.received",
		"wildfly.transaction.committed",
		"wildfly.transaction.rolledback",
		"wildfly.datasource.connections.active",
		"wildfly.datasource.connections.available",
	}
	for _, name := range required {
		if _, ok := got[name]; !ok {
			t.Errorf("missing metric %s", name)
		}
	}
}

// TestCollect_Up verifies that up=1 when server is reachable.
func TestCollect_Up(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)

	if got["senhub.wildfly.up"] != 1 {
		t.Errorf("up = %v, want 1", got["senhub.wildfly.up"])
	}
}

// TestCollect_UpZeroWhenUnreachable checks that a connection failure
// records up=0 without returning a Collect() error.
func TestCollect_UpZeroWhenUnreachable(t *testing.T) {
	// Point at a port that refuses connections.
	probe, err := NewWildflyProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:1",
		"timeout":  1,
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewWildflyProbe: %v", err)
	}
	p := probe.(*WildflyProbe)
	p.SetName("test-down")

	got := collectByName(t, p)
	if got["senhub.wildfly.up"] != 0 {
		t.Errorf("up = %v, want 0 when server is unreachable", got["senhub.wildfly.up"])
	}
}

// TestCollect_UpZeroOnAuthFailure checks that a 401 response maps to up=0.
func TestCollect_UpZeroOnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{Unauthorized: true})
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)

	if got["senhub.wildfly.up"] != 0 {
		t.Errorf("up = %v, want 0 on auth failure", got["senhub.wildfly.up"])
	}
}

// TestCollect_UpZeroOnManagementFailure checks that a failed outcome
// from the management API maps to up=0.
func TestCollect_UpZeroOnManagementFailure(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{FailOutcome: "failed"})
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)

	if got["senhub.wildfly.up"] != 0 {
		t.Errorf("up = %v, want 0 on management API failure", got["senhub.wildfly.up"])
	}
}

// TestCollect_MetricValues spot-checks a few metric values match the
// fake server responses.
func TestCollect_MetricValues(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)

	// JVM heap: 512 MiB = 536870912 bytes
	const expectedHeapUsed = float64(512 * 1024 * 1024)
	if got["jvm.memory.heap.used"] != expectedHeapUsed {
		t.Errorf("jvm.memory.heap.used = %v, want %v", got["jvm.memory.heap.used"], expectedHeapUsed)
	}

	if got["wildfly.request.count"] != 1234 {
		t.Errorf("wildfly.request.count = %v, want 1234", got["wildfly.request.count"])
	}
	if got["wildfly.transaction.committed"] != 100 {
		t.Errorf("wildfly.transaction.committed = %v, want 100", got["wildfly.transaction.committed"])
	}
	if got["wildfly.datasource.connections.active"] != 4 {
		t.Errorf("wildfly.datasource.connections.active = %v, want 4", got["wildfly.datasource.connections.active"])
	}
}

// TestParseConfig_Defaults verifies that omitted fields take the expected
// defaults and that the constructor does not error on a minimal config.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, defaultEndpoint)
	}
	if cfg.Username != defaultUsername {
		t.Errorf("Username = %q, want %q", cfg.Username, defaultUsername)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, defaultTimeout)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", cfg.Interval, defaultInterval)
	}
}

// TestParseConfig_Overrides verifies that all configuration fields are
// parsed from the config map.
func TestParseConfig_Overrides(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint": "http://wf.example.com:9990",
		"username": "ops",
		"password": "s3cr3t",
		"timeout":  30,
		"interval": 120,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != "http://wf.example.com:9990" {
		t.Errorf("Endpoint = %q, want http://wf.example.com:9990", cfg.Endpoint)
	}
	if cfg.Username != "ops" {
		t.Errorf("Username = %q, want ops", cfg.Username)
	}
	if cfg.Password != "s3cr3t" {
		t.Errorf("Password = %q, want s3cr3t", cfg.Password)
	}
}

// TestProbeType confirms that SetProbeType was called with the right
// identifier string.
func TestProbeType(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	if got := p.GetProbeType(); got != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", got, ProbeType)
	}
}

// TestMetricTypeTags verifies that metric_type tags are set correctly.
func TestMetricTypeTags(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Build a map of metric name → metric_type tag value.
	metricTypes := make(map[string]string)
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "metric_type" {
				metricTypes[dp.Name] = tg.Value
			}
		}
	}

	cases := map[string]string{
		"senhub.wildfly.up":                        "availability",
		"jvm.memory.heap.used":                     "memory",
		"wildfly.request.count":                    "requests",
		"wildfly.transaction.committed":            "operations",
		"wildfly.datasource.connections.active":    "connections",
		"wildfly.datasource.connections.available": "connections",
	}
	for metric, wantType := range cases {
		if got, ok := metricTypes[metric]; !ok {
			t.Errorf("metric %s: missing metric_type tag", metric)
		} else if got != wantType {
			t.Errorf("metric %s: metric_type = %q, want %q", metric, got, wantType)
		}
	}
}
