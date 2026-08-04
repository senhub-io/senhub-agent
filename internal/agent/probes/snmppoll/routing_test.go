package snmppoll

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
)

// errWalkUnsupported stands in for the error a device returns when it does
// not implement a routing table at all.
var errWalkUnsupported = errors.New("no such object")

// routeWalkClient answers WalkRaw per base OID, so a device implementing
// only one of the two routing tables can be reproduced. The shared
// fakeClient carries a single walkRawErr for every base and cannot.
type routeWalkClient struct {
	responses map[string][]snmpRawBind
	failures  map[string]error
}

func (c *routeWalkClient) Connect() error                         { return nil }
func (c *routeWalkClient) Get([]string) ([]snmpVarBind, error)    { return nil, nil }
func (c *routeWalkClient) BulkWalk(string) ([]snmpVarBind, error) { return nil, nil }
func (c *routeWalkClient) Close() error                           { return nil }
func (c *routeWalkClient) WalkRaw(base string) ([]snmpRawBind, error) {
	if err := c.failures[base]; err != nil {
		return nil, err
	}
	return c.responses[base], nil
}

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
	if r.Destination != "10.0.0.0/24" {
		t.Errorf("destination = %q, want 10.0.0.0/24 (parsed from the entry index)", r.Destination)
	}
}

