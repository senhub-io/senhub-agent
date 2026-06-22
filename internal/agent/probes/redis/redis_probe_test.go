package redis

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestEntityObserver_LocalDBRunsOnHost: a loopback-reachable redis is anchored to
// the host with runs_on (enterprise#36); a remote one is not.
func TestEntityObserver_LocalDBRunsOnHost(t *testing.T) {
	runsOn := func(host string) bool {
		o := newEntityObserver(probeConfig{Host: host, Port: 6379}, host+":6379")
		o.hostID = func() string { return "h-1" }
		o.update(probeConfig{Host: host, Port: 6379}, map[string]string{})
		got, _ := o.Observe()
		for _, r := range got.Relations {
			if r.Type == "runs_on" && r.FromType == "db" && r.ToID["host.id"] == "h-1" {
				return true
			}
		}
		return false
	}
	if !runsOn("127.0.0.1") {
		t.Error("loopback db must emit runs_on→host")
	}
	if runsOn("10.0.0.5") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

func newTestProbe(t *testing.T) *redisProbe {
	t.Helper()
	p, err := NewRedisProbe(map[string]interface{}{}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	rp := p.(*redisProbe)
	rp.SetName("redis-test")
	return rp
}

// TestParseConfig_Defaults verifies all defaults are applied when no
// config keys are provided.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != 6379 {
		t.Errorf("Port = %d, want 6379", cfg.Port)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", cfg.Interval)
	}
	if cfg.TLS {
		t.Error("TLS should default to false")
	}
	if cfg.Password != "" {
		t.Error("Password should default to empty")
	}
}

// TestParseConfig_Custom verifies that explicitly supplied values
// override defaults.
func TestParseConfig_Custom(t *testing.T) {
	raw := map[string]interface{}{
		"host":     "10.0.0.1",
		"port":     6380,
		"password": "s3cr3t",
		"tls":      true,
		"timeout":  10,
		"interval": 30,
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "10.0.0.1" {
		t.Errorf("Host = %q, want 10.0.0.1", cfg.Host)
	}
	if cfg.Port != 6380 {
		t.Errorf("Port = %d, want 6380", cfg.Port)
	}
	if cfg.Password != "s3cr3t" {
		t.Errorf("Password = %q, want s3cr3t", cfg.Password)
	}
	if !cfg.TLS {
		t.Error("TLS should be true")
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", cfg.Timeout)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", cfg.Interval)
	}
}

// TestParseConfig_InvalidPort verifies out-of-range port is rejected.
func TestParseConfig_InvalidPort(t *testing.T) {
	cases := []int{0, -1, 65536, 99999}
	for _, p := range cases {
		if _, err := parseConfig(map[string]interface{}{"port": p}); err == nil {
			t.Errorf("port %d: expected error, got nil", p)
		}
	}
}

// TestParseInfoBlob verifies the INFO blob parser produces the correct
// flat key→value map from a multi-section input.
func TestParseInfoBlob(t *testing.T) {
	blob := "# Server\r\nredis_version:7.0.1\r\nuptime_in_seconds:12345\r\n\r\n" +
		"# Clients\r\nconnected_clients:42\r\n\r\n" +
		"# Keyspace\r\ndb0:keys=100,expires=5,avg_ttl=3000\r\n"

	m := parseInfoBlob(blob)

	if m["redis_version"] != "7.0.1" {
		t.Errorf("redis_version = %q, want 7.0.1", m["redis_version"])
	}
	if m["connected_clients"] != "42" {
		t.Errorf("connected_clients = %q, want 42", m["connected_clients"])
	}
	if _, ok := m["db0"]; !ok {
		t.Error("db0 key missing from parsed map")
	}
	if _, ok := m["# Server"]; ok {
		t.Error("section header should not appear in parsed map")
	}
}

