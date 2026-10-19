package proxmox

import (
	"bytes"
	"net/http"
	"strings"
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
	p := newGuestProbe(t, srv.URL, "")

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
	p := newGuestProbe(t, srv.URL, "")

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
	p := newGuestProbe(t, srv.URL, "")

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
	p := newGuestProbe(t, srv.URL, "")

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
	p := newGuestProbe(t, srv.URL, "")

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