func TestRouteDestFromIndex(t *testing.T) {
	cases := map[string]string{
		// dest(4).mask(4).tos(1).nextHop(4)
		"10.0.0.0.255.255.255.0.0.10.0.0.254": "10.0.0.0/24",
		"0.0.0.0.0.0.0.0.0.10.0.0.1":          "0.0.0.0/0", // default route
		"10.20.0.0.255.255.0.0.0.10.0.0.254":  "10.20.0.0/16",
		// canonical identity: host bits reported set are zeroed, /32 explicit
		"10.20.3.7.255.255.255.0.0.10.0.0.254": "10.20.3.0/24",
		"192.0.2.1.255.255.255.255.0.10.0.0.1": "192.0.2.1/32",
		"10.0.0.0.255.0.255.0.0.10.0.0.1":      "", // non-canonical mask → reject
		"10.0.0.0.255.255.255.0":               "", // short index → reject
	}
	for idx, want := range cases {
		if got := routeDestFromIndex(idx); got != want {
			t.Errorf("routeDestFromIndex(%q) = %q, want %q", idx, got, want)
		}
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

// ipv6IndexOctets renders an IPv6 address as the 16 decimal sub-identifiers
// an inetCidrRouteTable index carries it as.
func ipv6IndexOctets(t *testing.T, addr string) string {
	t.Helper()
	ip := net.ParseIP(addr)
	if ip == nil {
		t.Fatalf("bad fixture address %q", addr)
	}
	ip = ip.To16()
	parts := make([]string, 16)
	for i, b := range ip {
		parts[i] = strconv.Itoa(int(b))
	}
	return strings.Join(parts, ".")
}

// inetIndex builds an inetCidrRouteTable row key:
// destType.destLen.dest[…].pfxLen.policyLen.policy[…].nextHopType.nextHopLen.nextHop[…]
func inetIndex(destType int, dest string, destLen, pfxLen int) string {
	return fmt.Sprintf("%d.%d.%s.%d.1.0.%d.%d.%s",
		destType, destLen, dest, pfxLen, destType, destLen, dest)
}

func TestInetRouteDestFromIndex_IPv6(t *testing.T) {
	octets := ipv6IndexOctets(t, "2001:db8:abcd::")
	got := inetRouteDestFromIndex(inetIndex(inetAddrTypeIPv6, octets, 16, 48))
	if got != "2001:db8:abcd::/48" {
		t.Fatalf("destination = %q, want 2001:db8:abcd::/48", got)
	}
}

func TestInetRouteDestFromIndex_IPv6DefaultRoute(t *testing.T) {
	octets := ipv6IndexOctets(t, "::")
	if got := inetRouteDestFromIndex(inetIndex(inetAddrTypeIPv6, octets, 16, 0)); got != "::/0" {
		t.Fatalf("destination = %q, want ::/0", got)
	}
}

// Host bits a device reports set are zeroed, and the result is RFC 5952
// (lowercase, longest run of zeros compressed) — the same canonical
// identity contract the IPv4 rows carry.
func TestInetRouteDestFromIndex_IPv6HostBitsZeroed(t *testing.T) {
	octets := ipv6IndexOctets(t, "2001:0DB8:0000:0001:dead:beef:0:1")
	got := inetRouteDestFromIndex(inetIndex(inetAddrTypeIPv6, octets, 16, 64))
	if got != "2001:db8:0:1::/64" {
		t.Fatalf("destination = %q, want 2001:db8:0:1::/64", got)
	}
}

// The same table carries IPv4 routes on a dual-stack device.
func TestInetRouteDestFromIndex_IPv4(t *testing.T) {
	if got := inetRouteDestFromIndex(inetIndex(inetAddrTypeIPv4, "10.20.3.7", 4, 24)); got != "10.20.3.0/24" {
		t.Fatalf("destination = %q, want 10.20.3.0/24", got)
	}
}

func TestInetRouteDestFromIndex_Rejects(t *testing.T) {
	v6 := ipv6IndexOctets(t, "2001:db8::")
	cases := map[string]string{
		"empty":                    "",
		"truncated before pfxLen":  "2.16." + v6,
		"length disagrees with v6": inetIndex(inetAddrTypeIPv6, "10.0.0.0", 4, 24), // type ipv6, 4 octets
		"length disagrees with v4": inetIndex(inetAddrTypeIPv4, v6, 16, 24),        // type ipv4, 16 octets
		// ipv4z/ipv6z carry a zone index; the destination is only meaningful
		// inside that scope and would collide with its unzoned twin.
		"zoned ipv6 (ipv6z)":      inetIndex(4, v6, 16, 64),
		"prefix longer than addr": inetIndex(inetAddrTypeIPv6, v6, 16, 200),
		"zero length":             "2.0.64.1.0.2.0",
	}
	for name, idx := range cases {
		t.Run(name, func(t *testing.T) {
			if got := inetRouteDestFromIndex(idx); got != "" {
				t.Fatalf("inetRouteDestFromIndex(%q) = %q, want \"\"", idx, got)
			}
		})
	}
}

// The inet table places next hop / ifIndex / type / metric at different
// sub-identifiers than the IPv4 table; a row must decode through its own
// spec.
func TestParseRouteTable_InetColumns(t *testing.T) {
	rk := inetIndex(inetAddrTypeIPv6, ipv6IndexOctets(t, "2001:db8:1::"), 16, 48)
	b := func(col string, v any) snmpRawBind {
		return snmpRawBind{OID: inetCidrRouteEntry + "." + col + "." + rk, Value: v}
	}
	rows := parseRouteTable(inetCidrRouteSpec, []snmpRawBind{
		b(colInetRouteNextHop, "fe80::1"),
		b(colInetRouteType, routeTypeRemote),
		b(colInetRouteIfIndex, 3),
		b(colInetRouteMetric1, 20),
	})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Destination != "2001:db8:1::/48" {
		t.Errorf("destination = %q, want 2001:db8:1::/48", r.Destination)
	}
	if r.NextHop != "fe80::1" || r.Type != routeTypeRemote || r.IfIndex != "3" || r.Metric != 20 {
		t.Errorf("row = %+v", r)
	}
}

// A device implementing only one of the two tables is the norm, not a
// failure: routes from the table that answered must still be collected.
func TestCollectRoutes_OneTableMissing(t *testing.T) {
	v6rk := inetIndex(inetAddrTypeIPv6, ipv6IndexOctets(t, "2001:db8:2::"), 16, 48)

	client := &routeWalkClient{
		responses: map[string][]snmpRawBind{
			inetCidrRouteEntry: {
				{OID: inetCidrRouteEntry + "." + colInetRouteNextHop + "." + v6rk, Value: "fe80::1"},
				{OID: inetCidrRouteEntry + "." + colInetRouteType + "." + v6rk, Value: routeTypeRemote},
			},
		},
		failures: map[string]error{ipCidrRouteEntry: errWalkUnsupported},
	}

	rows, err := collectRoutes(client)
	if err != nil {
		t.Fatalf("collectRoutes() error = %v, want the IPv6 rows despite the IPv4 table being absent", err)
	}
	if len(rows) != 1 || rows[0].Destination != "2001:db8:2::/48" {
		t.Fatalf("rows = %+v, want the single IPv6 route", rows)
	}
}

// Only when BOTH tables fail is there nothing to report — and the error
// must name both causes rather than hiding one.
func TestCollectRoutes_BothTablesFail(t *testing.T) {
	client := &routeWalkClient{
		failures: map[string]error{
			ipCidrRouteEntry:   errWalkUnsupported,
			inetCidrRouteEntry: errWalkUnsupported,
		},
	}

	_, err := collectRoutes(client)
	if err == nil {
		t.Fatal("collectRoutes() error = nil, want a failure when neither table answers")
	}
	for _, want := range []string{"ipCidrRoute", "inetCidrRoute"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}
