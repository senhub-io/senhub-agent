package tcpdial

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbe(t *testing.T, targets ...string) *TCPDialProbe {
	t.Helper()
	raw := make([]interface{}, len(targets))
	for i, tg := range targets {
		raw[i] = tg
	}
	probe, err := NewTCPDialProbe(map[string]interface{}{"targets": raw, "timeout": 2},
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewTCPDialProbe: %v", err)
	}
	p := probe.(*TCPDialProbe)
	p.SetName("dial-edge")
	return p
}

func TestParseConfig_Errors(t *testing.T) {
	logger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	cases := map[string]map[string]interface{}{
		"missing targets": {},
		"not host:port":   {"targets": []interface{}{"no-port-here"}},
		"empty list":      {"targets": []interface{}{}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewTCPDialProbe(config, logger); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

// TestCollect_RealListener dials a real local listener (up=1 +
// duration) and a closed port (up=0, no duration, no error).
func TestCollect_RealListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	p := newTestProbe(t, ln.Addr().String(), "127.0.0.1:1")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byTarget := map[string]map[string]float32{}
	for _, dp := range points {
		var target string
		for _, tg := range dp.Tags {
			if tg.Key == "target" {
				target = tg.Value
			}
		}
		if byTarget[target] == nil {
			byTarget[target] = map[string]float32{}
		}
		byTarget[target][dp.Name] = dp.Value
	}

	open := byTarget[ln.Addr().String()]
	if open["senhub.tcpdial.up"] != 1 {
		t.Errorf("open port up = %v, want 1", open["senhub.tcpdial.up"])
	}
	if _, ok := open["senhub.tcpdial.duration"]; !ok {
		t.Error("open port missing duration")
	}
	closed := byTarget["127.0.0.1:1"]
	if closed["senhub.tcpdial.up"] != 0 {
		t.Errorf("closed port up = %v, want 0", closed["senhub.tcpdial.up"])
	}
	if _, ok := closed["senhub.tcpdial.duration"]; ok {
		t.Error("closed port must not emit duration")
	}
}

func TestSeam(t *testing.T) {
	p := newTestProbe(t, "a:1", "b:2")
	var calls atomic.Int32
	p.dial = func(target string) dialResult {
		calls.Add(1)
		return dialResult{target: target, up: true, duration: 3 * time.Millisecond}
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("dial called %d times, want 2", calls.Load())
	}
}
