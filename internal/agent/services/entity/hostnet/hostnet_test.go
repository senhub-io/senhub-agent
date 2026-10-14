package hostnet

import (
	"encoding/binary"
	"fmt"
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildObservation_HostRoute(t *testing.T) {
	routes := []hostRoute{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", Metric: 100, RouteType: routeTypeRemote}}
	obs := buildObservation("h1", routes)

	if len(obs.Entities) != 3 {
		t.Fatalf("entities = %+v, want host + network.route + network.address", obs.Entities)
	}
	host, ok := entityOfType(obs.Entities, entityTypeHost)
	if !ok || host.Attributes[attrRoutesOmitted] != 0 || host.Attributes[attrRoutesLimit] != maxHostRoutes {
		t.Errorf("host counters wrong: %+v", host)
	}
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.ID[idKeyHost] != "h1" || route.ID[idKeyRouteDestination] != "0.0.0.0/0" {
		t.Errorf("route entity wrong: %+v", route)
	}
	if route.ID[idKeyNextHopIP] != "192.168.1.1" || route.Attributes[attrMetric] != int64(100) || route.Attributes[attrRouteType] != routeTypeRemote {
		t.Errorf("route attrs wrong: %+v", route.Attributes)
	}
	addr, ok := entityOfType(obs.Entities, entityTypeNetworkAddress)
	if !ok || addr.ID[idKeyNetworkAddress] != "192.168.1.1" {
		t.Errorf("address entity wrong: %+v", addr)
	}

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

func TestBuildObservation_ConnectedRoute(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{{
		Destination: "10.0.0.0/24",
		NextHop:     "",
		Iface:       "eth0",
		RouteType:   routeTypeLocal,
	}})

	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok {
		t.Fatalf("route entity not found: %+v", obs.Entities)
	}
	if route.ID[idKeyNextHopIP] != "0.0.0.0" {
		t.Errorf("connected next_hop.ip = %v, want 0.0.0.0", route.ID[idKeyNextHopIP])
	}
	if route.Attributes[attrEgressInterface] != "eth0" || route.Attributes[attrRouteType] != routeTypeLocal {
		t.Errorf("connected route attrs wrong: %+v", route.Attributes)
	}
	if _, ok := entityOfType(obs.Entities, entityTypeNetworkAddress); ok {
		t.Error("connected route must not emit a network.address")
	}
	if len(obs.Relations) != 1 || obs.Relations[0].Type != relHasRoute {
		t.Errorf("connected route relations = %+v, want only has_route", obs.Relations)
	}
}

func TestBuildObservation_MetricOmittedWhenZero(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{{Destination: "10.0.0.0/8", NextHop: "10.0.0.1", RouteType: routeTypeRemote}})
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok {
		t.Fatalf("route entity not found: %+v", obs.Entities)
	}
	if _, ok := route.Attributes[attrMetric]; ok {
		t.Errorf("metric 0 should be omitted, got %v", route.Attributes)
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []hostRoute{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", RouteType: routeTypeRemote}}); len(o.Entities) != 0 {
		t.Error("no hostID -> empty")
	}
	obs := buildObservation("h1", nil)
	if len(obs.Entities) != 1 {
		t.Fatalf("no routes should still emit host counters, got %+v", obs.Entities)
	}
	host, ok := entityOfType(obs.Entities, entityTypeHost)
	if !ok || host.Attributes[attrRoutesOmitted] != 0 || host.Attributes[attrRoutesLimit] != maxHostRoutes {
		t.Errorf("host counters wrong: %+v", host)
	}
}

func TestObserve_InjectedReader(t *testing.T) {
	s := &Source{
		hostID: func() string { return "h1" },
		readRoutes: func() ([]hostRoute, error) {
			return []hostRoute{{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", RouteType: routeTypeRemote}}, nil
		},
	}
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("successful read must report ok")
	}
	if len(obs.Entities) != 3 || len(obs.Relations) != 2 {
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

func TestBuildObservation_ECMP(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", RouteType: routeTypeRemote},
		{Destination: "0.0.0.0/0", NextHop: "192.168.1.2", RouteType: routeTypeRemote},
	})

	routes := entitiesOfType(obs.Entities, entityTypeNetworkRoute)
	if len(routes) != 2 {
		t.Fatalf("want two ECMP routes, got %+v", routes)
	}
	var nextHopVias int
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia {
			nextHopVias++
		}
	}
	if nextHopVias != 2 {
		t.Errorf("want two next_hop_via edges, got %d", nextHopVias)
	}
	if routes[0].ID[idKeyRouteDestination] != "0.0.0.0/0" || routes[1].ID[idKeyRouteDestination] != "0.0.0.0/0" {
		t.Errorf("ECMP destinations wrong: %+v", routes)
	}
}

func makeManyRoutes(n int) []hostRoute {
	routes := []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", RouteType: routeTypeRemote},
		{Destination: "10.1.0.0/24", NextHop: "", Iface: "eth0", RouteType: routeTypeLocal},
	}
	for i := 0; i < n; i++ {
		routes = append(routes, hostRoute{
			Destination: fmt.Sprintf("10.%d.%d.0/24", 100+i/250, i%250),
			NextHop:     fmt.Sprintf("10.%d.%d.1", 100+i/250, i%250),
			RouteType:   routeTypeRemote,
		})
	}
	return routes
}

func TestBuildObservation_Bound(t *testing.T) {
	obs := buildObservation("h1", makeManyRoutes(598))
	host, ok := entityOfType(obs.Entities, entityTypeHost)
	if !ok {
		t.Fatalf("host entity missing: %+v", obs.Entities)
	}
	if host.Attributes[attrRoutesOmitted] != 100 || host.Attributes[attrRoutesLimit] != maxHostRoutes {
		t.Errorf("host counters = %+v, want omitted 100 limit 500", host.Attributes)
	}
	routes := entitiesOfType(obs.Entities, entityTypeNetworkRoute)
	if len(routes) != 500 {
		t.Errorf("route count = %d, want 500", len(routes))
	}
	var foundDefault, foundLocal bool
	for _, r := range routes {
		if r.ID[idKeyRouteDestination] == "0.0.0.0/0" {
			foundDefault = true
		}
		if r.ID[idKeyRouteDestination] == "10.1.0.0/24" {
			foundLocal = true
		}
	}
	if !foundDefault || !foundLocal {
		t.Errorf("default or local route was omitted: default=%v local=%v", foundDefault, foundLocal)
	}
}

func TestBuildObservation_BelowBound(t *testing.T) {
	obs := buildObservation("h1", makeManyRoutes(3))
	host, ok := entityOfType(obs.Entities, entityTypeHost)
	if !ok {
		t.Fatalf("host entity missing: %+v", obs.Entities)
	}
	if host.Attributes[attrRoutesOmitted] != 0 || host.Attributes[attrRoutesLimit] != maxHostRoutes {
		t.Errorf("host counters = %+v, want omitted 0 limit 500", host.Attributes)
	}
	routes := entitiesOfType(obs.Entities, entityTypeNetworkRoute)
	if len(routes) != 5 {
		t.Errorf("route count = %d, want 5", len(routes))
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

func TestBuildObservation_NextHopViaNeedsAddressAndInterfaceToAgree(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Iface: "eth0", RouteType: routeTypeRemote},
		{Destination: "10.2.0.0/16", NextHop: "172.18.0.1", Iface: "br-02338442b035", RouteType: routeTypeRemote},
		{Destination: "10.3.0.0/16", NextHop: "172.17.0.1", Iface: "eth0", RouteType: routeTypeRemote},
		{Destination: "10.4.0.0/16", NextHop: "169.254.1.1", Iface: "eth0", RouteType: routeTypeRemote},
		{Destination: "10.5.0.0/16", NextHop: "127.0.0.2", Iface: "eth0", RouteType: routeTypeRemote},
		{Destination: "10.6.0.0/16", NextHop: "10.10.0.9", Iface: "veth3a9f1c2", RouteType: routeTypeRemote},
	})
	got := nextHopViaTargets(obs)
	if len(got) != 1 || !got["10.10.0.1"] {
		t.Errorf("only the routed gateway keeps its edge, got %v", got)
	}
}

