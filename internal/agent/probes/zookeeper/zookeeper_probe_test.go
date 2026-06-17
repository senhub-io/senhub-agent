package zookeeper

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

// stubDial returns a fake connection that serves the given mntr payload.
// The server side drains any incoming bytes (the "mntr\n" command) before
// writing the response, because net.Pipe is synchronous: a write blocks
// until the peer reads.
func stubDial(payload string) func(string, string, time.Duration) (net.Conn, error) {
	return func(network, address string, timeout time.Duration) (net.Conn, error) {
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			// Drain the command the probe sends ("mntr\n").
			buf := make([]byte, 64)
			server.Read(buf) //nolint:errcheck
			// Send the mntr response.
			fmt.Fprint(server, payload)
		}()
		return client, nil
	}
}

// errDial always returns an error (simulates unreachable node).
func errDial(_ string, _ string, _ time.Duration) (net.Conn, error) {
	return nil, fmt.Errorf("connection refused")
}

const mntrLeader = `zk_version	3.8.0
zk_avg_latency	2
zk_max_latency	15
zk_min_latency	0
zk_packets_received	1200
zk_packets_sent	1198
zk_num_alive_connections	5
zk_outstanding_requests	0
zk_server_state	leader
zk_znode_count	342
zk_watch_count	18
zk_ephemerals_count	7
zk_approximate_data_size	98304
zk_open_file_descriptor_count	42
zk_max_file_descriptor_count	1024
zk_followers	2
zk_synced_followers	2
zk_pending_syncs	0
`

const mntrFollower = `zk_version	3.8.0
zk_avg_latency	1
zk_max_latency	10
zk_min_latency	0
zk_packets_received	800
zk_packets_sent	798
zk_num_alive_connections	3
zk_outstanding_requests	0
zk_server_state	follower
zk_znode_count	342
zk_watch_count	10
zk_ephemerals_count	4
zk_approximate_data_size	98304
zk_open_file_descriptor_count	30
zk_max_file_descriptor_count	1024
`

func TestNewZookeeperProbe_Defaults(t *testing.T) {
	p, err := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	zp := p.(*ZookeeperProbe)
	if zp.cfg.Host != defaultHost {
		t.Errorf("host: want %q got %q", defaultHost, zp.cfg.Host)
	}
	if zp.cfg.Port != defaultPort {
		t.Errorf("port: want %d got %d", defaultPort, zp.cfg.Port)
	}
	if zp.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout: want %v got %v", defaultTimeout, zp.cfg.Timeout)
	}
	if zp.cfg.Interval != defaultInterval {
		t.Errorf("interval: want %v got %v", defaultInterval, zp.cfg.Interval)
	}
}

func TestNewZookeeperProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"host":     "zk.example.com",
		"port":     2182,
		"timeout":  5,
		"interval": 60,
	}
	p, err := NewZookeeperProbe(cfg, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	zp := p.(*ZookeeperProbe)
	if zp.cfg.Host != "zk.example.com" {
		t.Errorf("host: want zk.example.com got %q", zp.cfg.Host)
	}
	if zp.cfg.Port != 2182 {
		t.Errorf("port: want 2182 got %d", zp.cfg.Port)
	}
	if zp.cfg.Timeout != 5*time.Second {
		t.Errorf("timeout: want 5s got %v", zp.cfg.Timeout)
	}
	if zp.cfg.Interval != 60*time.Second {
		t.Errorf("interval: want 60s got %v", zp.cfg.Interval)
	}
}

func TestCollect_Up_Leader(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrLeader)

	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	byName := make(map[string]float64)
	byNameTag := make(map[string]string) // name -> state tag value
	for _, pt := range pts {
		byName[pt.Name] = pt.Value
		for _, tag := range pt.Tags {
			if tag.Key == "state" {
				byNameTag[pt.Name] = tag.Value
			}
		}
	}

	// up metric must be 1
	if v, ok := byName["senhub.zookeeper.up"]; !ok || v != 1 {
		t.Errorf("senhub.zookeeper.up: want 1, got %v (present=%v)", v, ok)
	}

	// latency
	if v := byName["zookeeper.latency.avg"]; v != 2 {
		t.Errorf("zookeeper.latency.avg: want 2, got %v", v)
	}
	if v := byName["zookeeper.latency.max"]; v != 15 {
		t.Errorf("zookeeper.latency.max: want 15, got %v", v)
	}
	if v := byName["zookeeper.latency.min"]; v != 0 {
		t.Errorf("zookeeper.latency.min: want 0, got %v", v)
	}

	// packets
	if v := byName["zookeeper.packets.received"]; v != 1200 {
		t.Errorf("zookeeper.packets.received: want 1200, got %v", v)
	}
	if v := byName["zookeeper.packets.sent"]; v != 1198 {
		t.Errorf("zookeeper.packets.sent: want 1198, got %v", v)
	}

	// connections / requests
	if v := byName["zookeeper.connections"]; v != 5 {
		t.Errorf("zookeeper.connections: want 5, got %v", v)
	}
	if v := byName["zookeeper.outstanding_requests"]; v != 0 {
		t.Errorf("zookeeper.outstanding_requests: want 0, got %v", v)
	}

	// data
	if v := byName["zookeeper.znodes"]; v != 342 {
		t.Errorf("zookeeper.znodes: want 342, got %v", v)
	}
	if v := byName["zookeeper.watches"]; v != 18 {
		t.Errorf("zookeeper.watches: want 18, got %v", v)
	}
	if v := byName["zookeeper.ephemerals"]; v != 7 {
		t.Errorf("zookeeper.ephemerals: want 7, got %v", v)
	}
	if v := byName["zookeeper.data_size"]; v != 98304 {
		t.Errorf("zookeeper.data_size: want 98304, got %v", v)
	}

	// file descriptors
	if v := byName["zookeeper.file_descriptors.open"]; v != 42 {
		t.Errorf("zookeeper.file_descriptors.open: want 42, got %v", v)
	}
	if v := byName["zookeeper.file_descriptors.max"]; v != 1024 {
		t.Errorf("zookeeper.file_descriptors.max: want 1024, got %v", v)
	}

	// leader-only metrics present
	if v := byName["zookeeper.followers"]; v != 2 {
		t.Errorf("zookeeper.followers: want 2, got %v", v)
	}
	if v := byName["zookeeper.synced_followers"]; v != 2 {
		t.Errorf("zookeeper.synced_followers: want 2, got %v", v)
	}
	if v := byName["zookeeper.pending_syncs"]; v != 0 {
		t.Errorf("zookeeper.pending_syncs: want 0, got %v", v)
	}

	// server_state label metric
	if v := byName["zookeeper.server_state"]; v != 1 {
		t.Errorf("zookeeper.server_state value: want 1, got %v", v)
	}
	if s := byNameTag["zookeeper.server_state"]; s != "leader" {
		t.Errorf("zookeeper.server_state state tag: want leader, got %q", s)
	}
}