// TestParseKeyspaceLine tests the keyspace line parser.
func TestParseKeyspaceLine(t *testing.T) {
	cases := []struct {
		line    string
		db      string
		keys    int64
		expires int64
		avgTTL  int64
		ok      bool
	}{
		{"db0:keys=100,expires=5,avg_ttl=3000", "0", 100, 5, 3000, true},
		{"db1:keys=0,expires=0,avg_ttl=0", "1", 0, 0, 0, true},
		{"db12:keys=999,expires=10,avg_ttl=500", "12", 999, 10, 500, true},
		{"notadb:keys=1,expires=0", "", 0, 0, 0, false},
		{"nocoron", "", 0, 0, 0, false},
	}
	for _, tc := range cases {
		db, keys, expires, avgTTL, ok := parseKeyspaceLine(tc.line)
		if ok != tc.ok {
			t.Errorf("parseKeyspaceLine(%q): ok=%v, want %v", tc.line, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if db != tc.db {
			t.Errorf("parseKeyspaceLine(%q): db=%q, want %q", tc.line, db, tc.db)
		}
		if keys != tc.keys {
			t.Errorf("parseKeyspaceLine(%q): keys=%d, want %d", tc.line, keys, tc.keys)
		}
		if expires != tc.expires {
			t.Errorf("parseKeyspaceLine(%q): expires=%d, want %d", tc.line, expires, tc.expires)
		}
		if avgTTL != tc.avgTTL {
			t.Errorf("parseKeyspaceLine(%q): avgTTL=%d, want %d", tc.line, avgTTL, tc.avgTTL)
		}
	}
}

// fullInfoMap returns a complete synthetic INFO map suitable for
// TestBuildDatapoints_Full.
func fullInfoMap() map[string]string {
	return map[string]string{
		"redis_version":                   "7.0.1",
		"uptime_in_seconds":               "12345",
		"connected_clients":               "10",
		"blocked_clients":                 "2",
		"total_connections_received":      "5000",
		"rejected_connections":            "3",
		"used_memory":                     "1048576",
		"used_memory_rss":                 "2097152",
		"used_memory_peak":                "3145728",
		"mem_fragmentation_ratio":         "1.5",
		"used_memory_lua":                 "37888",
		"total_commands_processed":        "100000",
		"total_net_input_bytes":           "204800",
		"total_net_output_bytes":          "409600",
		"instantaneous_ops_per_sec":       "500",
		"keyspace_hits":                   "900",
		"keyspace_misses":                 "100",
		"evicted_keys":                    "42",
		"expired_keys":                    "17",
		"db0":                             "keys=100,expires=5,avg_ttl=3000",
		"db1":                             "keys=200,expires=10,avg_ttl=1000",
		"role":                            "master",
		"master_repl_offset":              "99999",
		"connected_slaves":                "1",
		"repl_backlog_first_byte_offset":  "99999",
		"rdb_changes_since_last_save":     "7",
		"aof_enabled":                     "0",
		"rdb_last_bgsave_time_sec":        "2",
		"rdb_last_save_time":              "1700000000",
		"used_cpu_sys":                    "1.5",
		"used_cpu_user":                   "0.8",
		"used_cpu_sys_children":           "0.1",
		"used_cpu_user_children":          "0.05",
		"client_recent_max_input_buffer":  "32768",
		"client_recent_max_output_buffer": "65536",
		"latest_fork_usec":                "350",
	}
}

// fullCmdStats returns a synthetic commandstats map.
func fullCmdStats() map[string]cmdStat {
	return map[string]cmdStat{
		"get": {calls: 1000, usec: 5000},
		"set": {calls: 200, usec: 1200},
	}
}

// indexDatapoints builds a name→[]DataPoint map for assertion helpers.
func indexDatapoints(pts []data_store.DataPoint) map[string][]data_store.DataPoint {
	idx := make(map[string][]data_store.DataPoint)
	for _, dp := range pts {
		idx[dp.Name] = append(idx[dp.Name], dp)
	}
	return idx
}

func hasTag(dp data_store.DataPoint, key, val string) bool {
	for _, tag := range dp.Tags {
		if tag.Key == key && tag.Value == val {
			return true
		}
	}
	return false
}

// TestBuildDatapoints_Full verifies the happy path with a complete INFO map.
func TestBuildDatapoints_Full(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := fullInfoMap()
	pts := p.buildDatapoints(info, fullCmdStats(), nil, nil, now, 1)
	idx := indexDatapoints(pts)

	// up=1 present
	if ups := idx["senhub.db.up"]; len(ups) != 1 || ups[0].Value != 1 {
		t.Errorf("senhub.db.up: want 1, got %v", idx["senhub.db.up"])
	}

	// clients.connected value
	if cc := idx["redis.clients.connected"]; len(cc) != 1 || cc[0].Value != 10 {
		t.Errorf("redis.clients.connected: want 10, got %v", idx["redis.clients.connected"])
	}

	// keyspace hits / misses
	if h := idx["redis.keyspace.hits"]; len(h) != 1 || h[0].Value != 900 {
		t.Errorf("redis.keyspace.hits: want 900, got %v", idx["redis.keyspace.hits"])
	}
	if m := idx["redis.keyspace.misses"]; len(m) != 1 || m[0].Value != 100 {
		t.Errorf("redis.keyspace.misses: want 100, got %v", idx["redis.keyspace.misses"])
	}

	// hit ratio = 900/1000 = 0.9
	if hr := idx["redis.keyspace.hit.ratio"]; len(hr) != 1 {
		t.Errorf("redis.keyspace.hit.ratio missing")
	} else {
		got := hr[0].Value
		if got < 0.899 || got > 0.901 {
			t.Errorf("redis.keyspace.hit.ratio = %.4f, want ~0.9", got)
		}
	}

	// per-db keys: two entries
	dbKeys := idx["redis.db.keys"]
	if len(dbKeys) != 2 {
		t.Errorf("redis.db.keys: want 2 entries, got %d", len(dbKeys))
	}
	found0 := false
	for _, dp := range dbKeys {
		if hasTag(dp, "db", "0") {
			found0 = true
			if dp.Value != 100 {
				t.Errorf("redis.db.keys{db=0} = %v, want 100", dp.Value)
			}
		}
	}
	if !found0 {
		t.Error("redis.db.keys: no entry with tag db=0")
	}

	// every datapoint has instance and db.system.name=redis tags
	for _, dp := range pts {
		if !hasTag(dp, "db.system.name", "redis") {
			t.Errorf("datapoint %q missing db.system.name=redis tag", dp.Name)
		}
		if !hasTag(dp, "instance", p.instance) {
			t.Errorf("datapoint %q missing instance tag", dp.Name)
		}
	}
}

// TestBuildDatapoints_Down verifies that only senhub.db.up=0 is emitted
// when up=0.
func TestBuildDatapoints_Down(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	pts := p.buildDatapoints(map[string]string{}, nil, nil, nil, now, 0)
	if len(pts) != 1 {
		t.Fatalf("down: want 1 datapoint, got %d", len(pts))
	}
	if pts[0].Name != "senhub.db.up" || pts[0].Value != 0 {
		t.Errorf("down: want senhub.db.up=0, got %+v", pts[0])
	}
}

// TestBuildDatapoints_NoKeyspace verifies that no redis.db.keys are
// emitted when the keyspace section is absent.
func TestBuildDatapoints_NoKeyspace(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"redis_version":     "7.0.1",
		"connected_clients": "1",
		"role":              "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if _, ok := idx["redis.db.keys"]; ok {
		t.Error("redis.db.keys should not be emitted without keyspace section")
	}
}

// TestBuildDatapoints_HitRatioZeroDivision verifies that zero hits and
// misses yields hit.ratio=0 without a panic or NaN.
func TestBuildDatapoints_HitRatioZeroDivision(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"keyspace_hits":   "0",
		"keyspace_misses": "0",
		"role":            "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	hr := idx["redis.keyspace.hit.ratio"]
	if len(hr) != 1 {
		t.Fatal("redis.keyspace.hit.ratio should always be emitted")
	}
	if hr[0].Value != 0 {
		t.Errorf("hit.ratio with 0/0 = %v, want 0", hr[0].Value)
	}
}

// TestBuildDatapoints_CpuTime verifies all four cpu.time state variants.
func TestBuildDatapoints_CpuTime(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"used_cpu_sys":           "1.5",
		"used_cpu_user":          "0.8",
		"used_cpu_sys_children":  "0.1",
		"used_cpu_user_children": "0.05",
		"role":                   "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)

	cpuPts := idx["redis.cpu.time"]
	if len(cpuPts) != 4 {
		t.Fatalf("redis.cpu.time: want 4 datapoints (one per state), got %d", len(cpuPts))
	}
	wantStates := map[string]float64{
		"sys":           1.5,
		"user":          0.8,
		"sys_children":  0.1,
		"user_children": 0.05,
	}
	for state, wantVal := range wantStates {
		found := false
		for _, dp := range cpuPts {
			if hasTag(dp, "state", state) {
				found = true
				if dp.Value < wantVal-0.001 || dp.Value > wantVal+0.001 {
					t.Errorf("redis.cpu.time{state=%s} = %v, want %v", state, dp.Value, wantVal)
				}
			}
		}
		if !found {
			t.Errorf("redis.cpu.time: missing state=%s", state)
		}
	}
}

// TestBuildDatapoints_DbAvgTTL verifies that avg_ttl is emitted per-db.
func TestBuildDatapoints_DbAvgTTL(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"db0":  "keys=100,expires=5,avg_ttl=3000",
		"db1":  "keys=200,expires=0,avg_ttl=0",
		"role": "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)

	ttlPts := idx["redis.db.avg_ttl"]
	if len(ttlPts) != 2 {
		t.Fatalf("redis.db.avg_ttl: want 2 entries (db0+db1), got %d", len(ttlPts))
	}
	for _, dp := range ttlPts {
		if hasTag(dp, "db", "0") && dp.Value != 3000 {
			t.Errorf("redis.db.avg_ttl{db=0} = %v, want 3000", dp.Value)
		}
		if hasTag(dp, "db", "1") && dp.Value != 0 {
			t.Errorf("redis.db.avg_ttl{db=1} = %v, want 0", dp.Value)
		}
	}
}

// TestBuildDatapoints_MemoryLua verifies redis.memory.lua is emitted.
func TestBuildDatapoints_MemoryLua(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{"used_memory_lua": "37888", "role": "master"}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if lua := idx["redis.memory.lua"]; len(lua) != 1 || lua[0].Value != 37888 {
		t.Errorf("redis.memory.lua: want 37888, got %v", idx["redis.memory.lua"])
	}
}

// TestBuildDatapoints_ClientBuffers verifies max input/output buffer metrics.
func TestBuildDatapoints_ClientBuffers(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"client_recent_max_input_buffer":  "32768",
		"client_recent_max_output_buffer": "65536",
		"role":                            "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if v := idx["redis.clients.max_input_buffer"]; len(v) != 1 || v[0].Value != 32768 {
		t.Errorf("redis.clients.max_input_buffer: want 32768, got %v", v)
	}
	if v := idx["redis.clients.max_output_buffer"]; len(v) != 1 || v[0].Value != 65536 {
		t.Errorf("redis.clients.max_output_buffer: want 65536, got %v", v)
	}
}

