package redis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

const buildDatapointsGolden = "testdata/build_datapoints.golden"

func renderDatapoints(pts []data_store.DataPoint) string {
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

// The scenarios use a single database and a single command so the output
// order, which follows map iteration for those two loops, is fixed.
func TestBuildDatapoints_GoldenOutput(t *testing.T) {
	p := newTestProbe(t)
	p.entityObs = newEntityObserver(p.cfg, func() string { return "h-1" })
	ts := time.Unix(1700003600, 0)

	master := fullInfoMap()
	delete(master, "db1")
	master["redis_mode"] = "standalone"
	master["rdb_last_bgsave_status"] = "ok"
	master["aof_last_bgrewrite_status"] = "err"
	master["aof_last_write_status"] = "ok"
	master["aof_last_rewrite_time_sec"] = "3"
	master["repl_backlog_active"] = "1"
	master["repl_backlog_size"] = "1048576"
	master["repl_backlog_histlen"] = "512"
	master["pubsub_channels"] = "4"
	master["pubsub_patterns"] = "1"
	master["tracking_clients"] = "2"
	master["tracking_table_used_keys"] = "9"
	cluster := map[string]string{
		"cluster_state": "ok", "cluster_slots_assigned": "16384", "cluster_slots_ok": "16384",
		"cluster_slots_pfail": "0", "cluster_slots_fail": "0",
		"cluster_stats_messages_sent": "10", "cluster_stats_messages_received": "11",
	}

	replica := map[string]string{
		"role": "slave", "slave_repl_offset": "100", "replica_repl_offset": "200",
		"master_last_io_seconds_ago": "3", "keyspace_hits": "5", "rdb_last_save_time": "1700003000",
		"redis_version": "6.2.0",
	}

	sentinel := map[string]string{"redis_mode": "sentinel", "redis_version": "7.2.4", "role": "sentinel"}
	sentinelInfo := map[string]string{
		"sentinel_masters": "1", "sentinel_running_scripts": "0",
		"master0": "name=mymaster,status=ok,address=10.0.0.1:6379,slaves=2,sentinels=3",
	}

	scenarios := []struct {
		name string
		pts  []data_store.DataPoint
	}{
		{"master+cluster", p.buildDatapoints(master, map[string]cmdStat{"get": {calls: 10, usec: 50}}, cluster, nil, ts, 1)},
		{"replica", p.buildDatapoints(replica, nil, nil, nil, ts, 1)},
		{"sentinel", p.buildDatapoints(sentinel, nil, nil, sentinelInfo, ts, 1)},
		{"down", p.buildDatapoints(map[string]string{}, nil, nil, nil, ts, 0)},
	}
	var sb strings.Builder
	for _, s := range scenarios {
		fmt.Fprintf(&sb, "===== %s =====\n%s", s.name, renderDatapoints(s.pts))
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
