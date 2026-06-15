package proxmox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/cliArgs"
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

func TestEntitySource_Refresh(t *testing.T) {
	cfg := probeConfig{
		Endpoint: "https://pve:8006",
	}
	log := logger.NewModuleLogger(testLogger(), "test")
	src := newProxmoxEntitySource(cfg, log)

	_, ok := src.Observe()
	if ok {
		t.Error("should not be ready before first refresh")
	}

	nodes := []pveNode{
		{Node: "pve1", Status: "online"},
		{Node: "pve2", Status: "online"},
	}
	src.refresh(nodes)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("should be ready after refresh")
	}
	if len(obs.Entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(obs.Entities))
	}
	for _, e := range obs.Entities {
		if e.Type != "service.instance" {
			t.Errorf("entity type: got %q, want service.instance", e.Type)
		}
		if _, ok := e.ID["service.instance.id"]; !ok {
			t.Error("entity ID missing service.instance.id key")
		}
	}
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
