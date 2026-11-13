package proxmox

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
)

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(pveEnvelope(t, v))
}

// newPVE9Server serves a two-node cluster the way PVE 9 answers: the
// per-node qemu list has no diskread/diskwrite, /cluster/resources does.
func newPVE9Server(t *testing.T, resources http.HandlerFunc, extra func(mux *http.ServeMux)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]interface{}{
			{"node": "node-a", "status": "online"},
			{"node": "node-b", "status": "online"},
		})
	})
	for _, n := range []string{"node-a", "node-b"} {
		mux.HandleFunc("/api2/json/nodes/"+n+"/status", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]interface{}{"cpu": 0.1, "memory": map[string]interface{}{"used": 1, "total": 2}})
		})
		mux.HandleFunc("/api2/json/nodes/"+n+"/storage", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []interface{}{})
		})
		mux.HandleFunc("/api2/json/nodes/"+n+"/lxc", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []interface{}{})
		})
	}
	mux.HandleFunc("/api2/json/nodes/node-a/qemu", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []map[string]interface{}{
			{"vmid": 101, "name": "guest-web", "status": "running", "cpu": 0.07, "mem": 2147483648, "maxmem": 4294967296, "netin": 1000, "netout": 2000},
		})
	})
	mux.HandleFunc("/api2/json/nodes/node-b/qemu", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, []interface{}{})
	})
	mux.HandleFunc("/api2/json/cluster/resources", resources)
	// Routes registered by a test take precedence over the defaults above.
	over := http.NewServeMux()
	if extra != nil {
		extra(over)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, pattern := over.Handler(r); pattern != "" {
			h.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func resourcesReply(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "vm" {
			t.Errorf("cluster/resources must be asked with type=vm, got %q", r.URL.RawQuery)
		}
		writeJSON(t, w, []map[string]interface{}{
			{"id": "qemu/101", "type": "qemu", "vmid": 101, "name": "guest-web", "node": "node-a", "status": "running",
				"cpu": 0.07, "mem": 2147483648, "maxmem": 4294967296, "diskread": 987654321, "diskwrite": 123456789, "netin": 1000, "netout": 2000},
			{"id": "lxc/201", "type": "lxc", "vmid": 201, "name": "guest-ct", "node": "node-b", "status": "running",
				"cpu": 0.01, "mem": 1048576, "maxmem": 2097152, "diskread": 0, "diskwrite": 55, "netin": 1, "netout": 2},
			{"id": "qemu/102", "type": "qemu", "vmid": 102, "name": "guest-stopped", "node": "node-b", "status": "stopped",
				"maxmem": 1073741824, "netin": 5},
			{"id": "storage/node-a/local", "type": "storage", "node": "node-a"},
		})
	}
}

func newGuestProbe(t *testing.T, endpoint, node string) *ProxmoxProbe {
	t.Helper()
	return newGuestProbeWith(t, endpoint, node, nil)
}

func newGuestProbeWith(t *testing.T, endpoint, node string, extra map[string]interface{}) *ProxmoxProbe {
	t.Helper()
	cfg := map[string]interface{}{"endpoint": endpoint, "token_id": "monitor@pve!agent", "token_secret": "s", "verify_tls": false}
	for k, v := range extra {
		cfg[k] = v
	}
	if node != "" {
		cfg["node"] = node
	}
	p, err := NewProxmoxProbe(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}
	return p.(*ProxmoxProbe)
}

func vmPoints(points []data_store.DataPoint, vmid string) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "proxmox.vmid" && tg.Value == vmid {
				out[dp.Name] = dp.Value
			}
		}
	}
	return out
}

func TestCollect_GuestsFromClusterResources(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), nil)
	p := newGuestProbe(t, srv.URL, "")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	web := vmPoints(points, "101")
	if web["proxmox.vm.disk.read"] != 987654321 || web["proxmox.vm.disk.write"] != 123456789 {
		t.Errorf("disk counters must come from /cluster/resources, got %v", web)
	}
	if d := web["proxmox.vm.cpu.utilization"] - 7; d > 1e-9 || d < -1e-9 {
		t.Errorf("cpu utilization = %v, want 7", web["proxmox.vm.cpu.utilization"])
	}
	if web["proxmox.vm.status"] != 1 {
		t.Errorf("running guest status = %v, want 1", web["proxmox.vm.status"])
	}

	ct := vmPoints(points, "201")
	if v, ok := ct["proxmox.vm.disk.read"]; !ok || v != 0 {
		t.Errorf("a counter present as 0 must be emitted as 0, got %v (present=%v)", v, ok)
	}

	stopped := vmPoints(points, "102")
	for _, name := range []string{"proxmox.vm.disk.read", "proxmox.vm.disk.write", "proxmox.vm.cpu.utilization", "proxmox.vm.memory.used", "proxmox.vm.network.out"} {
		if _, ok := stopped[name]; ok {
			t.Errorf("%s is absent from the reply and must produce no point", name)
		}
	}
	if stopped["proxmox.vm.status"] != 0 || stopped["proxmox.vm.network.in"] != 5 {
		t.Errorf("stopped guest: got %v", stopped)
	}

	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "proxmox.vm.type" && dp.Name == "proxmox.vm.status" && tg.Value != "qemu" && tg.Value != "lxc" {
				t.Errorf("unexpected proxmox.vm.type %q", tg.Value)
			}
		}
	}
}

func TestCollect_GuestsNodeFilter(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), nil)
	p := newGuestProbe(t, srv.URL, "node-a")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(vmPoints(points, "101")) == 0 {
		t.Error("guest of node-a missing")
	}
	if len(vmPoints(points, "201")) != 0 || len(vmPoints(points, "102")) != 0 {
		t.Error("guests of node-b must be excluded by the node filter")
	}
}

func TestCollect_GuestsFallbackWhenClusterResourcesFails(t *testing.T) {
	var calls atomic.Int32
	srv := newPVE9Server(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
	}, nil)
	p := newGuestProbe(t, srv.URL, "")

	for i := 0; i < 2; i++ {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		web := vmPoints(points, "101")
		if web["proxmox.vm.network.in"] != 1000 {
			t.Errorf("fallback must read the per-node list, got %v", web)
		}
		if _, ok := web["proxmox.vm.disk.read"]; ok {
			t.Error("the per-node list has no diskread: no point expected, not 0")
		}
	}
	if calls.Load() != 2 {
		t.Errorf("cluster/resources asked %d times, want once per cycle", calls.Load())
	}
	if !p.resourcesDegraded.Load() {
		t.Error("probe should be flagged degraded while cluster/resources fails")
	}
}

var previewAll = map[string]interface{}{"enable_cluster": true, "enable_ha": true, "enable_replication": true}
