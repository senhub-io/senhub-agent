package proxmox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

// pveEnvelope wraps Proxmox API responses in the standard {"data": ...} envelope.
func pveEnvelope(t *testing.T, v interface{}) []byte {
	t.Helper()
	env := map[string]interface{}{"data": v}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling test envelope: %v", err)
	}
	return b
}

// newTestServer builds a minimal Proxmox API stub that serves fixture data.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api2/json/cluster/status", func(w http.ResponseWriter, r *http.Request) {
		// Return a clustered install with cluster name "test-cluster".
		items := []map[string]interface{}{
			{"type": "cluster", "name": "test-cluster"},
			{"type": "node", "name": "pve1"},
			{"type": "node", "name": "pve2"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, items))
	})

	mux.HandleFunc("/api2/json/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes := []map[string]interface{}{
			{"node": "pve1", "status": "online"},
			{"node": "pve2", "status": "online"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, nodes))
	})

	mux.HandleFunc("/api2/json/nodes/pve1/status", func(w http.ResponseWriter, r *http.Request) {
		s := map[string]interface{}{
			"cpu": 0.42,
			"memory": map[string]interface{}{
				"used":  int64(4 * 1024 * 1024 * 1024),
				"total": int64(32 * 1024 * 1024 * 1024),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, s))
	})

	mux.HandleFunc("/api2/json/nodes/pve2/status", func(w http.ResponseWriter, r *http.Request) {
		s := map[string]interface{}{
			"cpu": 0.10,
			"memory": map[string]interface{}{
				"used":  int64(2 * 1024 * 1024 * 1024),
				"total": int64(16 * 1024 * 1024 * 1024),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, s))
	})

	mux.HandleFunc("/api2/json/nodes/pve1/qemu", func(w http.ResponseWriter, r *http.Request) {
		vms := []map[string]interface{}{
			{
				"vmid": 100, "name": "vm-web", "status": "running",
				"cpu": 0.05, "mem": int64(1 << 30), "maxmem": int64(2 << 30),
				"diskread": int64(1024), "diskwrite": int64(2048),
				"netin": int64(512), "netout": int64(256),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, vms))
	})

	mux.HandleFunc("/api2/json/nodes/pve2/qemu", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, []interface{}{}))
	})

	mux.HandleFunc("/api2/json/nodes/pve1/lxc", func(w http.ResponseWriter, r *http.Request) {
		ctrs := []map[string]interface{}{
			{
				"vmid": 200, "name": "ct-db", "status": "running",
				"cpu": 0.02, "mem": int64(512 << 20), "maxmem": int64(1 << 30),
				"diskread": int64(0), "diskwrite": int64(0),
				"netin": int64(64), "netout": int64(32),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, ctrs))
	})

	mux.HandleFunc("/api2/json/nodes/pve2/lxc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, []interface{}{}))
	})

	storageHandler := func(w http.ResponseWriter, r *http.Request) {
		st := []map[string]interface{}{
			{"storage": "local-lvm", "used": int64(50 << 30), "total": int64(500 << 30)},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pveEnvelope(t, st))
	}
	mux.HandleFunc("/api2/json/nodes/pve1/storage", storageHandler)
	mux.HandleFunc("/api2/json/nodes/pve2/storage", storageHandler)

	return httptest.NewServer(mux)
}

func TestParseConfig_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"endpoint":     "https://pve:8006",
				"token_id":     "root@pam!mon",
				"token_secret": "uuid-value",
			},
			wantErr: false,
		},
		{
			name:    "missing endpoint",
			cfg:     map[string]interface{}{"token_id": "root@pam!mon", "token_secret": "x"},
			wantErr: true,
		},
		{
			name:    "missing token_id",
			cfg:     map[string]interface{}{"endpoint": "https://pve:8006", "token_secret": "x"},
			wantErr: true,
		},
		{
			name:    "missing token_secret",
			cfg:     map[string]interface{}{"endpoint": "https://pve:8006", "token_id": "root@pam!mon"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint":     "https://pve:8006",
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("interval: got %v, want %v", cfg.Interval, defaultInterval)
	}
	if !cfg.VerifyTLS {
		t.Error("verify_tls should default to true")
	}
}

func TestCollect_AllNodes(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected datapoints, got none")
	}

	byName := make(map[string]bool)
	for _, dp := range points {
		byName[dp.Name] = true
	}

	required := []string{
		"proxmox.node.cpu.utilization",
		"proxmox.node.memory.used",
		"proxmox.node.memory.total",
		"proxmox.node.status",
		"proxmox.vm.cpu.utilization",
		"proxmox.vm.memory.used",
		"proxmox.vm.memory.total",
		"proxmox.vm.disk.read",
		"proxmox.vm.disk.write",
		"proxmox.vm.network.in",
		"proxmox.vm.network.out",
		"proxmox.vm.status",
		"proxmox.storage.used",
		"proxmox.storage.total",
	}
	for _, name := range required {
		if !byName[name] {
			t.Errorf("missing metric %q in Collect output", name)
		}
	}
}

// TestCollect_UpMetric_Healthy verifies that senhub.proxmox.up=1 is emitted
// when the Proxmox API answers the node list successfully (#469).
func TestCollect_UpMetric_Healthy(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var found bool
	for _, dp := range points {
		if dp.Name == "senhub.proxmox.up" {
			found = true
			if dp.Value != 1 {
				t.Errorf("senhub.proxmox.up: got %v, want 1", dp.Value)
			}
		}
	}
	if !found {
		t.Error("senhub.proxmox.up was not emitted on healthy cluster")
	}
}

