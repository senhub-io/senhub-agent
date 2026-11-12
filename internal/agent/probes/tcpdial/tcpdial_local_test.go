package tcpdial

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func newModeProbe(t *testing.T, mode string, targets ...string) *TCPDialProbe {
	t.Helper()
	raw := make([]interface{}, len(targets))
	for i, tg := range targets {
		raw[i] = tg
	}
	probe, err := NewTCPDialProbe(map[string]interface{}{"targets": raw, "timeout": 2, "mode": mode},
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewTCPDialProbe: %v", err)
	}
	p := probe.(*TCPDialProbe)
	p.SetName("tcpdial-local-test")
	return p
}

func fakeTable() []gnet.ConnectionStat {
	pid := int32(os.Getpid())
	return []gnet.ConnectionStat{
		{Status: "LISTEN", Laddr: gnet.Addr{IP: "0.0.0.0", Port: 8080}, Pid: pid},
		{Status: "ESTABLISHED", Laddr: gnet.Addr{IP: "10.0.0.5", Port: 8080}, Raddr: gnet.Addr{IP: "10.0.0.1", Port: 51000}},
		{Status: "ESTABLISHED", Laddr: gnet.Addr{IP: "10.0.0.5", Port: 8080}, Raddr: gnet.Addr{IP: "10.0.0.2", Port: 51001}},
		{Status: "ESTABLISHED", Laddr: gnet.Addr{IP: "10.0.0.5", Port: 8080}, Raddr: gnet.Addr{IP: "10.0.0.1", Port: 51002}},
		{Status: "TIME_WAIT", Laddr: gnet.Addr{IP: "10.0.0.5", Port: 8080}, Raddr: gnet.Addr{IP: "10.0.0.3", Port: 51003}},
		{Status: "ESTABLISHED", Laddr: gnet.Addr{IP: "10.0.0.5", Port: 9090}, Raddr: gnet.Addr{IP: "10.0.0.9", Port: 51004}},
	}
}

func localTagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tg := range tags {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func TestCollect_AutoLocalReadsTable(t *testing.T) {
	p := newModeProbe(t, "auto", "127.0.0.1:8080")
	var dials atomic.Int32
	p.dial = func(_ context.Context, target string) dialResult {
		dials.Add(1)
		return dialResult{target: target, up: true, duration: time.Millisecond}
	}
	p.isLocalIP = func(ip net.IP) bool { return ip.IsLoopback() }
	p.tcpConnections = func() ([]gnet.ConnectionStat, error) { return fakeTable(), nil }
	p.processName = func(pid int32) (string, error) {
		if pid == int32(os.Getpid()) {
			return "fakeproc", nil
		}
		return "", errors.New("no such process")
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("local target was dialed %d times", dials.Load())
	}

	type key struct {
		name  string
		state string
		proc  string
	}
	got := map[key]float64{}
	names := map[string]bool{}
	for _, dp := range points {
		names[dp.Name] = true
		if dp.Name == "senhub.tcpdial.up" || dp.Name == "senhub.tcpdial.duration" {
			t.Errorf("local mode emitted dial metric %s", dp.Name)
		}
		state, _ := localTagValue(dp.Tags, "network.connection.state")
		proc, _ := localTagValue(dp.Tags, "process.executable.name")
		port, ok := localTagValue(dp.Tags, "server.port")
		if !ok || port != "8080" {
			t.Errorf("%s missing server.port=8080", dp.Name)
		}
		if tr, ok := localTagValue(dp.Tags, "network.transport"); !ok || tr != "tcp" {
			t.Errorf("%s missing network.transport=tcp", dp.Name)
		}
		got[key{dp.Name, state, proc}] = dp.Value
	}

	want := map[key]float64{
		{"senhub.tcp.listener.up", "", "fakeproc"}:             1,
		{"senhub.tcp.listener.connections", "established", ""}: 3,
		{"senhub.tcp.listener.peers", "", ""}:                  2,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("series %+v = %v, want %v", k, got[k], v)
		}
	}
	for name := range names {
		switch name {
		case "senhub.tcp.listener.up", "senhub.tcp.listener.connections", "senhub.tcp.listener.peers":
		default:
			t.Errorf("unexpected series %s", name)
		}
	}
}

