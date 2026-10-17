package proxmox

import (
	"math"
	"net/http"
	"testing"
)

func disksHandlers(t *testing.T) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/disks/list", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"devpath": "/dev/nvme0n1", "type": "nvme", "model": "Vendor NVMe 1TB", "serial": "SN0001", "size": 1000204886016, "health": "PASSED", "wearout": 97},
				{"devpath": "/dev/sda", "type": "ssd", "model": "Vendor SSD", "serial": "SN0002", "size": 480103981056, "health": "FAILED", "wearout": "N/A"},
				{"devpath": "/dev/sdb", "type": "hdd", "model": "Vendor HDD", "serial": "SN0003", "size": 4000787030016, "health": "UNKNOWN", "wearout": "N/A"},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-a/disks/zfs", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{
				{"name": "rpool", "health": "ONLINE", "size": 999650344960, "alloc": 412316860416, "free": 587333484544, "frag": 12, "dedup": 1.0},
				{"name": "tank", "health": "DEGRADED", "size": 3985729650688, "alloc": 1099511627776, "free": 2886218022912, "frag": 3, "dedup": 1.0},
			})
		})
		mux.HandleFunc("/api2/json/nodes/node-b/disks/list", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Permission check failed (/nodes/node-b, Sys.Audit)", http.StatusForbidden)
		})
		mux.HandleFunc("/api2/json/nodes/node-b/disks/zfs", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Permission check failed (/nodes/node-b, Sys.Audit)", http.StatusForbidden)
		})
	}
}

func TestCollect_DisksAndZFS(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), disksHandlers(t))
	p := newGuestProbe(t, srv.URL, "")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	type key struct{ name, id string }
	got := map[key]float64{}
	attrs := map[key]map[string]string{}
	for _, dp := range points {
		tm := tagsMap(dp)
		id := tm["proxmox.disk"] + tm["proxmox.zfs.pool"]
		if id == "" {
			continue
		}
		if tm["proxmox.node"] != "node-a" {
			t.Errorf("%s from %q: node-b answers 403, only node-a may contribute", dp.Name, tm["proxmox.node"])
		}
		got[key{dp.Name, id}] = dp.Value
		attrs[key{dp.Name, id}] = tm
	}

	want := map[key]float64{
		{"proxmox.disk.health", "/dev/nvme0n1"}:     1,
		{"proxmox.disk.wearout", "/dev/nvme0n1"}:    0.03,
		{"proxmox.disk.health", "/dev/sda"}:         0,
		{"proxmox.zfs.pool.health", "rpool"}:        1,
		{"proxmox.zfs.pool.health", "tank"}:         0,
		{"proxmox.zfs.pool.size", "rpool"}:          999650344960,
		{"proxmox.zfs.pool.used", "rpool"}:          412316860416,
		{"proxmox.zfs.pool.free", "rpool"}:          587333484544,
		{"proxmox.zfs.pool.fragmentation", "rpool"}: 0.12,
		{"proxmox.zfs.pool.fragmentation", "tank"}:  0.03,
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing %s for %s", k.name, k.id)
			continue
		}
		if math.Abs(g-v) > 1e-9 {
			t.Errorf("%s %s = %v, want %v", k.name, k.id, g, v)
		}
	}

	if _, ok := got[key{"proxmox.disk.wearout", "/dev/sda"}]; ok {
		t.Error("wearout N/A must produce no point")
	}
	for _, n := range []string{"proxmox.disk.health", "proxmox.disk.wearout"} {
		if _, ok := got[key{n, "/dev/sdb"}]; ok {
			t.Errorf("%s: a disk without SMART verdict must produce no point", n)
		}
	}
	if a := attrs[key{"proxmox.disk.health", "/dev/sda"}]; a["proxmox.health"] != "FAILED" || a["proxmox.disk.serial"] != "SN0002" || a["proxmox.disk.type"] != "ssd" {
		t.Errorf("disk attributes = %v", a)
	}
	if a := attrs[key{"proxmox.zfs.pool.health", "tank"}]; a["proxmox.health"] != "DEGRADED" {
		t.Errorf("pool attributes = %v", a)
	}
}

func TestCollect_DisksDeniedIsSilentAndNotAnError(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), disksHandlers(t))
	p := newGuestProbe(t, srv.URL, "node-b")

	for i := 0; i < 2; i++ {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		for _, dp := range points {
			if tagsMap(dp)["proxmox.disk"] != "" || tagsMap(dp)["proxmox.zfs.pool"] != "" {
				t.Errorf("%s emitted although the token cannot read the endpoint", dp.Name)
			}
			if dp.Name == "senhub.probe.check.result" && dp.Value != 0 {
				t.Errorf("a 403 on disks must not degrade the probe check result, got %v", dp.Value)
			}
		}
	}
	if len(p.denied.seen) != 2 {
		t.Errorf("each denied endpoint is remembered once, got %v", p.denied.seen)
	}
}
