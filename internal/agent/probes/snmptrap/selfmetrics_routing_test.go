package snmptrap

import "testing"

// Regression for #701: snmp_trap emits self-metrics from Collect()
// (rejected_community, decode_panics) but GetTargetStrategies() used to
// return []string{}, so those datapoints were routed to no strategy at all
// — dropped before ever reaching OTLP (or any sink). The probe must now
// inherit the BaseProbe default and route its metrics like every other
// probe.
func TestSNMPTrap_SelfMetricsRouteToOTLP(t *testing.T) {
	p := newTestTrapProbe(t)

	targets := p.GetTargetStrategies()
	if len(targets) == 0 {
		t.Fatal("GetTargetStrategies is empty — self-metrics would be dropped (#701 regression)")
	}
	has := func(s string) bool {
		for _, x := range targets {
			if x == s {
				return true
			}
		}
		return false
	}
	if !has("otlp") {
		t.Errorf("targets %v must include otlp so self-metrics reach the OTLP sink", targets)
	}

	// The metrics that now route must actually be emitted by Collect().
	pts, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("Collect emitted no self-metrics to route")
	}
}