// TestBuildDatapoints_ForkDuration verifies redis.latest.fork.
func TestBuildDatapoints_ForkDuration(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{"latest_fork_usec": "350", "role": "master"}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if v := idx["redis.latest.fork"]; len(v) != 1 || v[0].Value != 350 {
		t.Errorf("redis.latest.fork: want 350, got %v", v)
	}
}

// TestBuildDatapoints_ReplicationBacklog verifies replication backlog metric.
func TestBuildDatapoints_ReplicationBacklog(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"repl_backlog_first_byte_offset": "99999",
		"role":                           "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if v := idx["redis.replication.backlog_first_byte_offset"]; len(v) != 1 || v[0].Value != 99999 {
		t.Errorf("redis.replication.backlog_first_byte_offset: want 99999, got %v", v)
	}
}

// TestBuildDatapoints_EvictedExpiredKeys verifies evicted_keys and expired_keys.
func TestBuildDatapoints_EvictedExpiredKeys(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"evicted_keys": "42",
		"expired_keys": "17",
		"role":         "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if v := idx["redis.evicted_keys"]; len(v) != 1 || v[0].Value != 42 {
		t.Errorf("redis.evicted_keys: want 42, got %v", v)
	}
	if v := idx["redis.expired_keys"]; len(v) != 1 || v[0].Value != 17 {
		t.Errorf("redis.expired_keys: want 17, got %v", v)
	}
}

