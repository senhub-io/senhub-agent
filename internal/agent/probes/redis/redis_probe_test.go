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
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

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
		ok      bool
	}{
		{"db0:keys=100,expires=5,avg_ttl=3000", "0", 100, 5, true},
		{"db1:keys=0,expires=0,avg_ttl=0", "1", 0, 0, true},
		{"db12:keys=999,expires=10,avg_ttl=500", "12", 999, 10, true},
		{"notadb:keys=1,expires=0", "", 0, 0, false},
		{"nocoron", "", 0, 0, false},
	}
	for _, tc := range cases {
		db, keys, expires, ok := parseKeyspaceLine(tc.line)
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
	}
}

// infoMap returns a complete synthetic INFO map suitable for
// TestBuildDatapoints_Full.
func fullInfoMap() map[string]string {
	return map[string]string{
		"redis_version":               "7.0.1",
		"uptime_in_seconds":           "12345",
		"connected_clients":           "10",
		"blocked_clients":             "2",
		"total_connections_received":  "5000",
		"rejected_connections":        "3",
		"used_memory":                 "1048576",
		"used_memory_rss":             "2097152",
		"used_memory_peak":            "3145728",
		"mem_fragmentation_ratio":     "1.5",
		"total_commands_processed":    "100000",
		"total_net_input_bytes":       "204800",
		"total_net_output_bytes":      "409600",
		"instantaneous_ops_per_sec":   "500",
		"keyspace_hits":               "900",
		"keyspace_misses":             "100",
		"db0":                         "keys=100,expires=5,avg_ttl=3000",
		"db1":                         "keys=200,expires=10,avg_ttl=1000",
		"role":                        "master",
		"master_repl_offset":          "99999",
		"connected_slaves":            "1",
		"rdb_changes_since_last_save": "7",
		"aof_enabled":                 "0",
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
	pts := p.buildDatapoints(info, now, 1)
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
	pts := p.buildDatapoints(map[string]string{}, now, 0)
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
	pts := p.buildDatapoints(info, now, 1)
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
	pts := p.buildDatapoints(info, now, 1)
	idx := indexDatapoints(pts)
	hr := idx["redis.keyspace.hit.ratio"]
	if len(hr) != 1 {
		t.Fatal("redis.keyspace.hit.ratio should always be emitted")
	}
	if hr[0].Value != 0 {
		t.Errorf("hit.ratio with 0/0 = %v, want 0", hr[0].Value)
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

	p := newTestProbe(t)
	p.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return fakeServer(t, respBulkString(infoBody)), nil
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

// TestEntityObserver_Update verifies the entityObserver builds the
// correct entity shape after a successful collection.
func TestEntityObserver_Update(t *testing.T) {
	cfg := probeConfig{Host: "10.0.0.1", Port: 6379}
	instance := "10.0.0.1:6379"
	info := map[string]string{"redis_version": "7.2.0"}

	var obs entityObserver
	obs.update(cfg, instance, info)

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
	if e.ID["db.instance.id"] != instance {
		t.Errorf("db.instance.id = %v, want %s", e.ID["db.instance.id"], instance)
	}
	if e.Attributes["db.system.name"] != "redis" {
		t.Errorf("db.system.name = %v, want redis", e.Attributes["db.system.name"])
	}
	if e.Attributes["db.version"] != "7.2.0" {
		t.Errorf("db.version = %v, want 7.2.0", e.Attributes["db.version"])
	}
}