func TestCollect_Follower_NoLeaderMetrics(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrFollower)

	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	byName := make(map[string]float64)
	byNameTag := make(map[string]string)
	for _, pt := range pts {
		byName[pt.Name] = pt.Value
		for _, tag := range pt.Tags {
			if tag.Key == "state" {
				byNameTag[pt.Name] = tag.Value
			}
		}
	}

	// Leader-only metrics must NOT be present
	if _, ok := byName["zookeeper.followers"]; ok {
		t.Error("zookeeper.followers should not be emitted on a follower node")
	}
	if _, ok := byName["zookeeper.synced_followers"]; ok {
		t.Error("zookeeper.synced_followers should not be emitted on a follower node")
	}
	if _, ok := byName["zookeeper.pending_syncs"]; ok {
		t.Error("zookeeper.pending_syncs should not be emitted on a follower node")
	}

	// state tag should reflect follower
	if s := byNameTag["zookeeper.server_state"]; s != "follower" {
		t.Errorf("state tag: want follower, got %q", s)
	}
}

func TestCollect_NodeDown_UpZero(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = errDial

	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect must not return error on connection failure, got: %v", err)
	}

	for _, pt := range pts {
		if pt.Name == "senhub.zookeeper.up" {
			if pt.Value != 0 {
				t.Errorf("senhub.zookeeper.up: want 0 on failure, got %v", pt.Value)
			}
			return
		}
	}
	t.Error("senhub.zookeeper.up point not found in output")
}

func TestCollect_EnrichesWithProbeName(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrLeader)
	zp.BaseProbe.SetName("my-zk")

	pts, _ := zp.Collect()
	for _, pt := range pts {
		for _, tag := range pt.Tags {
			if tag.Key == "probe_name" && tag.Value == "my-zk" {
				return
			}
		}
	}
	t.Error("probe_name tag not found in enriched output")
}

func TestParseFloat(t *testing.T) {
	cases := []struct {
		input string
		want  float64
		ok    bool
	}{
		{"42", 42, true},
		{"3.14", 3.14, true},
		{" 0 ", 0, true},
		{"notanumber", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		v, err := parseFloat(c.input)
		if c.ok && err != nil {
			t.Errorf("parseFloat(%q): unexpected error %v", c.input, err)
		}
		if !c.ok && err == nil {
			t.Errorf("parseFloat(%q): expected error, got nil", c.input)
		}
		if c.ok && !floatNear(v, c.want) {
			t.Errorf("parseFloat(%q): want %v got %v", c.input, c.want, v)
		}
	}
}

func floatNear(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.01
}

func TestProbeType(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	if zp.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType: want %q got %q", ProbeType, zp.GetProbeType())
	}
}

func TestGetInterval(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{"interval": 45}, testLogger())
	if p.GetInterval() != 45*time.Second {
		t.Errorf("GetInterval: want 45s got %v", p.GetInterval())
	}
}

func TestMetricTypeTag_Presence(t *testing.T) {
	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(mntrLeader)

	pts, _ := zp.Collect()
	for _, pt := range pts {
		found := false
		for _, tag := range pt.Tags {
			if tag.Key == "metric_type" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("metric_type tag missing on point %q", pt.Name)
		}
	}
}

func TestMntrLineWithoutTab_Skipped(t *testing.T) {
	payload := strings.Join([]string{
		"This line has no tab",
		"zk_avg_latency\t3",
		"another line without tab",
		"",
	}, "\n")

	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = stubDial(payload)

	pts, err := zp.Collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, pt := range pts {
		if pt.Name == "zookeeper.latency.avg" && pt.Value == 3 {
			return
		}
	}
	t.Error("zookeeper.latency.avg (from valid tab-separated line) not found")
}
