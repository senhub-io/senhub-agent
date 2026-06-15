package icmpcheck

import (
	"errors"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *ICMPCheckProbe {
	t.Helper()
	probe, err := NewICMPCheckProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewICMPCheckProbe: %v", err)
	}
	p, ok := probe.(*ICMPCheckProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("ping-edge")
	return p
}

func TestParseConfig(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]interface{}
		wantErr string
		check   func(t *testing.T, cfg checkConfig)
	}{
		{
			name:    "missing targets",
			config:  map[string]interface{}{},
			wantErr: "targets",
		},
		{
			name:    "empty targets",
			config:  map[string]interface{}{"targets": []interface{}{}},
			wantErr: "at least one",
		},
		{
			name:    "non-string target",
			config:  map[string]interface{}{"targets": []interface{}{42}},
			wantErr: "non-empty strings",
		},
		{
			name:   "defaults",
			config: map[string]interface{}{"targets": []interface{}{"gw.example"}},
			check: func(t *testing.T, cfg checkConfig) {
				if cfg.Count != defaultCount || cfg.Timeout != defaultTimeout || cfg.Interval != defaultInterval {
					t.Errorf("defaults not applied: %+v", cfg)
				}
			},
		},
		{
			name: "full overrides",
			config: map[string]interface{}{
				"targets":     []interface{}{"a", "b"},
				"count":       2,
				"timeout":     3,
				"interval":    30,
				"privileged":  true,
				"packet_size": 120,
			},
			check: func(t *testing.T, cfg checkConfig) {
				if len(cfg.Targets) != 2 || cfg.Count != 2 || cfg.Timeout != 3*time.Second ||
					cfg.Interval != 30*time.Second || !cfg.Privileged || cfg.PacketSize != 120 {
					t.Errorf("overrides not applied: %+v", cfg)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := parseConfig(c.config)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.check != nil {
				c.check(t, cfg)
			}
		})
	}
}

// TestCollect_BuildsExpectedMetricSet drives Collect through the ping
// seam: a healthy target yields the full metric set with the target
// tag, an unreachable one yields up=0/loss=100 and no RTT points, and
// per-target outcomes never become a collection error.
func TestCollect_BuildsExpectedMetricSet(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"ok.example", "down.example"}})
	p.ping = func(target string) pingResult {
		if target == "ok.example" {
			return pingResult{
				target: target, resolvedIP: "192.0.2.10",
				sent: 4, received: 4, lossRatio: 0,
				minRTT: 10 * time.Millisecond, avgRTT: 12 * time.Millisecond,
				maxRTT: 15 * time.Millisecond, stddevRTT: 2 * time.Millisecond,
			}
		}
		return pingResult{target: target, sent: 4, received: 0, lossRatio: 1, err: errors.New("timeout")}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byTarget := map[string]map[string]float32{}
	for _, dp := range points {
		var target string
		hasProbeTags := false
		for _, tg := range dp.Tags {
			if tg.Key == "target" {
				target = tg.Value
			}
			if tg.Key == "probe_type" && tg.Value == ProbeType {
				hasProbeTags = true
			}
		}
		if !hasProbeTags {
			t.Fatalf("datapoint %s missing probe_type enrichment", dp.Name)
		}
		if byTarget[target] == nil {
			byTarget[target] = map[string]float32{}
		}
		byTarget[target][dp.Name] = dp.Value
	}

	ok := byTarget["ok.example"]
	if ok["senhub.icmp.up"] != 1 {
		t.Errorf("ok target up = %v, want 1", ok["senhub.icmp.up"])
	}
	if ok["senhub.icmp.rtt.avg"] != 12 {
		t.Errorf("ok target rtt.avg = %v ms, want 12", ok["senhub.icmp.rtt.avg"])
	}
	if ok["senhub.icmp.packet_loss"] != 0 {
		t.Errorf("ok target loss = %v, want 0", ok["senhub.icmp.packet_loss"])
	}

	down := byTarget["down.example"]
	if down["senhub.icmp.up"] != 0 {
		t.Errorf("down target up = %v, want 0", down["senhub.icmp.up"])
	}
	if down["senhub.icmp.packet_loss"] != 100 {
		t.Errorf("down target loss = %v, want 100", down["senhub.icmp.packet_loss"])
	}
	if _, hasRTT := down["senhub.icmp.rtt.avg"]; hasRTT {
		t.Error("down target must not emit RTT points")
	}
	if len(down) != 4 {
		t.Errorf("down target emits %d metrics, want 4 (up/loss/sent/received)", len(down))
	}
}

// TestCollect_RealLocalhost exercises the real pro-bing path against
// 127.0.0.1 when the environment allows unprivileged ICMP; skipped
// otherwise (CI runners and locked-down hosts).
func TestCollect_RealLocalhost(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{"127.0.0.1"},
		"count":   1,
		"timeout": 2,
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if dp.Name == "senhub.icmp.up" && dp.Value == 1 {
			return // real ping worked
		}
	}
	t.Skip("unprivileged ICMP unavailable in this environment — covered by the seam tests")
}

// TestDefaultPrivileged pins the #357 decision matrix: Windows always
// privileged; Linux privileged iff euid 0 (root cannot use datagram
// ICMP on stock Ubuntu — ping_group_range "1 0"); everything else
// unprivileged, ready for the least-privilege work (#223).
func TestDefaultPrivileged(t *testing.T) {
	cases := []struct {
		goos string
		euid int
		want bool
	}{
		{"windows", 1000, true},
		{"linux", 0, true},
		{"linux", 1000, false},
		{"darwin", 0, false},
		{"darwin", 501, false},
	}
	for _, tc := range cases {
		if got := defaultPrivileged(tc.goos, tc.euid); got != tc.want {
			t.Errorf("defaultPrivileged(%s, %d) = %v, want %v", tc.goos, tc.euid, got, tc.want)
		}
	}
}

func TestParseConfig_PrivilegedOverride(t *testing.T) {
	raw := map[string]interface{}{
		"targets":    []interface{}{"127.0.0.1"},
		"privileged": true,
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if !cfg.Privileged {
		t.Error("explicit privileged: true must override the default")
	}
	raw["privileged"] = false
	if cfg, _ := parseConfig(raw); cfg.Privileged {
		t.Error("explicit privileged: false must override the default")
	}
}
