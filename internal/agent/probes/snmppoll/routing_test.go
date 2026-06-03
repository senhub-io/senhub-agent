package snmppoll

import "testing"

func TestParseRoutes(t *testing.T) {
	// Entry index = dest(4).mask(4).tos(1).nextHop(4).
	rk := "10.0.0.0.255.255.255.0.0.10.0.0.254"
	b := func(col string, v any) snmpRawBind {
		return snmpRawBind{OID: ipCidrRouteEntry + "." + col + "." + rk, Value: v}
	}
	rows := parseRoutes([]snmpRawBind{
		b(colRouteNextHop, "10.0.0.254"), // gosnmp IpAddress → string
		b(colRouteType, 4),
		b(colRouteIfIndex, 2),
		b(colRouteMetric1, 1),
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r.NextHop != "10.0.0.254" || r.Type != 4 || r.IfIndex != "2" || r.Metric != 1 {
		t.Errorf("row = %+v", r)
	}
}

func TestUsableNextHop(t *testing.T) {
	cases := []struct {
		nextHop, self string
		want          bool
	}{
		{"10.0.0.254", "10.0.0.1", true},
		{"0.0.0.0", "10.0.0.1", false},   // unspecified
		{"127.0.0.1", "10.0.0.1", false}, // loopback
		{"10.0.0.1", "10.0.0.1", false},  // == self mgmt
		{"notanip", "10.0.0.1", false},   // not parseable
	}
	for _, c := range cases {
		if got := usableNextHop(c.nextHop, c.self); got != c.want {
			t.Errorf("usableNextHop(%q,%q) = %v, want %v", c.nextHop, c.self, got, c.want)
		}
	}
}

func TestAsIPString(t *testing.T) {
	if got := asIPString("1.2.3.4"); got != "1.2.3.4" {
		t.Errorf("string = %q", got)
	}
	if got := asIPString([]byte{10, 0, 0, 1}); got != "10.0.0.1" {
		t.Errorf("4-byte = %q", got)
	}
	if got := asIPString(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}