func TestBuildObservation_K3sNodeKeepsItsDefaultRouteEdge(t *testing.T) {
	obs := buildObservation("node-1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "192.168.1.1", Iface: "ens18", RouteType: routeTypeRemote},
		{Destination: "10.1.0.0/24", NextHop: "10.1.0.1", Iface: "flannel.1", RouteType: routeTypeRemote},
		{Destination: "10.2.0.0/24", NextHop: "10.2.0.1", Iface: "flannel.1", RouteType: routeTypeRemote},
		{Destination: "0.0.0.0/0", NextHop: "169.254.1.1", Iface: "eth0", RouteType: routeTypeRemote},
	})

	got := nextHopViaTargets(obs)
	if len(got) != 1 || !got["192.168.1.1"] {
		t.Errorf("want next_hop_via to the LAN gateway only, got %v", got)
	}
	routes := entitiesOfType(obs.Entities, entityTypeNetworkRoute)
	if len(routes) != 4 {
		t.Errorf("routes emitted = %d, want 4 (default, 2 flannel, calico default)", len(routes))
	}
}

func TestBuildObservation_WindowsDefaultRouteKeepsItsEdge(t *testing.T) {
	obs := buildObservation("win-1", []hostRoute{
		{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Metric: 25, Iface: "Ethernet", RouteType: routeTypeRemote},
		{Destination: "10.20.0.0/16", NextHop: "10.10.0.254", Metric: 281, Iface: "Ethernet", RouteType: routeTypeRemote},
		{Destination: "172.28.0.0/20", NextHop: "172.28.0.1", Metric: 5256, Iface: "vEthernet (WSL)", RouteType: routeTypeRemote},
		{Destination: "172.29.0.0/20", NextHop: "172.29.0.1", Metric: 5256, Iface: "vEthernet (Default Switch)", RouteType: routeTypeRemote},
	})
	got := nextHopViaTargets(obs)
	if len(got) != 2 || !got["10.10.0.1"] || !got["10.10.0.254"] {
		t.Errorf("only the Ethernet routes keep an edge, got %v", got)
	}
}

