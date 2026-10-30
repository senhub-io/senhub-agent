package proxmox

import (
	"bytes"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func haTestFilter(points []data_store.DataPoint) []data_store.DataPoint {
	out := make([]data_store.DataPoint, 0, len(points))
	for _, dp := range points {
		if strings.HasPrefix(dp.Name, "proxmox.ha.") {
			out = append(out, dp)
		}
	}
	return out
}

func haTestQuorumStates(points []data_store.DataPoint) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range points {
		if dp.Name != "proxmox.ha.quorum.status" {
			continue
		}
		tm := tagsMap(dp)
		out[tm["proxmox.ha.quorum.state"]] = dp.Value
	}
	return out
}

func haTestResourceStates(points []data_store.DataPoint) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range points {
		if dp.Name != "proxmox.ha.resource.status" {
			continue
		}
		tm := tagsMap(dp)
		out[tm["proxmox.ha.resource"]+"|"+tm["proxmox.ha.resource.state"]] = dp.Value
	}
	return out
}

func haTestFindResourcePoint(points []data_store.DataPoint, resource, state string) (data_store.DataPoint, bool) {
	for _, dp := range points {
		if dp.Name != "proxmox.ha.resource.status" {
			continue
		}
		tm := tagsMap(dp)
		if tm["proxmox.ha.resource"] == resource && tm["proxmox.ha.resource.state"] == state {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

func TestCollect_HAStatusNormal(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"id": "quorum", "type": "quorum", "node": "n1", "quorate": 1, "status": "OK"},
				{"id": "master", "type": "master", "node": "n1", "status": "OK"},
				{"id": "service:vm:100", "type": "service", "sid": "vm:100", "node": "n1", "state": "started", "request_state": "started", "status": "vm:100, node = n1, state = started"},
				{"id": "service:ct:200", "type": "service", "sid": "ct:200", "node": "n2", "state": "error", "request_state": "started", "status": "ct:200, node = n2, state = error"},
				{"id": "service:vm:300", "type": "service", "sid": "vm:300", "node": "n3", "state": "weird"},
			})
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	ha := haTestFilter(points)
	for _, dp := range ha {
		if dp.Name != "proxmox.ha.quorum.status" && dp.Name != "proxmox.ha.resource.status" {
			t.Errorf("unexpected HA point name %q", dp.Name)
		}
	}

	q := haTestQuorumStates(ha)
	if len(q) != 2 || q["ok"] != 1 || q["lost"] != 0 {
		t.Errorf("quorum states = %v, want ok=1 lost=0", q)
	}

	r := haTestResourceStates(ha)
	if len(r) != 2*len(haResourceStates) {
		t.Errorf("resource status series count = %d, want %d: %v", len(r), 2*len(haResourceStates), r)
	}
	for _, tc := range []struct {
		sid, current string
	}{
		{"vm:100", "started"},
		{"ct:200", "error"},
	} {
		for _, state := range haResourceStates {
			key := tc.sid + "|" + state
			want := 0.0
			if state == tc.current {
				want = 1.0
			}
			if got, ok := r[key]; !ok {
				t.Errorf("missing %s", key)
			} else if got != want {
				t.Errorf("%s = %v, want %v", key, got, want)
			}
		}
	}
	for _, state := range haResourceStates {
		if _, ok := r["vm:300|"+state]; ok {
			t.Errorf("vm:300 with unknown state emitted for %s", state)
		}
	}

	if dp, ok := haTestFindResourcePoint(ha, "vm:100", "started"); !ok {
		t.Fatal("vm:100 started point missing")
	} else {
		tm := tagsMap(dp)
		if tm["proxmox.ha.resource"] != "vm:100" || tm["proxmox.vmid"] != "100" || tm["proxmox.node"] != "n1" || tm["metric_type"] != "ha" {
			t.Errorf("vm:100 tags = %v", tm)
		}
	}
	if dp, ok := haTestFindResourcePoint(ha, "ct:200", "error"); !ok {
		t.Fatal("ct:200 error point missing")
	} else {
		tm := tagsMap(dp)
		if tm["proxmox.ha.resource"] != "ct:200" || tm["proxmox.vmid"] != "200" || tm["proxmox.node"] != "n2" || tm["metric_type"] != "ha" {
			t.Errorf("ct:200 tags = %v", tm)
		}
	}
}

func TestCollect_HAQuorumLost(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"id": "quorum", "type": "quorum", "node": "n1", "quorate": 0, "status": "OK"},
			})
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	ha := haTestFilter(points)
	q := haTestQuorumStates(ha)
	if len(q) != 2 || q["ok"] != 0 || q["lost"] != 1 {
		t.Errorf("quorum states = %v, want ok=0 lost=1", q)
	}
	if len(haTestResourceStates(ha)) != 0 {
		t.Errorf("unexpected resource series: %v", haTestResourceStates(ha))
	}
}

