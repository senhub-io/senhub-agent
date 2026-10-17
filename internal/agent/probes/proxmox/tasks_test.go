package proxmox

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const (
	upidBackup = "UPID:node-a:0001:0002:6700:vzdump:101:backup@pbs:"
	upidStart  = "UPID:node-a:0001:0003:6701:qmstart:102:monitor@pve:"
	upidOld    = "UPID:node-a:0001:0004:6702:vzdump:103:backup@pbs:"
)

func tasksHandlers(t *testing.T, now time.Time, failedQueries *[]string) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/tasks", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("errors") != "1" {
				writeJSON(t, w, []interface{}{})
				return
			}
			*failedQueries = append(*failedQueries, r.URL.RawQuery)
			writeJSON(t, w, []map[string]interface{}{
				{"upid": upidBackup, "node": "node-a", "type": "vzdump", "id": "101", "starttime": now.Add(-90 * time.Second).Unix(), "endtime": now.Add(-30 * time.Second).Unix(), "status": "ERROR: job failed"},
				{"upid": upidStart, "node": "node-a", "type": "qmstart", "id": "102", "starttime": now.Add(-20 * time.Second).Unix(), "endtime": now.Add(-19 * time.Second).Unix(), "status": "WARNINGS: 1"},
				{"upid": upidOld, "node": "node-a", "type": "vzdump", "id": "103", "starttime": now.Add(-5 * time.Hour).Unix(), "endtime": now.Add(-4 * time.Hour).Unix(), "status": "ERROR: old"},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-a/tasks/"+upidBackup+"/log", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			if q.Get("limit") == "1" {
				_, _ = w.Write([]byte(`{"data":[{"n":1,"t":"INFO: starting new backup job"}],"total":30}`))
				return
			}
			if q.Get("start") != "10" || q.Get("limit") != "20" {
				t.Errorf("tail must be start=total-20 limit=20, got %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"n":11,"t":"INFO: transferred 4.2 GiB"},{"n":12,"t":"ERROR: backup write data failed: timeout"}],"total":30}`))
		})
		mux.HandleFunc("/api2/json/nodes/node-a/tasks/"+upidStart+"/log", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
	}
}

func TestCollect_FailedTasksCountAndLogs(t *testing.T) {
	now := time.Now()
	var queries []string
	srv := newPVE9Server(t, resourcesReply(t), tasksHandlers(t, now, &queries))
	p := newGuestProbe(t, srv.URL, "node-a")
	p.SetName("pve-test")
	p.SetLogTargets([]string{"otlp"})

	logs := agentstate.SubscribeLogs(64)
	defer agentstate.UnsubscribeLogs(logs)

	count := func() map[string]float64 {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		out := map[string]float64{}
		for _, dp := range points {
			if dp.Name == "proxmox.tasks.failed" {
				out[tagsMap(dp)["proxmox.task.type"]] = dp.Value
			}
		}
		return out
	}

	first := count()
	if len(first) != 2 || first["vzdump"] != 1 || first["qmstart"] != 1 {
		t.Errorf("first cycle counts = %v, want vzdump=1 qmstart=1 (the task ended before the probe started is ignored)", first)
	}
	second := count()
	if len(second) != 2 || second["vzdump"] != 0 || second["qmstart"] != 0 {
		t.Errorf("second cycle counts = %v, want zeros for the types already seen", second)
	}

	if len(queries) == 0 || !strings.Contains(queries[0], "errors=1") || !strings.Contains(queries[0], "since=") || !strings.Contains(queries[0], "limit=50") {
		t.Errorf("failed task query = %v", queries)
	}

	got := map[string]agentstate.LogRecord{}
	for len(logs) > 0 {
		r := <-logs
		if _, dup := got[r.Attributes["proxmox.task.upid"]]; dup {
			t.Errorf("task %s published twice", r.Attributes["proxmox.task.upid"])
		}
		got[r.Attributes["proxmox.task.upid"]] = r
	}
	if len(got) != 2 {
		t.Fatalf("published %d records, want 2: %v", len(got), got)
	}

	b := got[upidBackup]
	if b.Severity != agentstate.LogSeverityError {
		t.Errorf("severity = %v, want ERROR", b.Severity)
	}
	if b.Body != "INFO: transferred 4.2 GiB\nERROR: backup write data failed: timeout" {
		t.Errorf("body = %q", b.Body)
	}
	if b.Attributes["proxmox.node"] != "node-a" || b.Attributes["proxmox.task.type"] != "vzdump" || b.Attributes["proxmox.vmid"] != "101" {
		t.Errorf("attributes = %v", b.Attributes)
	}
	if b.ProducerProbeName != "pve-test" || len(b.TargetStrategies) != 1 || b.TargetStrategies[0] != "otlp" {
		t.Errorf("producer/targets = %q %v", b.ProducerProbeName, b.TargetStrategies)
	}

	s := got[upidStart]
	if s.Severity != agentstate.LogSeverityWarn || s.Body != "WARNINGS: 1" {
		t.Errorf("unreadable log must fall back to the status text, got severity %v body %q", s.Severity, s.Body)
	}
}