func TestParseForwardTable(t *testing.T) {
	row := func(dst, mask, nh [4]byte, ifIndex, typ, proto, metric uint32) []byte {
		b := make([]byte, forwardRowSize)
		copy(b[offDest:], dst[:])
		copy(b[offMask:], mask[:])
		copy(b[offNextHop:], nh[:])
		binary.LittleEndian.PutUint32(b[offIfIndex:], ifIndex)
		binary.LittleEndian.PutUint32(b[offType:], typ)
		binary.LittleEndian.PutUint32(b[offProto:], proto)
		binary.LittleEndian.PutUint32(b[offMetric1:], metric)
		return b
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 3)
	buf = append(buf, row([4]byte{0, 0, 0, 0}, [4]byte{0, 0, 0, 0}, [4]byte{10, 10, 0, 1}, 7, forwardTypeIndirect, 3, 25)...)
	buf = append(buf, row([4]byte{10, 10, 0, 0}, [4]byte{255, 255, 255, 0}, [4]byte{10, 10, 0, 60}, 7, forwardTypeDirect, 3, 281)...)
	buf = append(buf, row([4]byte{0, 0, 0, 0}, [4]byte{0, 0, 0, 0}, [4]byte{10, 10, 0, 1}, 7, forwardTypeIndirect, 3, 25)...)

	got := parseForwardTable(buf, func(i int) string { return "Ethernet" })
	if len(got) != 2 {
		t.Fatalf("want default remote + direct connected, got %+v", got)
	}
	if got[0].Destination != "0.0.0.0/0" || got[0].NextHop != "10.10.0.1" || got[0].Metric != 25 || got[0].Iface != "Ethernet" || got[0].RouteType != routeTypeRemote {
		t.Errorf("default route = %+v", got[0])
	}
	if got[1].Destination != "10.10.0.0/24" || got[1].NextHop != "0.0.0.0" || got[1].Metric != 281 || got[1].Iface != "Ethernet" || got[1].RouteType != routeTypeLocal {
		t.Errorf("direct route = %+v", got[1])
	}
	if got[0].ProtocolName != "netmgmt" || got[0].ProtocolCode == nil || *got[0].ProtocolCode != 3 {
		t.Errorf("default protocol = %+v code %v", got[0].ProtocolName, got[0].ProtocolCode)
	}
	if parseForwardTable([]byte{1, 0}, nil) != nil {
		t.Error("a truncated buffer yields no route")
	}
}