func TestCollect_HAEmptyList(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []interface{}{})
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	p.moduleLogger = &logger.ModuleLogger{Logger: &zl}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ha := haTestFilter(points); len(ha) != 0 {
		t.Errorf("no HA points expected, got %v", ha)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `"level":"warn"`) && strings.Contains(line, "cluster/ha") {
			t.Errorf("unexpected HA warn line: %s", line)
		}
	}
}

func TestCollect_HAStatusError(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	p.moduleLogger = &logger.ModuleLogger{Logger: &zl}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ha := haTestFilter(points); len(ha) != 0 {
		t.Errorf("no HA points expected, got %v", ha)
	}

	up := false
	for _, dp := range points {
		if dp.Name == "senhub.proxmox.up" && dp.Value == 1 {
			up = true
			break
		}
	}
	if !up {
		t.Error("senhub.proxmox.up not reported as 1")
	}

	warns := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `"level":"warn"`) && strings.Contains(line, "cluster/ha") {
			warns++
		}
	}
	if warns != 1 {
		t.Errorf("HA warn lines = %d, want 1; log:\n%s", warns, buf.String())
	}
}

func TestCollect_HAStatusForbidden(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	p.moduleLogger = &logger.ModuleLogger{Logger: &zl}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ha := haTestFilter(points); len(ha) != 0 {
		t.Errorf("no HA points expected, got %v", ha)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `"level":"warn"`) && strings.Contains(line, "cluster/ha") {
			t.Errorf("unexpected HA warn line: %s", line)
		}
	}
}

// The reply of a standalone PVE 9.2 node, as captured on a recette host: a
// quorum entry and a fencing entry, no service. quorate is an integer and
// the entries carry id and status, no sid or state.
func TestCollect_HAStandaloneNodeReply(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"id": "quorum", "node": "pve1", "quorate": 1, "status": "OK", "type": "quorum"},
				{"id": "fencing", "node": "pve1", "type": "fencing", "armed-state": "standby", "status": "standby (CRM watchdog standby)"},
			})
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := haTestQuorumStates(points)
	if got["ok"] != 1 || got["lost"] != 0 || len(got) != 2 {
		t.Errorf("quorum states = %v, want ok=1 lost=0", got)
	}
	if res := haTestResourceStates(points); len(res) != 0 {
		t.Errorf("a fencing entry must not become an HA resource: %v", res)
	}
}

func TestCollect_PreviewHAAndReplicationOffByDefaultCallNothing(t *testing.T) {
	var haCalls, replCalls atomic.Int32
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/ha/status/current", func(w http.ResponseWriter, r *http.Request) {
			haCalls.Add(1)
			writeJSON(t, w, []map[string]interface{}{
				{"id": "quorum", "type": "quorum", "node": "n1", "quorate": 1},
				{"id": "service:vm:100", "type": "service", "sid": "vm:100", "node": "n1", "state": "started"},
			})
		})
		for _, n := range []string{"node-a", "node-b"} {
			mux.HandleFunc("/api2/json/nodes/"+n+"/replication", func(w http.ResponseWriter, r *http.Request) {
				replCalls.Add(1)
				writeJSON(t, w, []map[string]interface{}{{"id": "100-0", "guest": 100, "target": "node-b", "fail_count": 0}})
			})
		}
	})

	cases := []struct {
		name                         string
		extra                        map[string]interface{}
		wantHACalls, wantReplCalls   int32
		wantQuorum, wantHA, wantRepl bool
	}{
		{"default", nil, 0, 0, false, false, false},
		{"cluster", map[string]interface{}{"enable_cluster": true}, 1, 0, true, false, false},
		{"ha", map[string]interface{}{"enable_ha": true}, 1, 0, false, true, false},
		{"replication", map[string]interface{}{"enable_replication": true}, 0, 2, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			haCalls.Store(0)
			replCalls.Store(0)
			p := newGuestProbeWith(t, srv.URL, "", tc.extra)
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			p.moduleLogger = &logger.ModuleLogger{Logger: &zl}

			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if haCalls.Load() != tc.wantHACalls || replCalls.Load() != tc.wantReplCalls {
				t.Errorf("calls ha=%d replication=%d, want %d and %d", haCalls.Load(), replCalls.Load(), tc.wantHACalls, tc.wantReplCalls)
			}
			var quorum, ha, repl bool
			for _, dp := range points {
				switch dp.Name {
				case "proxmox.ha.quorum.status":
					quorum = true
				case "proxmox.ha.resource.status":
					ha = true
				case "proxmox.replication.job.status", "proxmox.replication.job.age":
					repl = true
				}
			}
			if quorum != tc.wantQuorum || ha != tc.wantHA || repl != tc.wantRepl {
				t.Errorf("series quorum=%v ha=%v replication=%v, want %v %v %v", quorum, ha, repl, tc.wantQuorum, tc.wantHA, tc.wantRepl)
			}
			if tc.name == "default" {
				for _, line := range strings.Split(buf.String(), "\n") {
					if strings.Contains(line, "ha/status") || strings.Contains(line, "replication") || strings.Contains(line, "HA") || strings.Contains(line, "quorum") {
						t.Errorf("disabled preview parts must log nothing, got %s", line)
					}
				}
			}
		})
	}
}
