package hostnet

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestHexLEToIP(t *testing.T) {
	cases := map[string]string{
		"0101A8C0": "192.168.1.1", // C0.A8.01.01 little-endian
		"00000000": "0.0.0.0",
		"0A0A0A0A": "10.10.10.10",
		"bad":      "",
	}
	for in, want := range cases {
		if got := hexLEToIP(in); got != want {
			t.Errorf("hexLEToIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskHexToPrefix(t *testing.T) {
	cases := map[string]int{
		"00000000": 0,  // 0.0.0.0 → /0 (default route)
		"00FFFFFF": 24, // 255.255.255.0 → /24
		"0000FFFF": 16, // 255.255.0.0 → /16
		"00FF00FF": -1, // 255.0.255.0 → non-canonical mask → reject
		"bad":      -1,
	}
	for in, want := range cases {
		if got := maskHexToPrefix(in); got != want {
			t.Errorf("maskHexToPrefix(%q) = %d, want %d", in, got, want)
		}
	}
}

// One default route (next hop 192.168.1.1) plus a connected /24 (zero gateway,
// skipped — no next hop).
const routeSample = `Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask	MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
eth0	0001A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0
`

func TestParseProcRoute(t *testing.T) {
	routes := parseProcRoute([]byte(routeSample))
	if len(routes) != 1 {
		t.Fatalf("routes = %+v, want 1 (only the next-hop default route)", routes)
	}
	r := routes[0]
	if r.Destination != "0.0.0.0/0" || r.NextHop != "192.168.1.1" || r.Metric != 100 {
		t.Errorf("route = %+v, want {0.0.0.0/0 192.168.1.1 100}", r)
	}
}

func TestBuildObservation_HostRoute(t *testing.T) {
	routes := []hostRoute{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", Metric: 100}}
	obs := buildObservation("h1", routes)

	// network.route + the gateway network.address node.
	if len(obs.Entities) != 2 {
		t.Fatalf("entities = %+v, want network.route + network.address", obs.Entities)
	}
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.ID[idKeyHost] != "h1" || route.ID[idKeyRouteDestination] != "0.0.0.0/0" {
		t.Errorf("route entity wrong: %+v", route)
	}
	if route.Attributes[attrNextHopIP] != "192.168.1.1" || route.Attributes[attrMetric] != int64(100) {
		t.Errorf("route attrs wrong: %+v", route.Attributes)
	}
	addr, ok := entityOfType(obs.Entities, entityTypeNetworkAddress)
	if !ok || addr.ID[idKeyNetworkAddress] != "192.168.1.1" {
		t.Errorf("address entity wrong: %+v", addr)
	}

	// has_route (host→route) + next_hop_via (route→address).
	if len(obs.Relations) != 2 {
		t.Fatalf("relations = %d, want has_route + next_hop_via", len(obs.Relations))
	}
	hr, ok := relationOfType(obs.Relations, relHasRoute)
	if !ok || hr.FromType != entityTypeHost || hr.FromID[idKeyHost] != "h1" ||
		hr.ToType != entityTypeNetworkRoute || hr.ToID[idKeyRouteDestination] != "0.0.0.0/0" || len(hr.Attributes) != 0 {
		t.Errorf("has_route relation wrong: %+v", hr)
	}
	nhv, ok := relationOfType(obs.Relations, relNextHopVia)
	if !ok || nhv.FromType != entityTypeNetworkRoute || nhv.ToType != entityTypeNetworkAddress ||
		nhv.ToID[idKeyNetworkAddress] != "192.168.1.1" || len(nhv.Attributes) != 0 {
		t.Errorf("next_hop_via relation wrong: %+v", nhv)
	}
}

func entityOfType(es []entity.Entity, typ string) (entity.Entity, bool) {
	for _, e := range es {
		if e.Type == typ {
			return e, true
		}
	}
	return entity.Entity{}, false
}

func relationOfType(rs []entity.Relation, typ string) (entity.Relation, bool) {
	for _, r := range rs {
		if r.Type == typ {
			return r, true
		}
	}
	return entity.Relation{}, false
}

func TestBuildObservation_MetricOmittedWhenZero(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{{Destination: "10.0.0.0/8", NextHop: "10.0.0.1"}})
	if _, ok := obs.Entities[0].Attributes[attrMetric]; ok {
		t.Errorf("metric 0 should be omitted, got %v", obs.Entities[0].Attributes)
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []hostRoute{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1"}}); len(o.Entities) != 0 {
		t.Error("no hostID → empty")
	}
	if o := buildObservation("h1", nil); len(o.Entities) != 0 {
		t.Error("no routes → empty")
	}
}

func TestObserve_InjectedReader(t *testing.T) {
	s := &Source{
		hostID:    func() string { return "h1" },
		readRoute: func() ([]byte, error) { return []byte(routeSample), nil },
	}
	obs := s.Observe()
	// network.route + network.address ; has_route + next_hop_via.
	if len(obs.Entities) != 2 || len(obs.Relations) != 2 {
		t.Fatalf("observe = %+v", obs)
	}
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.ID[idKeyRouteDestination] != "0.0.0.0/0" ||
		route.Attributes[attrNextHopIP] != "192.168.1.1" {
		t.Errorf("unexpected route entity: %+v", route)
	}
	if addr, ok := entityOfType(obs.Entities, entityTypeNetworkAddress); !ok ||
		addr.ID[idKeyNetworkAddress] != "192.168.1.1" {
		t.Errorf("unexpected address entity: %+v", addr)
	}
}