func TestParseForwardTable_TwoGatewaysAreTwoRoutes(t *testing.T) {
	mk := func(nh byte, ifIndex, metric uint32) []byte {
		b := make([]byte, forwardRowSize)
		copy(b[offNextHop:], []byte{10, 0, 0, nh})
		binary.LittleEndian.PutUint32(b[offIfIndex:], ifIndex)
		binary.LittleEndian.PutUint32(b[offType:], forwardTypeIndirect)
		binary.LittleEndian.PutUint32(b[offMetric1:], metric)
		binary.LittleEndian.PutUint32(b[offProto:], 3)
		return b
	}
	buf := []byte{3, 0, 0, 0}
	buf = append(append(append(buf, mk(2, 9, 50)...), mk(1, 7, 25)...), mk(1, 7, 25)...)
	got := parseForwardTable(buf, nil)
	if len(got) != 2 {
		t.Fatalf("want two default routes (the exact repeat dropped), got %+v", got)
	}
	obs := buildObservation("h1", got)
	routes := entitiesOfType(obs.Entities, entityTypeNetworkRoute)
	if len(routes) != 2 {
		t.Fatalf("want two route entities, got %+v", routes)
	}
	ids := map[string]bool{}
	for _, e := range routes {
		ids[e.ID[idKeyNextHopIP].(string)] = true
	}
	if !ids["10.0.0.1"] || !ids["10.0.0.2"] {
		t.Errorf("route identities = %v", ids)
	}
}

func TestParseForwardTable_ProtoMapping(t *testing.T) {
	mk := func(proto uint32) []byte {
		b := make([]byte, forwardRowSize)
		copy(b[offDest:], []byte{10, 0, 0, 0})
		copy(b[offMask:], []byte{255, 255, 255, 0})
		copy(b[offNextHop:], []byte{10, 0, 0, byte(proto)})
		binary.LittleEndian.PutUint32(b[offIfIndex:], 7)
		binary.LittleEndian.PutUint32(b[offType:], forwardTypeIndirect)
		binary.LittleEndian.PutUint32(b[offProto:], proto)
		return b
	}
	buf := []byte{3, 0, 0, 0}
	buf = append(append(append(buf, mk(3)...), mk(8)...), mk(99)...)
	got := parseForwardTable(buf, nil)
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %+v", got)
	}
	if got[0].ProtocolName != "netmgmt" || got[0].ProtocolCode == nil || *got[0].ProtocolCode != 3 {
		t.Errorf("code 3 = %+v", got[0])
	}
	if got[1].ProtocolName != "rip" || got[1].ProtocolCode == nil || *got[1].ProtocolCode != 8 {
		t.Errorf("code 8 = %+v", got[1])
	}
	if got[2].ProtocolName != "" || got[2].ProtocolCode == nil || *got[2].ProtocolCode != 99 {
		t.Errorf("unknown code = %+v", got[2])
	}
}

func TestBuildObservation_RouteCarriesItsEgressInterface(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{{Destination: "0.0.0.0/0", NextHop: "10.10.0.1", Iface: "Ethernet", RouteType: routeTypeRemote}})
	route, ok := entityOfType(obs.Entities, entityTypeNetworkRoute)
	if !ok || route.Attributes[attrEgressInterface] != "Ethernet" {
		t.Errorf("route attributes = %+v", route.Attributes)
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

func entitiesOfType(es []entity.Entity, typ string) []entity.Entity {
	var out []entity.Entity
	for _, e := range es {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func relationOfType(rs []entity.Relation, typ string) (entity.Relation, bool) {
	for _, r := range rs {
		if r.Type == typ {
			return r, true
		}
	}
	return entity.Relation{}, false
}

// TestBuildObservation_DropRouteHasNoEgressAndInvalidRowsCount checks a
// prohibit route the kernel attaches to lo does not name lo as an egress,
// and that a row listed but not emitted is counted as omitted.
func TestBuildObservation_DropRouteHasNoEgressAndInvalidRowsCount(t *testing.T) {
	obs := buildObservation("h1", []hostRoute{
		{Destination: "10.66.0.0/16", Iface: "lo", RouteType: routeTypeReject},
		{Destination: "10.67.0.0/16", Iface: "lo", RouteType: routeTypeBlackhole},
		{Destination: "10.68.0.0/16", RouteType: routeTypeInvalid},
		{Destination: "10.69.0.0/16", NextHop: "", RouteType: routeTypeRemote},
	})
	for _, e := range entitiesOfType(obs.Entities, entityTypeNetworkRoute) {
		if _, ok := e.Attributes[attrEgressInterface]; ok {
			t.Errorf("%v route names an egress: %v", e.Attributes[attrRouteType], e.Attributes)
		}
		if e.ID[idKeyNextHopIP] != "0.0.0.0" {
			t.Errorf("route without a next hop = %v", e.ID)
		}
	}
	host, _ := entityOfType(obs.Entities, entityTypeHost)
	if host.Attributes[attrRoutesOmitted] != 2 {
		t.Errorf("omitted = %v, want 2 (the invalid row and the remote route without gateway)", host.Attributes[attrRoutesOmitted])
	}
}
