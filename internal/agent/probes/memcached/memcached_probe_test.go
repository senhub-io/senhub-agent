package memcached

import (
	"fmt"
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestNewMemcachedProbe_Defaults(t *testing.T) {
	l := newTestLogger(t)
	p, err := NewMemcachedProbe(map[string]interface{}{}, l)
	if err != nil {
		t.Fatalf("NewMemcachedProbe() error: %v", err)
	}
	if p == nil {
		t.Fatal("NewMemcachedProbe() returned nil")
	}
	mp := p.(*MemcachedProbe)
	if mp.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", mp.GetProbeType(), ProbeType)
	}
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
	if p.GetInterval() != defaultInterval {
		t.Errorf("GetInterval() = %v, want %v", p.GetInterval(), defaultInterval)
	}
}

func TestNewMemcachedProbe_CustomConfig(t *testing.T) {
	l := newTestLogger(t)
	p, err := NewMemcachedProbe(map[string]interface{}{
		"host":     "myserver",
		"port":     11212,
		"interval": 30,
		"timeout":  3,
	}, l)
	if err != nil {
		t.Fatalf("NewMemcachedProbe() error: %v", err)
	}
	mp := p.(*MemcachedProbe)
	if mp.cfg.Host != "myserver" {
		t.Errorf("host = %q, want %q", mp.cfg.Host, "myserver")
	}
	if mp.cfg.Port != 11212 {
		t.Errorf("port = %d, want %d", mp.cfg.Port, 11212)
	}
	if mp.cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v, want %v", mp.cfg.Interval, 30*time.Second)
	}
	if mp.cfg.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want %v", mp.cfg.Timeout, 3*time.Second)
	}
}

// fakeMemcached starts a minimal Memcached stats server on a random port.
// It replies to "stats\r\n" with a canned set of STAT lines.
func fakeMemcached(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				if string(buf[:n]) != "stats\r\n" {
					return
				}
				response := "" +
					"STAT uptime 12345\r\n" +
					"STAT curr_connections 10\r\n" +
					"STAT total_connections 500\r\n" +
					"STAT curr_items 200\r\n" +
					"STAT total_items 1000\r\n" +
					"STAT bytes 1048576\r\n" +
					"STAT limit_maxbytes 67108864\r\n" +
					"STAT bytes_written 9999\r\n" +
					"STAT bytes_read 8888\r\n" +
					"STAT get_hits 750\r\n" +
					"STAT get_misses 250\r\n" +
					"STAT cmd_get 1000\r\n" +
					"STAT cmd_set 200\r\n" +
					"STAT cmd_flush 5\r\n" +
					"STAT evictions 3\r\n" +
					"STAT rusage_user 1.234567\r\n" +
					"STAT rusage_system 0.567890\r\n" +
					"END\r\n"
				fmt.Fprint(c, response)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestCollect_ReachableServer(t *testing.T) {
	addr, stop := fakeMemcached(t)
	defer stop()

	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	l := newTestLogger(t)
	probe, err := NewMemcachedProbe(map[string]interface{}{
		"host": host,
		"port": port,
	}, l)
	if err != nil {
		t.Fatalf("NewMemcachedProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	byName := make(map[string][]float64)
	for _, dp := range points {
		byName[dp.Name] = append(byName[dp.Name], dp.Value)
	}

	// senhub.memcached.up must be 1
	upVals, ok := byName["senhub.memcached.up"]
	if !ok {
		t.Error("missing senhub.memcached.up")
	} else if upVals[0] != 1 {
		t.Errorf("senhub.memcached.up = %v, want 1", upVals[0])
	}

	// Spot-check a few metrics
	for _, name := range []string{
		"memcached.uptime",
		"memcached.current.connections",
		"memcached.connections.total",
		"memcached.current.items",
		"memcached.items.total",
		"memcached.bytes",
		"memcached.limit_maxbytes",
		"memcached.network",
		"memcached.evictions",
	} {
		if _, found := byName[name]; !found {
			t.Errorf("missing metric %q", name)
		}
	}

	// memcached.network must appear twice: one transmit, one receive
	if len(byName["memcached.network"]) != 2 {
		t.Errorf("memcached.network: got %d points, want 2 (transmit+receive)", len(byName["memcached.network"]))
	}

	// Multi-instance metrics should appear twice
	if len(byName["memcached.operations"]) != 2 {
		t.Errorf("memcached.operations: got %d points, want 2 (hit+miss)", len(byName["memcached.operations"]))
	}
	if len(byName["memcached.commands"]) != 3 {
		t.Errorf("memcached.commands: got %d points, want 3 (get+set+flush)", len(byName["memcached.commands"]))
	}
	if len(byName["memcached.cpu.usage"]) != 2 {
		t.Errorf("memcached.cpu.usage: got %d points, want 2 (user+system)", len(byName["memcached.cpu.usage"]))
	}
}

func TestCollect_UnreachableServer_EmitsUpZero(t *testing.T) {
	l := newTestLogger(t)
	probe, err := NewMemcachedProbe(map[string]interface{}{
		"host":    "127.0.0.1",
		"port":    19999, // nothing listening
		"timeout": 1,
	}, l)
	if err != nil {
		t.Fatalf("NewMemcachedProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() must never return an error: %v", err)
	}

	found := false
	for _, dp := range points {
		if dp.Name == "senhub.memcached.up" {
			found = true
			if dp.Value != 0 {
				t.Errorf("senhub.memcached.up = %v, want 0 (unreachable)", dp.Value)
			}
		}
	}
	if !found {
		t.Error("senhub.memcached.up not emitted when server is unreachable")
	}

	// No other metrics should be emitted when unreachable
	if len(points) != 1 {
		t.Errorf("got %d points on error, want exactly 1 (up=0 only)", len(points))
	}
}

func TestParseInt(t *testing.T) {
	stats := map[string]string{
		"good":  "42",
		"zero":  "0",
		"bad":   "notanumber",
		"float": "1.5",
		"neg":   "-1",
	}
	tests := []struct {
		key    string
		want   int64
		wantOK bool
	}{
		{"good", 42, true},
		{"zero", 0, true},
		{"bad", 0, false},
		{"float", 0, false},
		{"neg", -1, true},
		{"missing", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := parseInt(stats, tt.key)
			if ok != tt.wantOK {
				t.Errorf("parseInt(%q) ok=%v, want %v", tt.key, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("parseInt(%q) = %d, want %d", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	stats := map[string]string{
		"good": "1.234",
		"zero": "0",
		"bad":  "notanumber",
		"int":  "42",
	}
	tests := []struct {
		key    string
		wantOK bool
	}{
		{"good", true},
		{"zero", true},
		{"bad", false},
		{"int", true},
		{"missing", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, ok := parseFloat(stats, tt.key)
			if ok != tt.wantOK {
				t.Errorf("parseFloat(%q) ok=%v, want %v", tt.key, ok, tt.wantOK)
			}
		})
	}
}
