package consul

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// newTestLogger returns a minimal logger suitable for unit tests.
func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

// promFixture is a minimal Prometheus text exposition with the Consul
// metrics the probe expects.
const promFixture = `# HELP consul_catalog_registered_services Number of services registered
# TYPE consul_catalog_registered_services gauge
consul_catalog_registered_services 5
# HELP consul_serf_lan_members Number of LAN members
# TYPE consul_serf_lan_members gauge
consul_serf_lan_members 3
# HELP consul_raft_commitTime_sum Raft commit time sum (ms)
# TYPE consul_raft_commitTime_sum gauge
consul_raft_commitTime_sum 120.5
# HELP consul_raft_commitTime_count Raft commit time count
# TYPE consul_raft_commitTime_count gauge
consul_raft_commitTime_count 10
# HELP consul_rpc_request Total RPC requests
# TYPE consul_rpc_request counter
consul_rpc_request 42
# HELP consul_dns_domain_query_count DNS queries
# TYPE consul_dns_domain_query_count counter
consul_dns_domain_query_count 100
`

// agentSelfFixture represents a Consul leader node with a stable NodeID.
const agentSelfFixture = `{
  "Config": {
    "NodeID": "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
    "Version": "1.17.0"
  },
  "Stats": {
    "consul": {
      "leader": "true"
    }
  }
}`

// agentSelfNonLeaderFixture represents a non-leader node with a stable NodeID.
const agentSelfNonLeaderFixture = `{
  "Config": {
    "NodeID": "11112222-3333-4444-5555-666677778888",
    "Version": "1.17.0"
  },
  "Stats": {
    "consul": {
      "leader": "false"
    }
  }
}`

// healthFixture builds a JSON array of N anonymous check objects.
func healthFixture(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = `{}`
	}
	b, _ := json.Marshal(make([]json.RawMessage, n))
	_ = b
	out := "["
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	out += "]"
	return out
}

// newTestServer builds an httptest.Server that serves the probe's four
// API paths. The caller is responsible for closing it.
func newTestServer(t *testing.T, promBody, selfBody string, critN, warnN, passN int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/agent/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(promBody))
	})
	mux.HandleFunc("/v1/agent/self", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(selfBody))
	})
	mux.HandleFunc("/v1/health/state/critical", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(healthFixture(critN)))
	})
	mux.HandleFunc("/v1/health/state/warning", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(healthFixture(warnN)))
	})
	mux.HandleFunc("/v1/health/state/passing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(healthFixture(passN)))
	})
	return httptest.NewServer(mux)
}

func TestNewConsulProbe_Defaults(t *testing.T) {
	p, err := NewConsulProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewConsulProbe: unexpected error: %v", err)
	}
	cp := p.(*ConsulProbe)
	if cp.cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint: got %q, want %q", cp.cfg.Endpoint, defaultEndpoint)
	}
	if cp.cfg.Interval != defaultInterval {
		t.Errorf("interval: got %v, want %v", cp.cfg.Interval, defaultInterval)
	}
	if cp.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout: got %v, want %v", cp.cfg.Timeout, defaultTimeout)
	}
}

func TestNewConsulProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"endpoint": "http://consul.example.com:8500",
		"token":    "my-secret-token",
		"timeout":  30,
		"interval": 60,
	}
	p, err := NewConsulProbe(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewConsulProbe: unexpected error: %v", err)
	}
	cp := p.(*ConsulProbe)
	if cp.cfg.Endpoint != "http://consul.example.com:8500" {
		t.Errorf("endpoint: got %q", cp.cfg.Endpoint)
	}
	if cp.cfg.Token != "my-secret-token" {
		t.Errorf("token: got %q", cp.cfg.Token)
	}
	if cp.cfg.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v", cp.cfg.Timeout)
	}
	if cp.cfg.Interval != 60*time.Second {
		t.Errorf("interval: got %v", cp.cfg.Interval)
	}
}

func TestConsulProbe_ProbeType(t *testing.T) {
	p, _ := NewConsulProbe(map[string]interface{}{}, newTestLogger())
	cp := p.(*ConsulProbe)
	if cp.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", cp.GetProbeType(), ProbeType)
	}
}

func TestConsulProbe_Collect_Up(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfFixture, 1, 0, 10)
	defer srv.Close()

	p, err := NewConsulProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewConsulProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no points")
	}

	// The first point should be senhub.consul.up = 1
	upPoint := points[0]
	if upPoint.Name != "senhub.consul.up" {
		t.Errorf("first point name: got %q, want %q", upPoint.Name, "senhub.consul.up")
	}
	if upPoint.Value != 1 {
		t.Errorf("senhub.consul.up: got %v, want 1", upPoint.Value)
	}
}

