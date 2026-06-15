package envoy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *EnvoyProbe {
	t.Helper()
	probe, err := NewEnvoyProbe(config, testLogger())
	if err != nil {
		t.Fatalf("NewEnvoyProbe: %v", err)
	}
	p, ok := probe.(*EnvoyProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("envoy-edge")
	return p
}

// collectByName returns a map of metric name → value from a single Collect cycle.
func collectByName(t *testing.T, p *EnvoyProbe) map[string]float64 {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float64{}
	for _, dp := range points {
		got[dp.Name] = dp.Value
	}
	return got
}

// minimalEnvoyStats is a valid Prometheus-text Envoy stats response
// covering every metric this probe is interested in.
const minimalEnvoyStats = `# HELP envoy_server_uptime Server uptime in seconds.
# TYPE envoy_server_uptime counter
envoy_server_uptime 3600
# HELP envoy_server_memory_allocated Current memory allocated in bytes.
# TYPE envoy_server_memory_allocated gauge
envoy_server_memory_allocated 104857600
# HELP envoy_server_memory_heap_size Current heap size in bytes.
# TYPE envoy_server_memory_heap_size gauge
envoy_server_memory_heap_size 209715200
# HELP envoy_listener_downstream_cx_total Total downstream connections.
# TYPE envoy_listener_downstream_cx_total counter
envoy_listener_downstream_cx_total{envoy_listener_address="0.0.0.0_80"} 1000
envoy_listener_downstream_cx_total{envoy_listener_address="0.0.0.0_443"} 500
# HELP envoy_listener_downstream_cx_active Active downstream connections.
# TYPE envoy_listener_downstream_cx_active gauge
envoy_listener_downstream_cx_active{envoy_listener_address="0.0.0.0_80"} 42
envoy_listener_downstream_cx_active{envoy_listener_address="0.0.0.0_443"} 18
# HELP envoy_http_downstream_rq_total Total HTTP downstream requests.
# TYPE envoy_http_downstream_rq_total counter
envoy_http_downstream_rq_total{envoy_http_conn_manager_prefix="ingress_http"} 5000
# HELP envoy_cluster_upstream_cx_total Total upstream connections per cluster.
# TYPE envoy_cluster_upstream_cx_total counter
envoy_cluster_upstream_cx_total{envoy_cluster_name="backend"} 200
envoy_cluster_upstream_cx_total{envoy_cluster_name="auth"} 50
# HELP envoy_cluster_upstream_rq_total Total upstream requests per cluster.
# TYPE envoy_cluster_upstream_rq_total counter
envoy_cluster_upstream_rq_total{envoy_cluster_name="backend"} 4800
envoy_cluster_upstream_rq_total{envoy_cluster_name="auth"} 200
# HELP envoy_cluster_upstream_rq_time_sum Sum of upstream request latency per cluster.
# TYPE envoy_cluster_upstream_rq_time_sum counter
envoy_cluster_upstream_rq_time_sum{envoy_cluster_name="backend"} 96000
envoy_cluster_upstream_rq_time_sum{envoy_cluster_name="auth"} 4000
`

func TestCollect_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" || r.URL.Query().Get("format") != "prometheus" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(minimalEnvoyStats)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"endpoint": srv.URL,
	})

	got := collectByName(t, p)

	// up must be 1
	if got["senhub.envoy.up"] != 1 {
		t.Errorf("senhub.envoy.up = %v, want 1", got["senhub.envoy.up"])
	}
	// server metrics
	if got["envoy.server.uptime"] != 3600 {
		t.Errorf("envoy.server.uptime = %v, want 3600", got["envoy.server.uptime"])
	}
	if got["envoy.server.memory.allocated"] != 104857600 {
		t.Errorf("envoy.server.memory.allocated = %v, want 104857600", got["envoy.server.memory.allocated"])
	}
	// listener downstream: sum of both listeners
	if got["envoy.listener.downstream.connections.total"] != 1500 {
		t.Errorf("envoy.listener.downstream.connections.total = %v, want 1500", got["envoy.listener.downstream.connections.total"])
	}
	if got["envoy.listener.downstream.connections.active"] != 60 {
		t.Errorf("envoy.listener.downstream.connections.active = %v, want 60", got["envoy.listener.downstream.connections.active"])
	}
	// http downstream
	if got["envoy.http.downstream.requests.total"] != 5000 {
		t.Errorf("envoy.http.downstream.requests.total = %v, want 5000", got["envoy.http.downstream.requests.total"])
	}
}

func TestCollect_Unreachable(t *testing.T) {
	// Point at a closed server — no listener on that port.
	p := newTestProbe(t, map[string]interface{}{
		"endpoint": "http://127.0.0.1:19901",
	})

	got := collectByName(t, p)

	if got["senhub.envoy.up"] != 0 {
		t.Errorf("senhub.envoy.up = %v, want 0 when endpoint is unreachable", got["senhub.envoy.up"])
	}
	// Collect must not error — unreachable is a measurement, not a fatal.
}

func TestCollect_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"endpoint": srv.URL,
	})

	got := collectByName(t, p)
	if got["senhub.envoy.up"] != 0 {
		t.Errorf("senhub.envoy.up = %v, want 0 when HTTP 503", got["senhub.envoy.up"])
	}
}

func TestNewEnvoyProbe_Defaults(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})

	if p.config.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", p.config.Endpoint, defaultEndpoint)
	}
	if p.config.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", p.config.Timeout, defaultTimeout)
	}
	if p.config.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", p.config.Interval, defaultInterval)
	}
}

func TestNewEnvoyProbe_CustomConfig(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"endpoint": "http://envoy.internal:9901",
		"timeout":  5,
		"interval": 60,
	})

	if p.config.Endpoint != "http://envoy.internal:9901" {
		t.Errorf("endpoint = %q", p.config.Endpoint)
	}
	if p.config.Timeout.Seconds() != 5 {
		t.Errorf("timeout = %v", p.config.Timeout)
	}
	if p.config.Interval.Seconds() != 60 {
		t.Errorf("interval = %v", p.config.Interval)
	}
}

// TestCollect_PerClusterMetrics verifies that per-cluster series are
// produced with the correct cluster tag and that each cluster is separate.
func TestCollect_PerClusterMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(minimalEnvoyStats)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	p.SetName("envoy-test")

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Collect per-cluster connection totals.
	clusterCxTotal := map[string]float64{}
	for _, dp := range pts {
		if dp.Name != "envoy.cluster.upstream.connections.total" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "cluster" {
				clusterCxTotal[tg.Value] = dp.Value
			}
		}
	}

	if clusterCxTotal["backend"] != 200 {
		t.Errorf("backend upstream cx total = %v, want 200", clusterCxTotal["backend"])
	}
	if clusterCxTotal["auth"] != 50 {
		t.Errorf("auth upstream cx total = %v, want 50", clusterCxTotal["auth"])
	}
}

func TestGetTargetStrategies(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	strats := p.GetTargetStrategies()
	if len(strats) == 0 {
		t.Error("GetTargetStrategies returned empty slice")
	}
}