// TestBuildDatapoints_RdbLastSaveAge verifies that rdb.last_save.age is
// computed as now − rdb_last_save_time.
func TestBuildDatapoints_RdbLastSaveAge(t *testing.T) {
	p := newTestProbe(t)
	now := time.Unix(1700001000, 0)
	info := map[string]string{
		"rdb_last_save_time":       "1700000000", // 1000s before now
		"rdb_last_bgsave_time_sec": "2",
		"role":                     "master",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if v := idx["redis.rdb.last_save.age"]; len(v) != 1 {
		t.Fatal("redis.rdb.last_save.age missing")
	} else if v[0].Value < 999 || v[0].Value > 1001 {
		t.Errorf("redis.rdb.last_save.age = %v, want ~1000", v[0].Value)
	}
	if v := idx["redis.rdb.last_bgsave.duration"]; len(v) != 1 || v[0].Value != 2 {
		t.Errorf("redis.rdb.last_bgsave.duration: want 2, got %v", v)
	}
}

// TestBuildDatapoints_CommandStats verifies per-command metrics.
func TestBuildDatapoints_CommandStats(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	cmdStats := map[string]cmdStat{
		"get": {calls: 1000, usec: 5000},
		"set": {calls: 200, usec: 1200},
	}
	pts := p.buildDatapoints(map[string]string{"role": "master"}, cmdStats, nil, nil, now, 1)
	idx := indexDatapoints(pts)

	callsPts := idx["redis.cmd.calls"]
	if len(callsPts) != 2 {
		t.Fatalf("redis.cmd.calls: want 2 entries (get+set), got %d", len(callsPts))
	}
	usecPts := idx["redis.cmd.usec"]
	if len(usecPts) != 2 {
		t.Fatalf("redis.cmd.usec: want 2 entries (get+set), got %d", len(usecPts))
	}

	for _, dp := range callsPts {
		switch {
		case hasTag(dp, "cmd", "get") && dp.Value != 1000:
			t.Errorf("redis.cmd.calls{cmd=get} = %v, want 1000", dp.Value)
		case hasTag(dp, "cmd", "set") && dp.Value != 200:
			t.Errorf("redis.cmd.calls{cmd=set} = %v, want 200", dp.Value)
		}
	}
}

// TestBuildDatapoints_ClusterMetrics verifies that cluster metrics are emitted
// when clusterInfo is provided and that cluster_state is correctly mapped.
func TestBuildDatapoints_ClusterMetrics(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()

	clusterInfo := map[string]string{
		"cluster_state":                   "ok",
		"cluster_slots_assigned":          "16384",
		"cluster_slots_ok":                "16384",
		"cluster_slots_pfail":             "0",
		"cluster_slots_fail":              "0",
		"cluster_stats_messages_sent":     "1000",
		"cluster_stats_messages_received": "900",
	}

	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, clusterInfo, nil, now, 1)
	idx := indexDatapoints(pts)

	if v := idx["redis.cluster.state"]; len(v) != 1 || v[0].Value != 1 {
		t.Errorf("redis.cluster.state: want 1 (ok), got %v", v)
	}
	if v := idx["redis.cluster.slots.assigned"]; len(v) != 1 || v[0].Value != 16384 {
		t.Errorf("redis.cluster.slots.assigned: want 16384, got %v", v)
	}
	if v := idx["redis.cluster.slots.ok"]; len(v) != 1 || v[0].Value != 16384 {
		t.Errorf("redis.cluster.slots.ok: want 16384, got %v", v)
	}
	if v := idx["redis.cluster.slots.pfail"]; len(v) != 1 || v[0].Value != 0 {
		t.Errorf("redis.cluster.slots.pfail: want 0, got %v", v)
	}
	if v := idx["redis.cluster.slots.fail"]; len(v) != 1 || v[0].Value != 0 {
		t.Errorf("redis.cluster.slots.fail: want 0, got %v", v)
	}
	if v := idx["redis.cluster.links.created"]; len(v) != 1 || v[0].Value != 1000 {
		t.Errorf("redis.cluster.links.created: want 1000, got %v", v)
	}
	if v := idx["redis.cluster.links.disconnected"]; len(v) != 1 || v[0].Value != 900 {
		t.Errorf("redis.cluster.links.disconnected: want 900, got %v", v)
	}
}