func TestConsulProbe_Collect_MetricValues(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfFixture, 2, 1, 15)
	defer srv.Close()

	p, _ := NewConsulProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := make(map[string]float32)
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	// Catalog services
	if v, ok := byName["consul.catalog.services"]; !ok || v != 5 {
		t.Errorf("consul.catalog.services: got %v (present=%v), want 5", v, ok)
	}
	// Serf members
	if v, ok := byName["consul.serf.members"]; !ok || v != 3 {
		t.Errorf("consul.serf.members: got %v (present=%v), want 3", v, ok)
	}
	// Raft commit time mean: 120.5 / 10 = 12.05
	if v, ok := byName["consul.raft.commit.time"]; !ok || v < 12.04 || v > 12.06 {
		t.Errorf("consul.raft.commit.time: got %v (present=%v), want ~12.05", v, ok)
	}
	// RPC requests
	if v, ok := byName["consul.rpc.requests"]; !ok || v != 42 {
		t.Errorf("consul.rpc.requests: got %v (present=%v), want 42", v, ok)
	}
	// DNS queries
	if v, ok := byName["consul.dns.queries"]; !ok || v != 100 {
		t.Errorf("consul.dns.queries: got %v (present=%v), want 100", v, ok)
	}
	// Leader
	if v, ok := byName["consul.leader"]; !ok || v != 1 {
		t.Errorf("consul.leader: got %v (present=%v), want 1 (leader)", v, ok)
	}
}

func TestConsulProbe_Collect_NonLeader(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfNonLeaderFixture, 0, 0, 5)
	defer srv.Close()

	p, _ := NewConsulProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	points, _ := p.Collect()

	for _, dp := range points {
		if dp.Name == "consul.leader" && dp.Value != 0 {
			t.Errorf("consul.leader: got %v, want 0 (non-leader)", dp.Value)
		}
	}
}

func TestConsulProbe_Collect_HealthChecks(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfFixture, 3, 1, 20)
	defer srv.Close()

	p, _ := NewConsulProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	points, _ := p.Collect()

	stateValues := make(map[string]float32)
	for _, dp := range points {
		if dp.Name == "consul.health.checks" {
			for _, tag := range dp.Tags {
				if tag.Key == "state" {
					stateValues[tag.Value] = dp.Value
				}
			}
		}
	}

	cases := map[string]float32{
		"critical": 3,
		"warning":  1,
		"passing":  20,
	}
	for state, want := range cases {
		if got, ok := stateValues[state]; !ok || got != want {
			t.Errorf("consul.health.checks{state=%s}: got %v (present=%v), want %v", state, got, ok, want)
		}
	}
}

func TestConsulProbe_Collect_UpIsZeroOnFailure(t *testing.T) {
	// Point to a server that doesn't exist.
	p, _ := NewConsulProbe(map[string]interface{}{"endpoint": "http://127.0.0.1:19999"}, newTestLogger())
	points, err := p.Collect()

	// Collect must never return an error.
	if err != nil {
		t.Fatalf("Collect must return nil error on unreachable agent, got: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no points")
	}
	// First point is up=0.
	if points[0].Name != "senhub.consul.up" {
		t.Errorf("first point: got %q, want senhub.consul.up", points[0].Name)
	}
	if points[0].Value != 0 {
		t.Errorf("senhub.consul.up on failure: got %v, want 0", points[0].Value)
	}
}

func TestConsulProbe_Token_SentAsHeader(t *testing.T) {
	receivedToken := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Consul-Token")
		switch r.URL.Path {
		case "/v1/agent/metrics":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(""))
		case "/v1/agent/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(agentSelfNonLeaderFixture))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	p, _ := NewConsulProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"token":    "test-acl-token",
	}, newTestLogger())
	_, _ = p.Collect()

	if receivedToken != "test-acl-token" {
		t.Errorf("X-Consul-Token: got %q, want %q", receivedToken, "test-acl-token")
	}
}

func TestConsulProbe_EnrichedWithProbeName(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfFixture, 0, 0, 1)
	defer srv.Close()

	p, _ := NewConsulProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	// Give the probe a name so EnrichDataPointsWithProbeName has something to set.
	p.(*ConsulProbe).BaseProbe.SetName("consul-test")
	points, _ := p.Collect()

	for _, dp := range points {
		hasProbeName := false
		for _, tag := range dp.Tags {
			if tag.Key == "probe_name" {
				hasProbeName = true
				break
			}
		}
		if !hasProbeName {
			t.Errorf("point %q missing probe_name tag", dp.Name)
		}
	}
}
