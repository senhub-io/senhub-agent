package proxmox

import (
	"net/http"
	"sync/atomic"
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
			if r.URL.Query().Get("typefilter") != "vzdump" {
				writeJSON(t, w, []interface{}{})
				return
			}
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
	p := newGuestProbeWith(t, srv.URL, "node-a", map[string]interface{}{"enable_pbs": true})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	web := vmPoints(points, "101")
	// Newest successful backup is the PBS archive of 26 h (the task of 29 h is older).
	if age := web["proxmox.vm.backup.age"]; age < 26*3600-5 || age > 26*3600+60 {
		t.Errorf("backup age = %v s, want about %v", age, 26*3600)
	}
	states := map[string]float64{}
	for _, dp := range points {
		if tm := tagsMap(dp); dp.Name == "proxmox.vm.backup.status" && tm["proxmox.vmid"] == "101" {
			states[tm["proxmox.vm.backup.state"]] = dp.Value
		}
	}
	if len(states) != 2 || states["ok"] != 0 || states["failed"] != 1 {
		t.Errorf("last task failed: backup status series = %v, want ok=0 failed=1", states)
	}
	if len(vmPoints(points, "999")) != 0 {
		t.Error("an archive of a guest unknown to the node must produce nothing")
	}

	if want := "limit=200&typefilter=vzdump"; tasksQuery != want {
		t.Errorf("task query = %q, want %q", tasksQuery, want)
	}

	status := map[string]float64{}
	for _, dp := range points {
		if dp.Name == "proxmox.storage.status" {
			tm := tagsMap(dp)
			status[tm["proxmox.storage"]+"|"+tm["proxmox.storage.state"]] = dp.Value
		}
	}
	wantStatus := map[string]float64{
		"pbs-main|active": 1, "pbs-main|inactive": 0,
		"pbs-offline|active": 0, "pbs-offline|inactive": 1,
		"local-zfs|active": 1, "local-zfs|inactive": 0,
	}
	if len(status) != len(wantStatus) {
		t.Errorf("storage.status = %v", status)
	}
	for k, v := range wantStatus {
		if g, ok := status[k]; !ok || g != v {
			t.Errorf("storage.status %s = %v (present=%v), want %v", k, g, ok, v)
		}
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
		if dp.Name == "proxmox.vm.backup.age" || dp.Name == "proxmox.vm.backup.status" || dp.Name == "proxmox.storage.status" {
			t.Errorf("%s emitted without any backup data", dp.Name)
		}
	}
}

func TestCollect_BackupCoverage(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/cluster/backup", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"id": "backup-all", "enabled": 1, "all": 1, "exclude": "201,102"},
				{"id": "backup-off", "enabled": 0, "vmid": "201"},
			})
		})
	})
	p := newGuestProbe(t, srv.URL, "")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float64{}
	for _, dp := range points {
		if dp.Name == "proxmox.backup.job.count" || dp.Name == "proxmox.vm.backup.unprotected.count" {
			got[dp.Name] = dp.Value
		}
	}
	if got["proxmox.backup.job.count"] != 1 || got["proxmox.vm.backup.unprotected.count"] != 2 {
		t.Errorf("coverage = %v, want 1 enabled job and 2 unprotected guests (201 and 102)", got)
	}
}

func TestCollect_BackupCoverageUnreadableEmitsNothing(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), nil)
	p := newGuestProbe(t, srv.URL, "")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if dp.Name == "proxmox.backup.job.count" || dp.Name == "proxmox.vm.backup.unprotected.count" {
			t.Errorf("%s emitted although /cluster/backup is unreadable", dp.Name)
		}
	}
}

func TestCollect_PreviewStoragesOffByDefaultCallNothing(t *testing.T) {
	var contentCalls atomic.Int32
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/storage", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"storage": "local-zfs", "type": "zfspool", "content": "images,rootdir", "active": 1, "used": 10, "total": 100},
				{"storage": "pbs-main", "type": "pbs", "content": "backup", "active": 1, "used": 40, "total": 400},
				{"storage": "ceph-pool", "type": "rbd", "content": "images,backup", "active": 1, "used": 5, "total": 50},
				{"storage": "ceph-fs", "type": "cephfs", "content": "backup", "active": 1, "used": 5, "total": 50},
			})
		})
		for _, name := range []string{"pbs-main", "ceph-pool", "ceph-fs"} {
			mux.HandleFunc("/api2/json/nodes/node-a/storage/"+name+"/content", func(w http.ResponseWriter, r *http.Request) {
				contentCalls.Add(1)
				writeJSON(t, w, []interface{}{})
			})
		}
	})

	for _, tc := range []struct {
		name  string
		extra map[string]interface{}
		want  map[string]bool
	}{
		{"default", nil, map[string]bool{"local-zfs": true}},
		{"pbs", map[string]interface{}{"enable_pbs": true}, map[string]bool{"local-zfs": true, "pbs-main": true}},
		{"ceph", map[string]interface{}{"enable_ceph": true}, map[string]bool{"local-zfs": true, "ceph-pool": true, "ceph-fs": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentCalls.Store(0)
			p := newGuestProbeWith(t, srv.URL, "node-a", tc.extra)
			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			got := map[string]bool{}
			for _, dp := range points {
				if st := tagsMap(dp)["proxmox.storage"]; st != "" {
					got[st] = true
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("storages with series = %v, want %v", got, tc.want)
			}
			for st := range tc.want {
				if !got[st] {
					t.Errorf("storage %s has no series", st)
				}
			}
			wantCalls := int32(0)
			for st := range tc.want {
				if st != "local-zfs" {
					wantCalls++
				}
			}
			if contentCalls.Load() != wantCalls {
				t.Errorf("backup archive listings = %d, want %d", contentCalls.Load(), wantCalls)
			}
		})
	}
}
