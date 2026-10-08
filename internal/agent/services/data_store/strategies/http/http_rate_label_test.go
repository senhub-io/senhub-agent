package http

import "testing"

// The channels of a customer PRTG migration (0.1.x labels): a rate whose
// OTel unit is a bare "1/s" still shows what it counts.
func TestPRTGCustomRateUnitsKeepTheirNoun(t *testing.T) {
	converter, defs := newSemanticsHarness(t)

	golden := []struct {
		probe  string
		metric string
		want   string
	}{
		{"netscaler", "netscaler.system.http.requests.rate", "req/s"},
		{"netscaler", "netscaler.system.http.responses.rate", "resp/s"},
		{"netscaler", "netscaler.ssl.transactions.rate", "tx/s"},
		{"netscaler", "netscaler.lbvserver.requests.rate", "req/s"},
		{"netscaler", "netscaler.system.network.rx.packets_per_sec", "pkt/s"},
	}
	for _, g := range golden {
		t.Run(g.metric, func(t *testing.T) {
			displayUnit, found := "", false
			for _, m := range defs[g.probe] {
				if m.Name == g.metric {
					displayUnit, found = m.Unit, true
				}
			}
			if !found {
				t.Fatalf("%s/%s is not in the embedded definitions", g.probe, g.metric)
			}
			channel := buildChannelFor(converter, g.probe, g.metric, displayUnit)
			if channel == nil {
				t.Fatal("nil channel")
			}
			if channel.Unit != "Custom" || channel.CustomUnit != g.want {
				t.Errorf("unit %q custom %q, want Custom %q", channel.Unit, channel.CustomUnit, g.want)
			}
		})
	}
}

func TestCustomRateUnitPicksTheNoun(t *testing.T) {
	cases := []struct {
		otel, display, want string
	}{
		{"{request}/s", "", "req/s"},
		{"{response}/s", "", "resp/s"},
		{"{packet}/s", "", "pkt/s"},
		{"{transaction}/s", "", "tx/s"},
		{"{request}/s", "something/s", "req/s"},
		{"1/s", "req/s", "req/s"},
		{"1/s", "pps", "pkt/s"},
		{"1/s", "", "/s"},
		{"1/s", "#", "/s"},
		{"1/s", "/s", "/s"},
	}
	for _, c := range cases {
		if got := customRateUnit(c.otel, c.display); got != c.want {
			t.Errorf("customRateUnit(%q, %q) = %q, want %q", c.otel, c.display, got, c.want)
		}
	}
}