func TestCollect_AutoRemoteStillDials(t *testing.T) {
	p := newModeProbe(t, "auto", "203.0.113.10:443")
	var dials atomic.Int32
	p.resolveHost = func(host string) (net.IP, error) { return net.ParseIP("203.0.113.10"), nil }
	p.isLocalIP = func(ip net.IP) bool { return false }
	p.tcpConnections = func() ([]gnet.ConnectionStat, error) {
		t.Fatal("TCP table must not be read for a remote target")
		return nil, nil
	}
	p.dial = func(_ context.Context, target string) dialResult {
		dials.Add(1)
		return dialResult{target: target, up: true, duration: 2 * time.Millisecond}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if dials.Load() != 1 {
		t.Fatalf("remote target dialed %d times, want 1", dials.Load())
	}
	found := false
	for _, dp := range points {
		if dp.Name == "senhub.tcpdial.up" && dp.Value == 1 {
			found = true
		}
		if dp.Name == "senhub.tcp.listener.up" {
			t.Error("remote target emitted listener metric")
		}
	}
	if !found {
		t.Error("remote target missing senhub.tcpdial.up=1")
	}
}

func TestCollect_ModeDialOnLocalTargetDials(t *testing.T) {
	p := newModeProbe(t, "dial", "127.0.0.1:8080")
	var dials atomic.Int32
	p.isLocalIP = func(ip net.IP) bool { return true }
	p.tcpConnections = func() ([]gnet.ConnectionStat, error) {
		t.Fatal("mode dial must not read the TCP table")
		return nil, nil
	}
	p.dial = func(_ context.Context, target string) dialResult {
		dials.Add(1)
		return dialResult{target: target, up: true, duration: time.Millisecond}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if dials.Load() != 1 {
		t.Fatalf("mode dial did not dial the local target")
	}
	for _, dp := range points {
		if dp.Name == "senhub.tcp.listener.up" {
			t.Error("mode dial emitted listener metric")
		}
	}
}

func TestCollect_LocalListenerAbsent(t *testing.T) {
	p := newModeProbe(t, "local", "127.0.0.1:9999")
	p.tcpConnections = func() ([]gnet.ConnectionStat, error) { return nil, nil }
	p.dial = func(_ context.Context, target string) dialResult {
		t.Fatal("mode local must not dial")
		return dialResult{target: target}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float64{}
	for _, dp := range points {
		state, _ := localTagValue(dp.Tags, "network.connection.state")
		if _, ok := localTagValue(dp.Tags, "process.executable.name"); ok {
			t.Errorf("%s must not carry a process tag without a listener", dp.Name)
		}
		got[dp.Name+"|"+state] = dp.Value
	}
	want := map[string]float64{
		"senhub.tcp.listener.up|":                     0,
		"senhub.tcp.listener.connections|established": 0,
		"senhub.tcp.listener.peers|":                  0,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("absent listener series %s = %v, want %v", k, got[k], v)
		}
	}
}

var _ = data_store.DataPoint{}

func TestAnUnreadableTCPTableReportsNothingForLocalTargets(t *testing.T) {
	p := newTestProbe(t, "127.0.0.1:8080")
	p.isLocalIP = func(ip net.IP) bool { return ip.IsLoopback() }
	p.tcpConnections = func() ([]gnet.ConnectionStat, error) { return nil, errors.New("permission denied") }
	points, err := p.CollectContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, dp := range points {
		if strings.HasPrefix(dp.Name, "senhub.tcp.listener.") {
			t.Fatalf("%s emitted although the TCP table could not be read", dp.Name)
		}
	}
}
