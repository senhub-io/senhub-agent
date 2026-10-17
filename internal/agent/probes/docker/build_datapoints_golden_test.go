package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

const buildDatapointsGolden = "testdata/build_datapoints.golden"

func renderDockerDatapoints(pts []data_store.DataPoint) string {
	var sb strings.Builder
	for _, dp := range pts {
		tg := make([]string, 0, len(dp.Tags))
		for _, tag := range dp.Tags {
			tg = append(tg, tag.Key+"="+tag.Value)
		}
		fmt.Fprintf(&sb, "%s | %g | %s | %s\n", dp.Name, dp.Value, dp.Timestamp.UTC().Format(time.RFC3339), strings.Join(tg, ","))
	}
	return sb.String()
}

const (
	statsCgroupV2 = `{
 "cpu_stats": {"cpu_usage": {"total_usage": 3000, "usage_in_kernelmode": 1000, "usage_in_usermode": 2000, "percpu_usage": [1500, 1500]},
   "system_cpu_usage": 100000, "online_cpus": 2, "throttling_data": {"throttled_periods": 1, "throttling_periods": 5, "throttled_time": 70}},
 "precpu_stats": {"cpu_usage": {"total_usage": 1000}, "system_cpu_usage": 50000},
 "memory_stats": {"usage": 1000000, "limit": 4000000, "stats": {"anon": 300000, "file": 200000, "inactive_file": 100000, "swap": 5, "pgfault": 11, "active_file": 7}},
 "networks": {"eth0": {"tx_bytes": 10, "rx_bytes": 20, "tx_packets": 1, "rx_packets": 2, "tx_errors": 3, "rx_errors": 4, "tx_dropped": 5, "rx_dropped": 6},
              "eth1": {"tx_bytes": 1, "rx_bytes": 2, "tx_packets": 1, "rx_packets": 1, "tx_errors": 0, "rx_errors": 0, "tx_dropped": 0, "rx_dropped": 0}},
 "blkio_stats": {"io_service_bytes_recursive": [{"op": "read", "value": 400}, {"op": "write", "value": 600}]},
 "pids_stats": {"current": 12, "limit": 100}
}`
	statsCgroupV1 = `{
 "cpu_stats": {"cpu_usage": {"total_usage": 500, "percpu_usage": [500]}, "system_cpu_usage": 1000},
 "precpu_stats": {"cpu_usage": {"total_usage": 100}, "system_cpu_usage": 1000},
 "memory_stats": {"usage": 900, "limit": 2000, "stats": {"rss": 300, "cache": 200, "swap": 1, "mapped_file": 9}},
 "blkio_stats": {"io_service_bytes_recursive": [{"op": "Total", "value": 77}, {"op": "Read", "value": 50}, {"op": "Write", "value": 27}],
   "io_service_time_recursive": [{"op": "Total", "value": 33}], "io_sectors_recursive": [{"op": "Total", "value": 44}]},
 "pids_stats": {"current": 3, "limit": 0}
}`
	statsWindows = `{
 "cpu_stats": {"cpu_usage": {"total_usage": 20000000}},
 "precpu_stats": {"cpu_usage": {"total_usage": 10000000}},
 "num_procs": 4,
 "read": "2026-01-01T00:00:10Z", "preread": "2026-01-01T00:00:09Z",
 "memory_stats": {"privateworkingset": 5000, "commitbytes": 7000, "commitpeakbytes": 8000},
 "storage_stats": {"read_size_bytes": 10, "write_size_bytes": 20},
 "networks": {"nat": {"tx_bytes": 5, "rx_bytes": 6}}
}`
)

func TestBuildDatapoints_GoldenOutput(t *testing.T) {
	p := &dockerProbe{}
	ts := time.Unix(1700003600, 0)
	const id = "9f2a1c4e7b3d5a6f8e0c2b4d6a8f1e3c5b7d9f0a2c4e6b8d0f2a4c6e8b0d2f4a"

	scenarios := []struct {
		name  string
		state string
		stats string
	}{
		{"cgroup-v2", "running", statsCgroupV2},
		{"cgroup-v1", "running", statsCgroupV1},
		{"windows", "running", statsWindows},
		{"stopped", "exited", ""},
	}
	var sb strings.Builder
	for _, s := range scenarios {
		res := statsResult{container: containerListItem{ID: id, Names: []string{"/api"}, Image: "nginx:1.27", State: s.state, RestartCount: 2}}
		if s.stats != "" {
			var cs containerStats
			if err := json.Unmarshal([]byte(s.stats), &cs); err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
			res.stats = &cs
		}
		fmt.Fprintf(&sb, "===== %s =====\n%s", s.name, renderDockerDatapoints(p.buildDatapoints(res, ts)))
	}
	got := sb.String()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(buildDatapointsGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(buildDatapointsGolden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(buildDatapointsGolden)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("buildDatapoints output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
