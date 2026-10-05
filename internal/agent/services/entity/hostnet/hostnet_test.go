package hostnet

import (
	"encoding/binary"
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

// A kernel row whose destination carries host bits beyond the mask must still
// mint the canonical route.destination (host bits zeroed) — the identity rule
// shared with the SNMP-side routes.
func TestParseProcRoute_CanonicalDestination(t *testing.T) {
	// dest 10.0.1.5 (LE 0501000A), gw 10.0.1.1 (LE 0101000A), mask /24.
	sample := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\n" +
		"eth0\t0501000A\t0101000A\t0003\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"
	routes := parseProcRoute([]byte(sample))
	if len(routes) != 1 {
		t.Fatalf("routes = %+v, want 1", routes)
	}
	if routes[0].Destination != "10.0.1.0/24" {
		t.Errorf("destination = %q, want 10.0.1.0/24 (host bits zeroed)", routes[0].Destination)
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
	if route.ID[idKeyNextHopIP] != "192.168.1.1" || route.Attributes[attrMetric] != int64(100) {
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
		hostID:     func() string { return "h1" },
		readRoutes: func() ([]hostRoute, error) { return parseProcRoute([]byte(routeSample)), nil },
	}
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("successful read must report ok")
	}
	// network.route + network.address ; has_route + next_hop_via.
	if len(obs.Entities) != 2 || len(obs.Relations) != 2 {
		t.Fatalf("observe = %+v", obs)
	}
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.ID[idKeyRouteDestination] != "0.0.0.0/0" ||
		route.ID[idKeyNextHopIP] != "192.168.1.1" {
		t.Errorf("unexpected route entity: %+v", route)
	}
	if addr, ok := entityOfType(obs.Entities, entityTypeNetworkAddress); !ok ||
		addr.ID[idKeyNetworkAddress] != "192.168.1.1" {
		t.Errorf("unexpected address entity: %+v", addr)
	}
}

func nextHopViaTargets(obs entity.Observation) map[string]bool {
	got := map[string]bool{}
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia {
			got[r.ToID[idKeyNetworkAddress].(string)] = true
		}
	}
	return got
}

// next_hop_via needs both rules: the gateway address is not in the exclusion
// list AND the route does not leave by a container bridge. In doubt, no edge.
func TestBuildObservation_NextHopViaNeedsAddressAndInterfaceToAgree(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Iface: "eth0"},
		{Destination: "10.2.0.0/16", NextHop: "172.18.0.1", Iface: "br-02338442b035"},
		{Destination: "10.3.0.0/16", NextHop: "172.17.0.1", Iface: "eth0"},
		{Destination: "10.4.0.0/16", NextHop: "169.254.1.1", Iface: "eth0"},
		{Destination: "10.5.0.0/16", NextHop: "127.0.0.2", Iface: "eth0"},
		{Destination: "10.6.0.0/16", NextHop: "10.10.0.9", Iface: "veth3a9f1c2"},
	})
	got := nextHopViaTargets(obs)
	if len(got) != 1 || !got["10.10.0.1"] {
		t.Errorf("only the routed gateway keeps its edge, got %v", got)
	}
}

// A k3s node as /proc/net/route shows it: the default route through the
// physical NIC, flannel pod routes toward the other nodes, the node's own pod
// network on cni0 (direct, no gateway) and a Calico-style 169.254.1.1 default.
const k3sNodeRoutes = `Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask	MTU	Window	IRTT
ens18	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
ens18	0001A8C0	00000000	0001	0	0	100	00FFFFFF	0	0	0
cni0	00002A0A	00000000	0001	0	0	0	00FFFFFF	0	0	0
flannel.1	00012A0A	00012A0A	0003	0	0	0	00FFFFFF	0	0	0
flannel.1	00022A0A	00022A0A	0003	0	0	0	00FFFFFF	0	0	0
eth0	00000000	0101FEA9	0003	0	0	200	00000000	0	0	0
`

// The node's default route through its physical NIC was already given its
// next_hop_via before the interface rule was touched: the edges that were
// missing on a Kubernetes node are the pod and bridge routes, and their absence
// is correct.
func TestBuildObservation_K3sNodeKeepsItsDefaultRouteEdge(t *testing.T) {
	routes := parseProcRoute([]byte(k3sNodeRoutes))
	obs := buildObservation("node-1", routes)

	got := nextHopViaTargets(obs)
	if len(got) != 1 || !got["192.168.1.1"] {
		t.Errorf("want next_hop_via to the LAN gateway only, got %v", got)
	}
	var def bool
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia && r.FromID[idKeyRouteDestination] == "0.0.0.0/0" &&
			r.FromID[idKeyNextHopIP] == "192.168.1.1" {
			def = true
		}
	}
	if !def {
		t.Error("the default route through ens18 has no next_hop_via")
	}
	// The flannel pod routes and the 169.254.1.1 default are routes too, with
	// their next hop as an attribute of the identity but no shared node.
	routesEmitted := 0
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkRoute {
			routesEmitted++
		}
	}
	if routesEmitted != 4 {
		t.Errorf("routes emitted = %d, want 4 (default, 2 flannel, calico default)", routesEmitted)
	}
}