// TestBuildDatapoints_ClusterStateFail verifies that cluster_state=fail → 0.
func TestBuildDatapoints_ClusterStateFail(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()

	clusterInfo := map[string]string{
		"cluster_state":          "fail",
		"cluster_slots_assigned": "16384",
		"cluster_slots_ok":       "16000",
		"cluster_slots_pfail":    "100",
		"cluster_slots_fail":     "284",
	}

	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, clusterInfo, nil, now, 1)
	idx := indexDatapoints(pts)

	if v := idx["redis.cluster.state"]; len(v) != 1 || v[0].Value != 0 {
		t.Errorf("redis.cluster.state: want 0 (fail), got %v", v)
	}
	if v := idx["redis.cluster.slots.pfail"]; len(v) != 1 || v[0].Value != 100 {
		t.Errorf("redis.cluster.slots.pfail: want 100, got %v", v)
	}
	if v := idx["redis.cluster.slots.fail"]; len(v) != 1 || v[0].Value != 284 {
		t.Errorf("redis.cluster.slots.fail: want 284, got %v", v)
	}
}

// TestBuildDatapoints_ClusterNil verifies that no cluster metrics are emitted
// when clusterInfo is nil (cluster_enabled=0 or standalone mode).
func TestBuildDatapoints_ClusterNil(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if _, ok := idx["redis.cluster.state"]; ok {
		t.Error("redis.cluster.state should not be emitted when clusterInfo is nil")
	}
}

// TestBuildDatapoints_SentinelMetrics verifies sentinel metrics are emitted
// from a multi-master INFO sentinel map.
func TestBuildDatapoints_SentinelMetrics(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()

	// Two masters: master0 status=ok (2 slaves, 3 sentinels),
	// master1 status=ok (1 slave, 3 sentinels).
	sentinelInfo := map[string]string{
		"sentinel_masters":         "2",
		"sentinel_running_scripts": "0",
		"master0":                  "name=mymaster,status=ok,address=127.0.0.1:6379,slaves=2,sentinels=3",
		"master1":                  "name=other,status=ok,address=127.0.0.1:6380,slaves=1,sentinels=3",
	}

	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, nil, sentinelInfo, now, 1)
	idx := indexDatapoints(pts)

	if v := idx["redis.sentinel.masters"]; len(v) != 1 || v[0].Value != 2 {
		t.Errorf("redis.sentinel.masters: want 2, got %v", v)
	}
	if v := idx["redis.sentinel.scripts_queue_length"]; len(v) != 1 || v[0].Value != 0 {
		t.Errorf("redis.sentinel.scripts_queue_length: want 0, got %v", v)
	}
	if v := idx["redis.sentinel.slaves"]; len(v) != 1 || v[0].Value != 3 {
		t.Errorf("redis.sentinel.slaves: want 3 total (2+1), got %v", v)
	}
	if v := idx["redis.sentinel.ok_slaves"]; len(v) != 1 || v[0].Value != 3 {
		t.Errorf("redis.sentinel.ok_slaves: want 3 (both ok), got %v", v)
	}
	if v := idx["redis.sentinel.sentinels"]; len(v) != 1 || v[0].Value != 6 {
		t.Errorf("redis.sentinel.sentinels: want 6 total (3+3), got %v", v)
	}
	if v := idx["redis.sentinel.ok_sentinels"]; len(v) != 1 || v[0].Value != 6 {
		t.Errorf("redis.sentinel.ok_sentinels: want 6, got %v", v)
	}
}

// TestBuildDatapoints_SentinelPartialDown verifies ok_slaves/ok_sentinels
// exclude masters whose status is not "ok".
func TestBuildDatapoints_SentinelPartialDown(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()

	sentinelInfo := map[string]string{
		"sentinel_masters": "2",
		// master0 is ok: contributes its slaves/sentinels to ok counts.
		"master0": "name=mymaster,status=ok,address=127.0.0.1:6379,slaves=2,sentinels=3",
		// master1 is down: slaves/sentinels not counted as ok.
		"master1": "name=other,status=down,address=127.0.0.1:6380,slaves=1,sentinels=3",
	}

	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, nil, sentinelInfo, now, 1)
	idx := indexDatapoints(pts)

	if v := idx["redis.sentinel.slaves"]; len(v) != 1 || v[0].Value != 3 {
		t.Errorf("redis.sentinel.slaves: want 3 total, got %v", v)
	}
	if v := idx["redis.sentinel.ok_slaves"]; len(v) != 1 || v[0].Value != 2 {
		t.Errorf("redis.sentinel.ok_slaves: want 2 (only master0 ok), got %v", v)
	}
	if v := idx["redis.sentinel.sentinels"]; len(v) != 1 || v[0].Value != 6 {
		t.Errorf("redis.sentinel.sentinels: want 6 total, got %v", v)
	}
	if v := idx["redis.sentinel.ok_sentinels"]; len(v) != 1 || v[0].Value != 3 {
		t.Errorf("redis.sentinel.ok_sentinels: want 3 (only master0 ok), got %v", v)
	}
}

