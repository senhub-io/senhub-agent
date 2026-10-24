package icmpcheck

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestICMPCheckProbe_GetTargetStrategies_ReturnsFixedStrategyList(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"host.example"}})

	got := p.GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetTargetStrategies() = %v, want %v", got, want)
	}
}

func TestICMPCheckProbe_ShouldStart_AlwaysReturnsTrue(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"host.example"}})

	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
}

func TestICMPCheckProbe_GetInterval_ReturnsConfiguredInterval(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets":  []interface{}{"host.example"},
		"interval": 33,
	})

	if got, want := p.GetInterval(), 33*time.Second; got != want {
		t.Errorf("GetInterval() = %v, want %v", got, want)
	}
}

func TestICMPCheckProbe_OnStart_ReturnsNil(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"host.example"}})

	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart() error = %v, want nil", err)
	}
}

func TestICMPCheckProbe_OnShutdown_ReturnsNil(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"host.example"}})

	if err := p.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown() error = %v, want nil", err)
	}
}

func TestPingOnce_ResolveError_ReportsLossAndNoPackets(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets":    []interface{}{"localhost:0"},
		"count":      1,
		"timeout":    1,
		"privileged": false,
	})

	// host:port is invalid for a ping target; NewPinger rejects it before any socket work.
	res := p.pingOnce(context.Background(), "localhost:0")

	if res.err == nil {
		t.Fatal("pingOnce() error = nil, want a resolving error")
	}
	if !strings.Contains(res.err.Error(), "localhost:0") {
		t.Errorf("pingOnce() error = %q, want it to contain target", res.err)
	}
	if res.lossRatio != 1 {
		t.Errorf("pingOnce() lossRatio = %v, want 1 on resolve error", res.lossRatio)
	}
	if res.sent != 0 || res.received != 0 {
		t.Errorf("pingOnce() sent/received = %d/%d, want 0/0 on resolve error", res.sent, res.received)
	}
	if res.resolvedIP != "" {
		t.Errorf("pingOnce() resolvedIP = %q, want empty on resolve error", res.resolvedIP)
	}
}

func TestCollect_Seam_PartialLossAndUnresolvedHost(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{"unresolved.example", "partial.example"},
	})

	p.ping = func(_ context.Context, target string) pingResult {
		switch target {
		case "unresolved.example":
			return pingResult{
				target: target, sent: 0, received: 0, lossRatio: 1,
				err: errors.New("lookup unresolved.example: no such host"),
			}
		case "partial.example":
			return pingResult{
				target: target, resolvedIP: "192.0.2.25",
				sent: 4, received: 3, lossRatio: 0.25,
				minRTT: 9 * time.Millisecond, avgRTT: 12 * time.Millisecond,
				maxRTT: 18 * time.Millisecond, stddevRTT: 4 * time.Millisecond,
			}
		default:
			t.Fatalf("unexpected ping target %q", target)
			return pingResult{}
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	vals := map[string]float64{}
	for _, dp := range points {
		var target string
		for _, tg := range dp.Tags {
			if tg.Key == "target" {
				target = tg.Value
			}
		}
		if target == "" {
			t.Fatalf("datapoint %s missing target tag", dp.Name)
		}
		vals[target+"\x00"+dp.Name] = dp.Value
	}

	if got := vals["unresolved.example\x00senhub.icmp.up"]; got != 0 {
		t.Errorf("unresolved up = %v, want 0", got)
	}
	if got := vals["unresolved.example\x00senhub.icmp.packet_loss"]; got != 100 {
		t.Errorf("unresolved packet_loss = %v, want 100", got)
	}
	if got := vals["unresolved.example\x00senhub.icmp.packets.sent"]; got != 0 {
		t.Errorf("unresolved packets.sent = %v, want 0", got)
	}
	if _, ok := vals["unresolved.example\x00senhub.icmp.rtt.avg"]; ok {
		t.Error("unresolved target must not emit RTT metrics")
	}

	if got := vals["partial.example\x00senhub.icmp.up"]; got != 1 {
		t.Errorf("partial up = %v, want 1", got)
	}
	if got := vals["partial.example\x00senhub.icmp.packet_loss"]; got != 25 {
		t.Errorf("partial packet_loss = %v, want 25", got)
	}
	if got := vals["partial.example\x00senhub.icmp.packets.sent"]; got != 4 {
		t.Errorf("partial packets.sent = %v, want 4", got)
	}
	if got := vals["partial.example\x00senhub.icmp.packets.received"]; got != 3 {
		t.Errorf("partial packets.received = %v, want 3", got)
	}
	if got := vals["partial.example\x00senhub.icmp.rtt.min"]; got != 9 {
		t.Errorf("partial rtt.min = %v ms, want 9", got)
	}
	if got := vals["partial.example\x00senhub.icmp.rtt.avg"]; got != 12 {
		t.Errorf("partial rtt.avg = %v ms, want 12", got)
	}
	if got := vals["partial.example\x00senhub.icmp.rtt.max"]; got != 18 {
		t.Errorf("partial rtt.max = %v ms, want 18", got)
	}
	if got := vals["partial.example\x00senhub.icmp.rtt.stddev"]; got != 4 {
		t.Errorf("partial rtt.stddev = %v ms, want 4", got)
	}
}
