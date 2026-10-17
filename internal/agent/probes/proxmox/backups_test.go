package proxmox

import (
	"net/http"
	"testing"
	"time"
)

func backupHandlers(t *testing.T, now time.Time, tasksQuery *string) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/storage", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"storage": "local-zfs", "type": "zfspool", "content": "images,rootdir", "active": 1, "used": 10, "total": 100},
				{"storage": "pbs-main", "type": "pbs", "content": "backup", "active": 1, "used": 40, "total": 400},
				{"storage": "pbs-offline", "type": "pbs", "content": "backup", "active": 0},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-a/storage/pbs-main/content", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("content") != "backup" {
				t.Errorf("archives must be asked with content=backup, got %q", r.URL.RawQuery)
			}
			writeJSON(t, w, []map[string]interface{}{
				{"volid": "pbs-main:backup/vm/101/2026-10-17T01:00:00Z", "vmid": 101, "ctime": now.Add(-26 * time.Hour).Unix(), "format": "pbs-vm"},
				{"volid": "pbs-main:backup/vm/101/2026-10-16T01:00:00Z", "vmid": 101, "ctime": now.Add(-50 * time.Hour).Unix(), "format": "pbs-vm"},
				{"volid": "pbs-main:backup/vm/999/2026-10-16T01:00:00Z", "vmid": 999, "ctime": now.Add(-1 * time.Hour).Unix(), "format": "pbs-vm"},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-a/storage/pbs-offline/content", func(w http.ResponseWriter, r *http.Request) {
			t.Error("an inactive storage must not be listed")
			http.Error(w, "unreachable", http.StatusInternalServerError)
		})
		mux.HandleFunc("/api2/json/nodes/node-a/tasks", func(w http.ResponseWriter, r *http.Request) {
			*tasksQuery = r.URL.RawQuery
			writeJSON(t, w, []map[string]interface{}{
				{"upid": "UPID:node-a:0001:0002:6700:vzdump:101:backup@pbs:", "node": "node-a", "type": "vzdump", "id": "101", "starttime": now.Add(-3 * time.Hour).Unix(), "endtime": now.Add(-2 * time.Hour).Unix(), "status": "ERROR: backup failed: timeout"},
				{"upid": "UPID:node-a:0001:0003:6600:vzdump:101:backup@pbs:", "node": "node-a", "type": "vzdump", "id": "101", "starttime": now.Add(-30 * time.Hour).Unix(), "endtime": now.Add(-29 * time.Hour).Unix(), "status": "OK"},
				{"upid": "UPID:node-a:0001:0004:6500:vzdump::backup@pbs:", "node": "node-a", "type": "vzdump", "id": "", "starttime": now.Add(-48 * time.Hour).Unix(), "endtime": now.Add(-47 * time.Hour).Unix(), "status": "OK"},
			})
		})
	}
}

func TestCollect_BackupsAndStorageActive(t *testing.T) {
	now := time.Now()
	var tasksQuery string
	srv := newPVE9Server(t, resourcesReply(t), backupHandlers(t, now, &tasksQuery))
	p := newGuestProbe(t, srv.URL, "node-a")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	web := vmPoints(points, "101")
	// Newest successful backup is the PBS archive of 26 h (the task of 29 h is older).
	if age := web["proxmox.vm.backup.age"]; age < 26*3600-5 || age > 26*3600+60 {
		t.Errorf("backup age = %v s, want about %v", age, 26*3600)
	}
	if st, ok := web["proxmox.vm.backup.last_status"]; !ok || st != 0 {
		t.Errorf("last task failed: last_status = %v (present=%v), want 0", st, ok)
	}
	if len(vmPoints(points, "999")) != 0 {
		t.Error("an archive of a guest unknown to the node must produce nothing")
	}

	if want := "limit=200&typefilter=vzdump"; tasksQuery != want {
		t.Errorf("task query = %q, want %q", tasksQuery, want)
	}

	active := map[string]float64{}
	for _, dp := range points {
		if dp.Name == "proxmox.storage.active" {
			active[tagsMap(dp)["proxmox.storage"]] = dp.Value
		}
	}
	if len(active) != 3 || active["pbs-main"] != 1 || active["pbs-offline"] != 0 || active["local-zfs"] != 1 {
		t.Errorf("storage.active = %v", active)
	}
}

func TestCollect_NoBackupInformationEmitsNothing(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), nil)
	p := newGuestProbe(t, srv.URL, "node-a")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if dp.Name == "proxmox.vm.backup.age" || dp.Name == "proxmox.vm.backup.last_status" || dp.Name == "proxmox.storage.active" {
			t.Errorf("%s emitted without any backup data", dp.Name)
		}
	}
}