// TestBuildDatapoints_SentinelNil verifies no sentinel metrics are emitted
// when sentinelInfo is nil (standalone / cluster mode).
func TestBuildDatapoints_SentinelNil(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	pts := p.buildDatapoints(map[string]string{"role": "master"}, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if _, ok := idx["redis.sentinel.masters"]; ok {
		t.Error("redis.sentinel.masters should not be emitted when sentinelInfo is nil")
	}
}

// TestBuildDatapoints_TrackingMetrics verifies that tracking_clients and
// tracking_table_used_keys are emitted when present in the INFO map.
func TestBuildDatapoints_TrackingMetrics(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{
		"role":                     "master",
		"tracking_clients":         "5",
		"tracking_table_used_keys": "1200",
	}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)

	if v := idx["redis.tracking.clients"]; len(v) != 1 || v[0].Value != 5 {
		t.Errorf("redis.tracking.clients: want 5, got %v", v)
	}
	if v := idx["redis.tracking.keys"]; len(v) != 1 || v[0].Value != 1200 {
		t.Errorf("redis.tracking.keys: want 1200, got %v", v)
	}
}

// TestBuildDatapoints_TrackingAbsent verifies that no tracking metrics are
// emitted when tracking fields are absent from INFO (RESP2 / no tracking active).
func TestBuildDatapoints_TrackingAbsent(t *testing.T) {
	p := newTestProbe(t)
	now := time.Now()
	info := map[string]string{"role": "master"}
	pts := p.buildDatapoints(info, nil, nil, nil, now, 1)
	idx := indexDatapoints(pts)
	if _, ok := idx["redis.tracking.clients"]; ok {
		t.Error("redis.tracking.clients should not be emitted when field absent")
	}
	if _, ok := idx["redis.tracking.keys"]; ok {
		t.Error("redis.tracking.keys should not be emitted when field absent")
	}
}

// TestParseSentinelMasterStats verifies the aggregate parser for sentinel masters.
func TestParseSentinelMasterStats(t *testing.T) {
	m := map[string]string{
		"sentinel_masters": "2",
		"master0":          "name=m0,status=ok,address=127.0.0.1:6379,slaves=2,sentinels=3",
		"master1":          "name=m1,status=down,address=127.0.0.1:6380,slaves=1,sentinels=2",
	}
	totalSlaves, okSlaves, totalSentinels, okSentinels := parseSentinelMasterStats(m)
	if totalSlaves != 3 {
		t.Errorf("totalSlaves = %d, want 3", totalSlaves)
	}
	if okSlaves != 2 {
		t.Errorf("okSlaves = %d, want 2 (only master0)", okSlaves)
	}
	if totalSentinels != 5 {
		t.Errorf("totalSentinels = %d, want 5", totalSentinels)
	}
	if okSentinels != 3 {
		t.Errorf("okSentinels = %d, want 3 (only master0)", okSentinels)
	}
}

// TestParseCommandStats verifies the commandstats parser.
func TestParseCommandStats(t *testing.T) {
	blob := "# Commandstats\r\n" +
		"cmdstat_get:calls=1000,usec=5000,usec_per_call=5.00,rejected_calls=0,failed_calls=0\r\n" +
		"cmdstat_set:calls=200,usec=1200,usec_per_call=6.00,rejected_calls=0,failed_calls=0\r\n" +
		"cmdstat_info:calls=5,usec=100,usec_per_call=20.00,rejected_calls=0,failed_calls=0\r\n"

	stats := parseCommandStats(blob)
	if len(stats) != 3 {
		t.Fatalf("parseCommandStats: want 3 entries, got %d: %v", len(stats), stats)
	}
	if stats["get"].calls != 1000 {
		t.Errorf("get.calls = %d, want 1000", stats["get"].calls)
	}
	if stats["get"].usec != 5000 {
		t.Errorf("get.usec = %d, want 5000", stats["get"].usec)
	}
	if stats["set"].calls != 200 {
		t.Errorf("set.calls = %d, want 200", stats["set"].calls)
	}
	if stats["info"].usec != 100 {
		t.Errorf("info.usec = %d, want 100", stats["info"].usec)
	}
}

// respBulkString wraps body in a RESP bulk string response.
func respBulkString(body string) string {
	return fmt.Sprintf("$%d\r\n%s\r\n", len(body), body)
}

