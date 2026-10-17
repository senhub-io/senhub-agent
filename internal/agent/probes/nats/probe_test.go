package nats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, srv *httptest.Server) *NATSProbe {
	t.Helper()
	cfg := map[string]interface{}{
		"endpoint": srv.URL,
	}
	p, err := NewNATSProbe(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewNATSProbe: %v", err)
	}
	np := p.(*NATSProbe)
	np.SetName("nats-test")
	return np
}

// pointMap indexes collected datapoints by name for easy assertion.
func pointMap(t *testing.T, p *NATSProbe) map[string]float64 {
	t.Helper()
	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	m := make(map[string]float64, len(pts))
	for _, dp := range pts {
		m[dp.Name] = dp.Value
	}
	return m
}

func findPoint(pts []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, dp := range pts {
		if dp.Name == name {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

func tagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

// TestParseConfig_Defaults verifies the zero-config case resolves to sane defaults.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint: want %q got %q", defaultEndpoint, cfg.Endpoint)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("interval: want %v got %v", defaultInterval, cfg.Interval)
	}
}

// TestParseConfig_Override verifies explicit values are honoured.
func TestParseConfig_Override(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint": "http://nats.example.com:8222",
		"interval": 30,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != "http://nats.example.com:8222" {
		t.Errorf("endpoint: got %q", cfg.Endpoint)
	}
	if cfg.Interval.Seconds() != 30 {
		t.Errorf("interval: want 30s got %v", cfg.Interval)
	}
}

// TestCollect_ServerUp verifies all expected metrics are emitted when NATS is up.
func TestCollect_ServerUp(t *testing.T) {
	varz := varzResponse{
		Connections:      5,
		TotalConnections: 42,
		Subscriptions:    20,
		InMsgs:           1000,
		OutMsgs:          900,
		InBytes:          512000,
		OutBytes:         480000,
		SlowConsumers:    1,
	}
	routez := routezResponse{NumRoutes: 3}
	jsz := jszResponse{Streams: 2, Consumers: 4, Messages: 5000, Bytes: 1024000}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/varz":
			if err := json.NewEncoder(w).Encode(varz); err != nil {
				t.Errorf("encode /varz: %v", err)
			}
		case "/routez":
			if err := json.NewEncoder(w).Encode(routez); err != nil {
				t.Errorf("encode /routez: %v", err)
			}
		case "/jsz":
			if err := json.NewEncoder(w).Encode(jsz); err != nil {
				t.Errorf("encode /jsz: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := pointMap(t, p)

	expect := map[string]float64{
		"senhub.nats.up":           1,
		"nats.connections.count":   5,
		"nats.connections.total":   42,
		"nats.subscriptions.count": 20,
		"nats.messages.in":         1000,
		"nats.messages.out":        900,
		"nats.bytes.in":            512000,
		"nats.bytes.out":           480000,
		"nats.slow_consumers":      1,
		"nats.routes.count":        3,
		"nats.jetstream.streams":   2,
		"nats.jetstream.consumers": 4,
		"nats.jetstream.messages":  5000,
		"nats.jetstream.storage":   1024000,
	}

	for name, want := range expect {
		if v, ok := got[name]; !ok {
			t.Errorf("metric %q missing from output", name)
		} else if v != want {
			t.Errorf("metric %q: want %v got %v", name, want, v)
		}
	}
}

// TestCollect_ServerDown verifies that up=0 is emitted and no panic occurs when
// the management API is unreachable.
func TestCollect_ServerDown(t *testing.T) {
	// Bind to a server and immediately close it so requests fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	p := newTestProbe(t, srv)
	got := pointMap(t, p)

	if up, ok := got["senhub.nats.up"]; !ok || up != 0 {
		t.Errorf("expected senhub.nats.up=0 when server is down, got %v (present=%v)", got["senhub.nats.up"], ok)
	}
	// No other metrics should be emitted when the server is unreachable.
	if _, ok := got["nats.connections.count"]; ok {
		t.Error("connections.count should not be emitted when server is down")
	}
}

// TestCollect_JetStreamDisabled verifies that /jsz 404 is tolerated gracefully.
func TestCollect_JetStreamDisabled(t *testing.T) {
	varz := varzResponse{Connections: 2}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/varz":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(varz); err != nil {
				t.Errorf("encode /varz: %v", err)
			}
		case "/routez":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(routezResponse{}); err != nil {
				t.Errorf("encode /routez: %v", err)
			}
		case "/jsz":
			// JetStream not enabled: server returns 503 or 404.
			http.Error(w, "JetStream not enabled", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := pointMap(t, p)

	if got["senhub.nats.up"] != 1 {
		t.Errorf("expected up=1 when varz succeeds, got %v", got["senhub.nats.up"])
	}
	if _, ok := got["nats.jetstream.streams"]; ok {
		t.Error("JetStream metrics should not appear when /jsz returns 503")
	}
}

// TestCollect_EntityKeysAndCheckResult verifies identity tags and the
// successful check_result metric.
func TestCollect_EntityKeysAndCheckResult(t *testing.T) {
	varz := varzResponse{
		Connections: 1,
		ServerID:    "S1",
		Version:     "2.10.0",
	}
	routez := routezResponse{NumRoutes: 0}
	jsz := jszResponse{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/varz":
			if err := json.NewEncoder(w).Encode(varz); err != nil {
				t.Errorf("encode /varz: %v", err)
			}
		case "/routez":
			if err := json.NewEncoder(w).Encode(routez); err != nil {
				t.Errorf("encode /routez: %v", err)
			}
		case "/jsz":
			if err := json.NewEncoder(w).Encode(jsz); err != nil {
				t.Errorf("encode /jsz: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, srv)
	p.entitySrc.getHostID = func() string { return "host-1" }

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("Collect returned no points")
	}

	for _, dp := range pts {
		v, ok := tagValue(dp.Tags, "service.instance.id")
		if !ok {
			t.Errorf("point %q missing service.instance.id tag", dp.Name)
			continue
		}
		want := "nats:S1"
		if v != want {
			t.Errorf("point %q service.instance.id = %q, want %q", dp.Name, v, want)
		}
	}

	check, ok := findPoint(pts, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("check result metric %q not found", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("check result = %v, want 0", check.Value)
	}
	if v, ok := tagValue(check.Tags, "metric_type"); !ok || v != "status" {
		t.Errorf("check result metric_type = %q, want status", v)
	}
}

// TestCollect_ConnectionFailureKeysAndCheckResult verifies identity tags are
// born on the error path and the check result classifies the failed connection.
func TestCollect_ConnectionFailureKeysAndCheckResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	p := newTestProbe(t, srv)
	p.entitySrc.getHostID = func() string { return "host-1" }

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up, ok := findPoint(pts, "senhub.nats.up")
	if !ok {
		t.Fatal("senhub.nats.up not found")
	}
	if up.Value != 0 {
		t.Errorf("up = %v, want 0", up.Value)
	}
	if v, ok := tagValue(up.Tags, "service.instance.id"); ok {
		t.Errorf("up service.instance.id = %q before any entity, want none", v)
	}

	check, ok := findPoint(pts, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("check result metric %q not found", dbcommon.CheckResultMetric)
	}
	want := float64(dbcommon.CheckTargetDown)
	if check.Value != want {
		t.Errorf("check result = %v, want %v", check.Value, want)
	}
	if v, ok := tagValue(check.Tags, "service.instance.id"); ok {
		t.Errorf("check result identity = %q before any entity, want none", v)
	}
	mv, ok := tagValue(check.Tags, "metric_type")
	if !ok || mv != "status" {
		t.Errorf("check result metric_type = %q, present=%v; want status", mv, ok)
	}
}

// TestTagsWithType verifies metric_type overwrite logic.
func TestTagsWithType(t *testing.T) {
	base := []tags.Tag{
		{Key: "instance", Value: "http://localhost:8222"},
		{Key: "metric_type", Value: "overview"},
	}
	result := tagsWithType(base, "connections")

	var found bool
	for _, tg := range result {
		if tg.Key == "metric_type" {
			if tg.Value != "connections" {
				t.Errorf("metric_type: want %q got %q", "connections", tg.Value)
			}
			found = true
		}
	}
	if !found {
		t.Error("metric_type tag not found in result")
	}
	// original slice must not be mutated.
	for _, tg := range base {
		if tg.Key == "metric_type" && tg.Value != "overview" {
			t.Error("tagsWithType must not mutate the base slice")
		}
	}
}