// A Windows table as GetIpForwardTable lists it: the default route and a LAN
// route through "Ethernet", plus routes through the Hyper-V virtual switches.
func TestBuildObservation_WindowsDefaultRouteKeepsItsEdge(t *testing.T) {
	obs := buildObservation("win-1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Metric: 25, Iface: "Ethernet"},
		{Destination: "10.20.0.0/16", NextHop: "10.10.0.254", Metric: 281, Iface: "Ethernet"},
		{Destination: "172.28.0.0/20", NextHop: "172.28.0.1", Metric: 5256, Iface: "vEthernet (WSL)"},
		{Destination: "172.29.0.0/20", NextHop: "172.29.0.1", Metric: 5256, Iface: "vEthernet (Default Switch)"},
	})
	got := nextHopViaTargets(obs)
	if !got["10.10.0.1"] || !got["10.10.0.254"] {
		t.Errorf("routes through Ethernet lose their edge: %v", got)
	}
	// vEthernet adapters are not container-bridge names by prefix, so their
	// routes keep an edge to a routable gateway (172.17/16 stays excluded by
	// address): the filter acts on Linux bridge names only.
	if !got["172.28.0.1"] || !got["172.29.0.1"] {
		t.Errorf("vEthernet gateways: %v", got)
	}
}

// Windows' routing table, as GetIpForwardTable returns it.
func TestParseForwardTable(t *testing.T) {
	row := func(dst, mask, nh [4]byte, ifIndex, typ, metric uint32) []byte {
		b := make([]byte, forwardRowSize)
		copy(b[offDest:], dst[:])
		copy(b[offMask:], mask[:])
		copy(b[offNextHop:], nh[:])
		binary.LittleEndian.PutUint32(b[offIfIndex:], ifIndex)
		binary.LittleEndian.PutUint32(b[offType:], typ)
		binary.LittleEndian.PutUint32(b[offMetric1:], metric)
		return b
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 3)
	buf = append(buf, row([4]byte{0, 0, 0, 0}, [4]byte{0, 0, 0, 0}, [4]byte{10, 10, 0, 1}, 7, routeIndirect, 25)...)
	// direct route: its "next hop" is the host's own address
	buf = append(buf, row([4]byte{10, 10, 0, 0}, [4]byte{255, 255, 255, 0}, [4]byte{10, 10, 0, 60}, 7, 3, 281)...)
	// exact repeat of the first row
	buf = append(buf, row([4]byte{0, 0, 0, 0}, [4]byte{0, 0, 0, 0}, [4]byte{10, 10, 0, 1}, 7, routeIndirect, 25)...)

	got := parseForwardTable(buf, func(i int) string { return "Ethernet" })
	if len(got) != 1 {
		t.Fatalf("want the default route only, got %+v", got)
	}
	if got[0].Destination != "0.0.0.0/0" || got[0].NextHop != "10.10.0.1" || got[0].Metric != 25 || got[0].Iface != "Ethernet" {
		t.Errorf("route = %+v", got[0])
	}
	if parseForwardTable([]byte{1, 0}, nil) != nil {
		t.Error("a truncated buffer yields no route")
	}
}

// Two default routes (two NICs, a VPN) are two routes: the next hop is
// part of the identity (IP-FORWARD-MIB), so the inventory keeps both
// instead of a sample of one.
func TestParseForwardTable_TwoGatewaysAreTwoRoutes(t *testing.T) {
	mk := func(nh byte, ifIndex, metric uint32) []byte {
		b := make([]byte, forwardRowSize)
		copy(b[offNextHop:], []byte{10, 0, 0, nh})
		binary.LittleEndian.PutUint32(b[offIfIndex:], ifIndex)
		binary.LittleEndian.PutUint32(b[offType:], routeIndirect)
		binary.LittleEndian.PutUint32(b[offMetric1:], metric)
		return b
	}
	buf := []byte{3, 0, 0, 0}
	buf = append(append(append(buf, mk(2, 9, 50)...), mk(1, 7, 25)...), mk(1, 7, 25)...)
	got := parseForwardTable(buf, nil)
	if len(got) != 2 {
		t.Fatalf("want two default routes (the exact repeat dropped), got %+v", got)
	}
	obs := buildObservation("h1", got)
	ids := map[string]bool{}
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkRoute {
			ids[e.ID[idKeyNextHopIP].(string)] = true
		}
	}
	if !ids["10.0.0.1"] || !ids["10.0.0.2"] {
		t.Errorf("route identities = %v", ids)
	}
}

func TestBuildObservation_RouteCarriesItsEgressInterface(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Iface: "Ethernet"}})
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.Attributes[attrEgressInterface] != "Ethernet" {
		t.Errorf("route attributes = %+v", route.Attributes)
	}
}
