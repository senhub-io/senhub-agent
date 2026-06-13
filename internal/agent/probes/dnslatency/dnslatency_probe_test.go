package dnslatency

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbe(t *testing.T, config map[string]interface{}) *DNSLatencyProbe {
	t.Helper()
	probe, err := NewDNSLatencyProbe(config, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewDNSLatencyProbe: %v", err)
	}
	p := probe.(*DNSLatencyProbe)
	p.SetName("dns-edge")
	return p
}

func TestParseConfig(t *testing.T) {
	logger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	if _, err := NewDNSLatencyProbe(map[string]interface{}{}, logger); err == nil {
		t.Fatal("missing names must be a configuration error")
	}
	p := newTestProbe(t, map[string]interface{}{
		"names":     []interface{}{"example.com"},
		"resolvers": []interface{}{"192.0.2.1"}, // bare IP gets :53
	})
	if p.config.Resolvers[0] != "192.0.2.1:53" {
		t.Errorf("bare resolver IP = %q, want default :53 appended", p.config.Resolvers[0])
	}
}

// TestCollect_PairFanout drives the (name x resolver) matrix through
// the seam: 2 names x 2 resolvers = 4 lookups, each series tagged with
// both discriminants; a failing pair is a measurement.
func TestCollect_PairFanout(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"names":     []interface{}{"ok.example", "broken.example"},
		"resolvers": []interface{}{"192.0.2.1:53", "192.0.2.2:53"},
	})
	p.lookup = func(name, resolver string) lookupResult {
		if name == "broken.example" {
			return lookupResult{name: name, resolver: resolver, err: errors.New("SERVFAIL")}
		}
		return lookupResult{name: name, resolver: resolver, up: true, duration: 4 * time.Millisecond, answers: 2}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	type key struct{ name, resolver, metric string }
	got := map[key]float64{}
	for _, dp := range points {
		var name, resolver string
		for _, tg := range dp.Tags {
			switch tg.Key {
			case "name":
				name = tg.Value
			case "resolver":
				resolver = tg.Value
			}
		}
		if name == "" || resolver == "" {
			t.Fatalf("datapoint %s missing name/resolver tags", dp.Name)
		}
		got[key{name, resolver, dp.Name}] = dp.Value
	}

	if got[key{"ok.example", "192.0.2.1:53", "senhub.dns.up"}] != 1 {
		t.Error("ok pair must be up")
	}
	if got[key{"ok.example", "192.0.2.2:53", "senhub.dns.answers"}] != 2 {
		t.Error("answers missing on second resolver")
	}
	if got[key{"broken.example", "192.0.2.1:53", "senhub.dns.up"}] != 0 {
		t.Error("broken pair must be up=0")
	}
	if _, ok := got[key{"broken.example", "192.0.2.1:53", "senhub.dns.lookup.duration"}]; ok {
		t.Error("failed lookup must not emit duration")
	}
}

// TestCollect_SystemResolverLocalhost exercises the real lookup path
// via the OS resolver; localhost resolves everywhere.
func TestCollect_SystemResolverLocalhost(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"names": []interface{}{"localhost"}})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if dp.Name == "senhub.dns.up" && dp.Value == 1 {
			for _, tg := range dp.Tags {
				if tg.Key == "resolver" && tg.Value == systemResolverLabel {
					return
				}
			}
		}
	}
	t.Error("localhost did not resolve via the system resolver")
}