// TestCollect_UpMetric_APIError verifies that senhub.proxmox.up=0 is emitted
// (not suppressed) when the API server is unreachable (#469). Collect must
// return nil error so the up=0 datapoint reaches all sinks, making an
// unreachable API distinguishable from a healthy but empty cluster.
func TestCollect_UpMetric_APIError(t *testing.T) {
	// Closed server: every request fails at the transport layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     url,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
		"timeout":      1,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() must not return an error on API failure (got: %v); up=0 must flow to sinks", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints; senhub.proxmox.up must still be emitted on API error")
	}

	var found bool
	for _, dp := range points {
		if dp.Name == "senhub.proxmox.up" {
			found = true
			if dp.Value != 0 {
				t.Errorf("senhub.proxmox.up: got %v, want 0 on API error", dp.Value)
			}
		}
	}
	if !found {
		t.Error("senhub.proxmox.up was not emitted on API error")
	}
}

func TestCollect_NodeFilter(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
		"node":         "pve1",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "proxmox.node" && tag.Value == "pve2" {
				t.Errorf("node filter pve1 should exclude pve2, but found datapoint %q tagged pve2", dp.Name)
			}
		}
	}
}

func TestCollect_VMTags(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
		"node":         "pve1",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name != "proxmox.vm.cpu.utilization" {
			continue
		}
		tagMap := make(map[string]string)
		for _, tg := range dp.Tags {
			tagMap[tg.Key] = tg.Value
		}
		if tagMap["proxmox.vm.type"] == "" {
			t.Error("proxmox.vm.type tag missing on vm metric")
		}
		if tagMap["proxmox.vmid"] == "" {
			t.Error("proxmox.vmid tag missing on vm metric")
		}
		if tagMap["proxmox.vm.name"] == "" {
			t.Error("proxmox.vm.name tag missing on vm metric")
		}
	}
}

func TestCollect_ProbeName(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
		"node":         "pve1",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}
	// SetName is normally called by the probe framework.
	probe.(*ProxmoxProbe).SetName("my-proxmox")

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		tagMap := make(map[string]string)
		for _, tg := range dp.Tags {
			tagMap[tg.Key] = tg.Value
		}
		if tagMap["probe_name"] != "my-proxmox" {
			t.Errorf("expected probe_name=my-proxmox, got %q", tagMap["probe_name"])
		}
		if tagMap["probe_type"] != ProbeType {
			t.Errorf("expected probe_type=%s, got %q", ProbeType, tagMap["probe_type"])
		}
	}
}

func TestCollect_VMStatus_Running(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
		"node":         "pve1",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "proxmox.vm.status" {
			if dp.Value != 1.0 {
				t.Errorf("running VM should have proxmox.vm.status=1, got %v", dp.Value)
			}
		}
	}
}

func TestEntitySource_Refresh_SingleEntity(t *testing.T) {
	cfg := probeConfig{
		Endpoint: "https://pve:8006",
	}
	log := logger.NewModuleLogger(testLogger(), "test")
	src := newProxmoxEntitySource(cfg, log)

	_, ok := src.Observe()
	if ok {
		t.Error("should not be ready before first refresh")
	}

	// Clustered install: cluster name is available.
	src.refresh("test-cluster")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("should be ready after refresh")
	}
	// ONE entity for the PVE management surface (not one per node).
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity (PVE surface), got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "service.instance" {
		t.Errorf("entity type: got %q, want service.instance", e.Type)
	}
	id, ok := e.ID["service.instance.id"]
	if !ok {
		t.Fatal("entity ID missing service.instance.id key")
	}
	if id != "proxmox:test-cluster" {
		t.Errorf("service.instance.id = %q, want \"proxmox:test-cluster\"", id)
	}
	if e.Attributes["service.name"] != "proxmox" {
		t.Errorf("service.name attribute = %q, want \"proxmox\"", e.Attributes["service.name"])
	}
	if e.Attributes["server.address"] != "https://pve:8006" {
		t.Errorf("server.address attribute = %q, want \"https://pve:8006\"", e.Attributes["server.address"])
	}
}

func TestEntitySource_Refresh_InstanceNameOverride(t *testing.T) {
	cfg := probeConfig{
		Endpoint:     "https://pve:8006",
		InstanceName: "my-pve",
	}
	log := logger.NewModuleLogger(testLogger(), "test")
	src := newProxmoxEntitySource(cfg, log)

	src.refresh("some-cluster")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("should be ready after refresh")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	id := obs.Entities[0].ID["service.instance.id"]
	if id != "my-pve" {
		t.Errorf("service.instance.id = %q, want \"my-pve\" (instance_name takes precedence)", id)
	}
}

func TestEntitySource_Refresh_StandaloneNoAgent(t *testing.T) {
	cfg := probeConfig{
		Endpoint: "https://pve:8006",
	}
	log := logger.NewModuleLogger(testLogger(), "test")
	src := newProxmoxEntitySource(cfg, log)

	// Standalone install: no cluster name, no agent ID set.
	src.refresh("")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("should be ready after refresh")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	id, _ := obs.Entities[0].ID["service.instance.id"].(string)
	if id == "" {
		t.Error("service.instance.id must not be empty")
	}
	// With no cluster name the id falls back to "proxmox@<host.id>" (the
	// agent's machine-id), or the "proxmox@unknown" sentinel when even the
	// host id is unavailable — either way non-empty and stable within a cycle.
}

func TestGetInterval(t *testing.T) {
	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     "https://pve:8006",
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"interval":     120,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}
	if probe.GetInterval() != 120*time.Second {
		t.Errorf("expected 120s interval, got %v", probe.GetInterval())
	}
}
