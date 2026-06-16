package ceph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// newTestLogger returns a base logger suitable for unit tests.
func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// TestParseConfig_Defaults verifies that defaults are applied when only the
// required fields are provided.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"username": "admin",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://localhost:8443" {
		t.Errorf("default endpoint = %q; want https://localhost:8443", cfg.Endpoint)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("default interval = %v; want %v", cfg.Interval, defaultInterval)
	}
	if !cfg.VerifyTLS {
		t.Error("default verify_tls should be true")
	}
}

// TestParseConfig_MissingCredentials verifies that missing username or password
// returns an error.
func TestParseConfig_MissingCredentials(t *testing.T) {
	cases := []map[string]interface{}{
		{"password": "secret"},
		{"username": "admin"},
		{},
	}
	for _, c := range cases {
		_, err := parseConfig(c)
		if err == nil {
			t.Errorf("expected error for config %v, got nil", c)
		}
	}
}

// TestParseConfig_Override verifies that all fields can be overridden.
func TestParseConfig_Override(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint":   "https://ceph.local:8080",
		"username":   "admin",
		"password":   "pass",
		"verify_tls": false,
		"interval":   30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://ceph.local:8080" {
		t.Errorf("endpoint = %q; want https://ceph.local:8080", cfg.Endpoint)
	}
	if cfg.VerifyTLS {
		t.Error("verify_tls should be false")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v; want 30s", cfg.Interval)
	}
}

// fakeServer builds a minimal Ceph API mock and returns a started httptest
// server together with the CephProbe pointed at it.
func fakeServer(t *testing.T) (*httptest.Server, *CephProbe) {
	t.Helper()

	mux := http.NewServeMux()

	// POST /api/auth → token
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token-xyz"})
	})

	// GET /api/health/full
	mux.HandleFunc("/api/health/full", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"health": map[string]string{"status": "HEALTH_OK"},
			"df": map[string]any{
				"stats": map[string]any{
					"total_bytes":      float64(1 << 30),
					"total_used_bytes": float64(1 << 28),
				},
			},
		})
	})

	// GET /api/osd → array
	mux.HandleFunc("/api/osd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"osd_info": map[string]int{"up": 1, "in": 1}},
			{"osd_info": map[string]int{"up": 1, "in": 1}},
			{"osd_info": map[string]int{"up": 0, "in": 0}},
		})
	})

	// GET /api/monitor
	mux.HandleFunc("/api/monitor", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		empty := map[string]any{}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mons":      []any{empty, empty, empty},
			"in_quorum": []any{empty, empty},
		})
	})

	// GET /api/cluster → cluster fsid used to pin the entity id.
	mux.HandleFunc("/api/cluster", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"fsid": "aabbccdd-1234-5678-abcd-ef0123456789",
		})
	})

	// GET /api/pool
	mux.HandleFunc("/api/pool", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"pool_name": "rbd",
				"stats": map[string]any{
					"objects": map[string]any{"latest": float64(100)},
					"stored":  map[string]any{"latest": float64(1024)},
					"rd_ops":  map[string]any{"latest": float64(50)},
					"wr_ops":  map[string]any{"latest": float64(25)},
				},
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	probe, err := NewCephProbe(map[string]interface{}{
		"endpoint":   srv.URL,
		"username":   "admin",
		"password":   "pass",
		"verify_tls": false,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCephProbe: %v", err)
	}
	return srv, probe.(*CephProbe)
}

// TestCollect_Up verifies that Collect returns senhub.ceph.up=1 when the
// fake server is reachable.
func TestCollect_Up(t *testing.T) {
	_, probe := fakeServer(t)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	byName := make(map[string]float64)
	for _, p := range points {
		byName[p.Name] = p.Value
	}

	if byName["senhub.ceph.up"] != 1 {
		t.Errorf("senhub.ceph.up = %v; want 1", byName["senhub.ceph.up"])
	}
	if byName["ceph.health.status"] != 2 {
		t.Errorf("ceph.health.status = %v; want 2 (HEALTH_OK)", byName["ceph.health.status"])
	}
	if byName["ceph.osd.total"] != 3 {
		t.Errorf("ceph.osd.total = %v; want 3", byName["ceph.osd.total"])
	}
	if byName["ceph.osd.up"] != 2 {
		t.Errorf("ceph.osd.up = %v; want 2", byName["ceph.osd.up"])
	}
	if byName["ceph.osd.in"] != 2 {
		t.Errorf("ceph.osd.in = %v; want 2", byName["ceph.osd.in"])
	}
	if byName["ceph.monitor.count"] != 3 {
		t.Errorf("ceph.monitor.count = %v; want 3", byName["ceph.monitor.count"])
	}
	if byName["ceph.monitor.quorum_count"] != 2 {
		t.Errorf("ceph.monitor.quorum_count = %v; want 2", byName["ceph.monitor.quorum_count"])
	}
}

// TestCollect_PoolMetrics verifies per-pool datapoints.
func TestCollect_PoolMetrics(t *testing.T) {
	_, probe := fakeServer(t)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	poolMetrics := map[string]float64{}
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "pool" && tag.Value == "rbd" {
				poolMetrics[dp.Name] = dp.Value
			}
		}
	}

	wantPool := map[string]float64{
		"ceph.pool.objects": 100,
		"ceph.pool.used":    1024,
		"ceph.pool.rd_ops":  50,
		"ceph.pool.wr_ops":  25,
	}
	for name, want := range wantPool {
		if got, ok := poolMetrics[name]; !ok {
			t.Errorf("missing pool metric %q", name)
		} else if got != want {
			t.Errorf("pool metric %q = %v; want %v", name, got, want)
		}
	}
}

// TestCollect_AuthFailure verifies that an unreachable endpoint emits
// senhub.ceph.up=0 without returning a collection error.
func TestCollect_AuthFailure(t *testing.T) {
	probe, err := NewCephProbe(map[string]interface{}{
		"endpoint":   "http://127.0.0.1:1", // nothing listening
		"username":   "admin",
		"password":   "pass",
		"verify_tls": false,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCephProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect should not return an error on auth failure; got: %v", err)
	}

	var up float64 = -1
	for _, dp := range points {
		if dp.Name == "senhub.ceph.up" {
			up = dp.Value
			break
		}
	}
	if up != 0 {
		t.Errorf("senhub.ceph.up = %v; want 0 when auth fails", up)
	}
}

// TestHealthStatus_Mapping verifies the numeric mapping for all health strings.
func TestHealthStatus_Mapping(t *testing.T) {
	cases := []struct {
		status string
		want   float64
	}{
		{"HEALTH_OK", 2},
		{"HEALTH_WARN", 1},
		{"HEALTH_ERR", 0},
		{"UNKNOWN", 0},
	}
	for _, c := range cases {
		got, ok := healthStatus[c.status]
		if c.status == "UNKNOWN" {
			if ok {
				t.Errorf("UNKNOWN should not be in healthStatus map")
			}
			continue
		}
		if !ok {
			t.Errorf("healthStatus[%q] not found", c.status)
			continue
		}
		if got != c.want {
			t.Errorf("healthStatus[%q] = %v; want %v", c.status, got, c.want)
		}
	}
}
