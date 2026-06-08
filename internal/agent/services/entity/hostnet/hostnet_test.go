package hostnet

import "testing"

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

	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %+v, want 1 network.route", obs.Entities)
	}
	e := obs.Entities[0]
	if e.Type != entityTypeNetworkRoute ||
		e.ID[idKeyHost] != "h1" || e.ID[idKeyRouteDestination] != "0.0.0.0/0" {
		t.Errorf("route entity id wrong: %+v", e)
	}
	if e.Attributes[attrNextHopIP] != "192.168.1.1" || e.Attributes[attrMetric] != int64(100) {
		t.Errorf("route attrs wrong: %+v", e.Attributes)
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("relations = %d, want 1 has_route", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != relHasRoute ||
		r.FromType != entityTypeHost || r.FromID[idKeyHost] != "h1" ||
		r.ToType != entityTypeNetworkRoute || r.ToID[idKeyRouteDestination] != "0.0.0.0/0" {
		t.Errorf("has_route relation wrong: %+v", r)
	}
	// Edge carries no attributes — the embedded descriptor is bare.
	if len(r.Attributes) != 0 {
		t.Errorf("has_route should carry no edge attributes, got %v", r.Attributes)
	}
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
	if len(obs.Entities) != 1 || len(obs.Relations) != 1 {
		t.Fatalf("observe = %+v", obs)
	}
	if obs.Entities[0].ID[idKeyRouteDestination] != "0.0.0.0/0" ||
		obs.Entities[0].Attributes[attrNextHopIP] != "192.168.1.1" {
		t.Errorf("unexpected route entity: %+v", obs.Entities[0])
	}
}
