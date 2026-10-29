package proxmox

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestCollect_ReplicationJobs(t *testing.T) {
	now := time.Now()
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/replication", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{
					"id":         "100-0",
					"guest":      100,
					"target":     "node-b",
					"vmtype":     "qemu",
					"schedule":   "*/15",
					"last_sync":  now.Add(-600 * time.Second).Unix(),
					"fail_count": 0,
				},
				{
					"id":         "101-0",
					"guest":      101,
					"target":     "node-b",
					"vmtype":     "qemu",
					"schedule":   "*/15",
					"last_sync":  now.Add(-7200 * time.Second).Unix(),
					"fail_count": 3,
					"error":      "no route",
				},
				{
					"id":         "102-0",
					"guest":      102,
					"target":     "",
					"vmtype":     "qemu",
					"schedule":   "*/15",
					"last_sync":  0,
					"fail_count": 0,
				},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-b/replication", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []interface{}{})
		})
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	statuses := map[string]map[string]float64{}
	ages := map[string]float64{}
	attrs := map[string]map[string]string{}
	for _, dp := range points {
		if !strings.HasPrefix(dp.Name, "proxmox.replication.") {
			continue
		}
		tm := tagsMap(dp)
		job := tm["proxmox.replication.job"]
		if attrs[job] == nil {
			attrs[job] = tm
		}
		if dp.Name == "proxmox.replication.job.status" {
			if statuses[job] == nil {
				statuses[job] = map[string]float64{}
			}
			statuses[job][tm["proxmox.replication.job.state"]] = dp.Value
		}
		if dp.Name == "proxmox.replication.job.age" {
			if _, dup := ages[job]; dup {
				t.Errorf("duplicate proxmox.replication.job.age for job %s", job)
			}
			ages[job] = dp.Value
		}
	}

	if got := statuses["100-0"]; len(got) != 2 || got["ok"] != 1 || got["failed"] != 0 {
		t.Errorf("job 100-0 status series = %v, want ok=1 failed=0", got)
	}
	if got := statuses["101-0"]; len(got) != 2 || got["ok"] != 0 || got["failed"] != 1 {
		t.Errorf("job 101-0 status series = %v, want ok=0 failed=1", got)
	}
	if got := statuses["102-0"]; len(got) != 2 || got["ok"] != 1 || got["failed"] != 0 {
		t.Errorf("job 102-0 status series = %v, want ok=1 failed=0", got)
	}
	if age := ages["100-0"]; age < 600-60 || age > 600+60 {
		t.Errorf("job 100-0 age = %v s, want about 600", age)
	}
	if age := ages["101-0"]; age < 7200-60 || age > 7200+60 {
		t.Errorf("job 101-0 age = %v s, want about 7200", age)
	}
	if _, ok := ages["102-0"]; ok {
		t.Errorf("job 102-0 never synced must have no age, got %v", ages["102-0"])
	}

	for _, job := range []string{"100-0", "101-0", "102-0"} {
		tm := attrs[job]
		if tm["proxmox.node"] != "node-a" {
			t.Errorf("job %s node = %q, want node-a", job, tm["proxmox.node"])
		}
		if tm["metric_type"] != "replication" {
			t.Errorf("job %s metric_type = %q, want replication", job, tm["metric_type"])
		}
		if tm["proxmox.vmid"] == "" {
			t.Errorf("job %s missing proxmox.vmid tag", job)
		}
	}
	if tm := attrs["100-0"]; tm["proxmox.vmid"] != "100" {
		t.Errorf("job 100-0 vmid = %q, want 100", tm["proxmox.vmid"])
	}
	if tm := attrs["101-0"]; tm["proxmox.vmid"] != "101" {
		t.Errorf("job 101-0 vmid = %q, want 101", tm["proxmox.vmid"])
	}
	if tm := attrs["102-0"]; tm["proxmox.vmid"] != "102" {
		t.Errorf("job 102-0 vmid = %q, want 102", tm["proxmox.vmid"])
	}
	if tm := attrs["100-0"]; tm["proxmox.replication.target"] != "node-b" {
		t.Errorf("job 100-0 target = %q, want node-b", tm["proxmox.replication.target"])
	}
	if tm := attrs["101-0"]; tm["proxmox.replication.target"] != "node-b" {
		t.Errorf("job 101-0 target = %q, want node-b", tm["proxmox.replication.target"])
	}
	if v, ok := attrs["102-0"]["proxmox.replication.target"]; ok {
		t.Errorf("job 102-0 has target tag %q, want omitted", v)
	}
}

func TestCollect_ReplicationEmptyDoesNothing(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		for _, n := range []string{"node-a", "node-b"} {
			n := n
			mux.HandleFunc("/api2/json/nodes/"+n+"/replication", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, []interface{}{})
			})
		}
	})
	p := newGuestProbeWith(t, srv.URL, "", previewAll)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	p.moduleLogger = &logger.ModuleLogger{Logger: &zl}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if strings.HasPrefix(dp.Name, "proxmox.replication.") {
			t.Errorf("replication point emitted for empty replication list: %s", dp.Name)
		}
	}
	if strings.Contains(buf.String(), "failed to read replication status") {
		t.Errorf("unexpected replication warning: %s", buf.String())
	}
}

func TestCollect_ReplicationErrorWarnsAndKeepsUp(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/replication", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		mux.HandleFunc("/api2/json/nodes/node-b/replication", func(w http.ResponseWriter, r *http.Request) {
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

	up := false
	for _, dp := range points {
		if strings.HasPrefix(dp.Name, "proxmox.replication.") {
			t.Errorf("replication point emitted after 500: %s", dp.Name)
		}
		if dp.Name == "senhub.proxmox.up" {
			up = true
			if dp.Value != 1 {
				t.Errorf("senhub.proxmox.up = %v, want 1", dp.Value)
			}
		}
	}
	if !up {
		t.Error("senhub.proxmox.up point missing")
	}
	if got := strings.Count(buf.String(), "failed to read replication status"); got != 1 {
		t.Errorf("replication warnings = %d, want 1; log:\n%s", got, buf.String())
	}
}

func TestCollect_ReplicationDeniedSilently(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/replication", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		})
		mux.HandleFunc("/api2/json/nodes/node-b/replication", func(w http.ResponseWriter, r *http.Request) {
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
	for _, dp := range points {
		if strings.HasPrefix(dp.Name, "proxmox.replication.") {
			t.Errorf("replication point emitted after 403: %s", dp.Name)
		}
	}
	if strings.Contains(buf.String(), "failed to read replication status") {
		t.Errorf("unexpected replication warning: %s", buf.String())
	}
}