// fakeServer returns a net.Conn that on read sends the provided RESP
// response. It accepts one write (the AUTH or INFO command) and
// responds with the pre-canned reply.
func fakeServer(t *testing.T, responses ...string) net.Conn {
	t.Helper()
	clientSide, serverSide := net.Pipe()

	go func() {
		defer serverSide.Close()
		br := bufio.NewReader(serverSide)
		for _, resp := range responses {
			// Drain the incoming command line-by-line until we see a complete
			// RESP array (consume lines until we've read all parts).
			header, err := br.ReadString('\n')
			if err != nil {
				return
			}
			header = strings.TrimRight(header, "\r\n")
			if strings.HasPrefix(header, "*") {
				n, _ := strconv.Atoi(header[1:])
				for i := 0; i < n*2; i++ {
					if _, err := br.ReadString('\n'); err != nil {
						return
					}
				}
			}
			_, _ = serverSide.Write([]byte(resp))
		}
	}()

	return clientSide
}

// TestSeam_ConnectAndParse injects a fake dial function returning a
// canned INFO response and verifies Collect emits up=1 and expected metrics.
func TestSeam_ConnectAndParse(t *testing.T) {
	infoBody := "# Server\r\nredis_version:7.0.1\r\nuptime_in_seconds:100\r\n\r\n" +
		"# Clients\r\nconnected_clients:5\r\nblocked_clients:0\r\n\r\n" +
		"# Memory\r\nused_memory:1024\r\nused_memory_rss:2048\r\nused_memory_peak:2048\r\nmem_fragmentation_ratio:2.0\r\n\r\n" +
		"# Stats\r\ntotal_commands_processed:1000\r\ntotal_net_input_bytes:4096\r\ntotal_net_output_bytes:8192\r\ninstantaneous_ops_per_sec:10\r\nkeyspace_hits:90\r\nkeyspace_misses:10\r\nrejected_connections:0\r\ntotal_connections_received:50\r\n\r\n" +
		"# Keyspace\r\ndb0:keys=50,expires=5,avg_ttl=1000\r\n\r\n" +
		"# Replication\r\nrole:master\r\nmaster_repl_offset:500\r\nconnected_slaves:0\r\n\r\n" +
		"# Persistence\r\nrdb_changes_since_last_save:3\r\naof_enabled:1\r\n"

	cmdStatsBody := "# Commandstats\r\n" +
		"cmdstat_get:calls=100,usec=500,usec_per_call=5.00,rejected_calls=0,failed_calls=0\r\n"

	p := newTestProbe(t)
	p.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return fakeServer(t, respBulkString(infoBody), respBulkString(cmdStatsBody)), nil
	}

	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	idx := indexDatapoints(pts)
	if up := idx["senhub.db.up"]; len(up) == 0 || up[0].Value != 1 {
		t.Errorf("senhub.db.up: want 1, got %v", idx["senhub.db.up"])
	}
	if cc := idx["redis.clients.connected"]; len(cc) == 0 || cc[0].Value != 5 {
		t.Errorf("redis.clients.connected: want 5, got %v", idx["redis.clients.connected"])
	}
	if _, ok := idx["redis.db.keys"]; !ok {
		t.Error("redis.db.keys missing from output")
	}
	// avg_ttl from keyspace
	if ttl := idx["redis.db.avg_ttl"]; len(ttl) == 0 {
		t.Error("redis.db.avg_ttl missing from output")
	}
	// per-command metrics from INFO commandstats
	if calls := idx["redis.cmd.calls"]; len(calls) == 0 {
		t.Error("redis.cmd.calls missing from output")
	}
}

// TestSeam_AuthFailure injects a fake server that rejects AUTH and
// verifies Collect emits up=0 without returning an error.
func TestSeam_AuthFailure(t *testing.T) {
	p, err := NewRedisProbe(map[string]interface{}{"password": "wrong"}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	rp := p.(*redisProbe)
	rp.SetName("redis-auth-fail")
	rp.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return fakeServer(t, "-WRONGPASS invalid username-password pair or user is disabled\r\n"), nil
	}

	pts, err := rp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	idx := indexDatapoints(pts)
	if up := idx["senhub.db.up"]; len(up) == 0 || up[0].Value != 0 {
		t.Errorf("senhub.db.up: want 0 on auth failure, got %v", idx["senhub.db.up"])
	}
}

// TestSeam_ConnectError verifies that a dial error yields senhub.db.up=0
// without a collection error.
func TestSeam_ConnectError(t *testing.T) {
	p := newTestProbe(t)
	p.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, fmt.Errorf("connection refused")
	}
	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	idx := indexDatapoints(pts)
	if up := idx["senhub.db.up"]; len(up) == 0 || up[0].Value != 0 {
		t.Errorf("senhub.db.up: want 0 on dial error, got %v", idx["senhub.db.up"])
	}
}

