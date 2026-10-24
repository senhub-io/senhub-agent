package proxmox

import (
	"math"
	"net/http"
	"strings"
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

	// series: name|hw.id|state attribute -> value
	got := map[string]float64{}
	attrs := map[string]map[string]string{}
	for _, dp := range points {
		tm := tagsMap(dp)
		if tm["hw.id"] == "" {
			continue
		}
		if tm["proxmox.node"] != "node-a" {
			t.Errorf("%s from %q: node-b answers 403, only node-a may contribute", dp.Name, tm["proxmox.node"])
		}
		state := tm["hw.state"] + tm["hw.physical_disk.state"] + tm["hw.logical_disk.state"]
		k := dp.Name + "|" + tm["hw.id"] + "|" + state
		if _, dup := got[k]; dup {
			t.Errorf("series %s emitted twice", k)
		}
		got[k] = dp.Value
		attrs[k] = tm
	}

	want := map[string]float64{
		"hw.physical_disk.status|SN0001|ok":                       1,
		"hw.physical_disk.status|SN0001|degraded":                 0,
		"hw.physical_disk.status|SN0001|failed":                   0,
		"hw.physical_disk.status|SN0001|predicted_failure":        0,
		"hw.physical_disk.status|SN0002|ok":                       0,
		"hw.physical_disk.status|SN0002|failed":                   1,
		"hw.physical_disk.endurance_utilization|SN0001|remaining": 0.97,
		"hw.logical_disk.status|rpool|ok":                         1,
		"hw.logical_disk.status|rpool|degraded":                   0,
		"hw.logical_disk.status|rpool|failed":                     0,
		"hw.logical_disk.status|tank|ok":                          0,
		"hw.logical_disk.status|tank|degraded":                    1,
		"hw.logical_disk.status|tank|failed":                      0,
		"hw.logical_disk.limit|rpool|":                            999650344960,
		"hw.logical_disk.usage|rpool|used":                        412316860416,
		"hw.logical_disk.usage|rpool|free":                        587333484544,
		"hw.logical_disk.utilization|rpool|":                      412316860416.0 / 999650344960.0,
		"proxmox.zfs.pool.fragmentation|rpool|":                   0.12,
		"proxmox.zfs.pool.fragmentation|tank|":                    0.03,
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing series %s", k)
			continue
		}
		if math.Abs(g-v) > 1e-9 {
			t.Errorf("%s = %v, want %v", k, g, v)
		}
	}

	for k := range got {
		if strings.HasPrefix(k, "hw.physical_disk.endurance_utilization|SN0002") {
			t.Error("wearout N/A must produce no point")
		}
		if strings.Contains(k, "|SN0003|") {
			t.Errorf("%s: a disk without SMART verdict must produce no point", k)
		}
		if strings.HasPrefix(k, "proxmox.disk.") || strings.HasPrefix(k, "proxmox.zfs.pool.health") {
			t.Errorf("legacy series %s must be gone", k)
		}
	}

	a := attrs["hw.physical_disk.status|SN0002|failed"]
	if a["hw.name"] != "/dev/sda" || a["hw.model"] != "Vendor SSD" || a["hw.serial_number"] != "SN0002" || a["hw.physical_disk.type"] != "ssd" {
		t.Errorf("disk attributes = %v", a)
	}
	if a := attrs["hw.physical_disk.endurance_utilization|SN0001|remaining"]; a["hw.name"] != "/dev/nvme0n1" {
		t.Errorf("wearout attributes = %v", a)
	}
}

func TestDiskAndPoolStateMapping(t *testing.T) {
	for in, want := range map[string]string{"PASSED": "ok", "OK": "ok", " passed ": "ok", "FAILED": "failed", "weird": "failed"} {
		if got, ok := diskState(in); !ok || got != want {
			t.Errorf("diskState(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "UNKNOWN"} {
		if _, ok := diskState(in); ok {
			t.Errorf("diskState(%q) must be unjudged", in)
		}
	}
	for in, want := range map[string]string{"ONLINE": "ok", "DEGRADED": "degraded", "FAULTED": "failed", "UNAVAIL": "failed", "REMOVED": "failed", "SUSPENDED": "failed", "OFFLINE": "failed"} {
		if got, ok := poolState(in); !ok || got != want {
			t.Errorf("poolState(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := poolState(""); ok {
		t.Error("an empty pool state must be unjudged")
	}
}

func TestCollect_DiskWithoutSerialFallsBackToDevicePath(t *testing.T) {
	srv := newPVE9Server(t, resourcesReply(t), func(mux *http.ServeMux) {
		mux.HandleFunc("/api2/json/nodes/node-a/disks/list", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, []map[string]interface{}{{"devpath": "/dev/sdc", "type": "hdd", "serial": "", "health": "PASSED", "wearout": "N/A"}})
		})
	})
	p := newGuestProbe(t, srv.URL, "node-a")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	n := 0
	for _, dp := range points {
		if dp.Name == "hw.physical_disk.status" {
			n++
			if id := tagsMap(dp)["hw.id"]; id != "/dev/sdc" {
				t.Errorf("hw.id = %q, want the device path", id)
			}
		}
	}
	if n != 4 {
		t.Errorf("hw.physical_disk.status series = %d, want 4", n)
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
			if tagsMap(dp)["hw.id"] != "" {
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