// TestEntityObserver_HostPortFallback verifies that when no instance_name is
// set, db.instance.id is pinned to host:port at construction and the entity
// is emitted immediately after the first update (ok=true).
func TestEntityObserver_HostPortFallback(t *testing.T) {
	cfg := probeConfig{Host: "10.0.0.1", Port: 6379}
	hostPort := "10.0.0.1:6379"
	obs := newEntityObserver(cfg, hostPort)

	// Before first update: ok=false.
	if _, ok := obs.Observe(); ok {
		t.Fatal("Observe ok=true before first update, want false")
	}

	info := map[string]string{"redis_version": "7.2.0"}
	obs.update(cfg, info)

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe ok=false after update")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got.Entities))
	}
	e := got.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity type = %q, want db", e.Type)
	}
	if e.ID["db.instance.id"] != hostPort {
		t.Errorf("db.instance.id = %v, want %s", e.ID["db.instance.id"], hostPort)
	}
	if e.Attributes["db.system.name"] != "redis" {
		t.Errorf("db.system.name = %v, want redis", e.Attributes["db.system.name"])
	}
	if e.Attributes["server.address"] != cfg.Host {
		t.Errorf("server.address = %v, want %s", e.Attributes["server.address"], cfg.Host)
	}
	if e.Attributes["db.system.version"] != "7.2.0" {
		t.Errorf("db.system.version = %v, want 7.2.0", e.Attributes["db.system.version"])
	}
	if _, stale := e.Attributes["db.version"]; stale {
		t.Error("legacy db.version key must no longer be emitted (toise#216 AT1)")
	}
}

// TestEntityObserver_InstanceNameOverride verifies that when instance_name is
// set in config, it is used verbatim as db.instance.id instead of host:port.
func TestEntityObserver_InstanceNameOverride(t *testing.T) {
	cfg := probeConfig{Host: "10.0.0.1", Port: 6379, InstanceName: "prod-redis-primary"}
	hostPort := "10.0.0.1:6379"
	obs := newEntityObserver(cfg, hostPort)
	obs.update(cfg, map[string]string{})

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe ok=false after update")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(got.Entities))
	}
	e := got.Entities[0]
	if e.ID["db.instance.id"] != "prod-redis-primary" {
		t.Errorf("db.instance.id = %v, want prod-redis-primary", e.ID["db.instance.id"])
	}
	// server.address must still be present as a descriptive attribute.
	if e.Attributes["server.address"] != cfg.Host {
		t.Errorf("server.address = %v, want %s", e.Attributes["server.address"], cfg.Host)
	}
}

// TestEntityObserver_IDImmutable verifies that calling update multiple times
// does NOT change the pinned db.instance.id.
func TestEntityObserver_IDImmutable(t *testing.T) {
	cfg := probeConfig{Host: "10.0.0.1", Port: 6379}
	hostPort := "10.0.0.1:6379"
	obs := newEntityObserver(cfg, hostPort)

	obs.update(cfg, map[string]string{"redis_version": "7.0.0"})
	first, _ := obs.Observe()
	idFirst := first.Entities[0].ID["db.instance.id"]

	obs.update(cfg, map[string]string{"redis_version": "7.2.0"})
	second, _ := obs.Observe()
	idSecond := second.Entities[0].ID["db.instance.id"]

	if idFirst != idSecond {
		t.Errorf("db.instance.id changed between updates: %v → %v", idFirst, idSecond)
	}
}

// TestEntityObserver_NotOKBeforeFirstUpdate verifies that Observe returns
// ok=false before any update call (the db entity must not be emitted until
// the probe has successfully collected at least once).
func TestEntityObserver_NotOKBeforeFirstUpdate(t *testing.T) {
	cfg := probeConfig{Host: "127.0.0.1", Port: 6379}
	obs := newEntityObserver(cfg, "127.0.0.1:6379")
	if _, ok := obs.Observe(); ok {
		t.Error("Observe should return ok=false before the first update")
	}
}

// TestEntityObserver_MonitorsEdgePresent verifies that when agentstate carries
// a non-empty agent instance id, the Observation contains a "monitors" relation
// from the service.instance to the db entity.
func TestEntityObserver_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("test-agent-id")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	cfg := probeConfig{Host: "10.0.0.1", Port: 6379}
	hostPort := "10.0.0.1:6379"
	obs := newEntityObserver(cfg, hostPort)
	obs.update(cfg, map[string]string{})

	got, _ := obs.Observe()
	if len(got.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(got.Relations))
	}
	rel := got.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", rel.FromType)
	}
	if rel.FromID["service.instance.id"] != "test-agent-id" {
		t.Errorf("FromID.service.instance.id = %v, want test-agent-id", rel.FromID["service.instance.id"])
	}
	if rel.ToType != "db" {
		t.Errorf("ToType = %q, want db", rel.ToType)
	}
	if rel.ToID["db.instance.id"] != hostPort {
		t.Errorf("ToID.db.instance.id = %v, want %s", rel.ToID["db.instance.id"], hostPort)
	}
}

// TestEntityObserver_MonitorsEdgeAbsentWhenNoAgentID verifies that when
// agentstate returns an empty agent id, no "monitors" relation is emitted.
func TestEntityObserver_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	cfg := probeConfig{Host: "10.0.0.1", Port: 6379}
	obs := newEntityObserver(cfg, "10.0.0.1:6379")
	obs.update(cfg, map[string]string{})

	got, _ := obs.Observe()
	if len(got.Relations) != 0 {
		t.Errorf("expected no relations when agentID empty, got %d", len(got.Relations))
	}
}
