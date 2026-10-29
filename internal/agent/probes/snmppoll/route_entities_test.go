package snmppoll

import (
	"fmt"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func routeEntityByDestNextHop(t *testing.T, obs entity.Observation, dest, nextHop string) *entity.Entity {
	t.Helper()
	for i := range obs.Entities {
		e := &obs.Entities[i]
		if e.Type != entityTypeNetworkRoute {
			continue
		}
		if e.ID[idKeyRouteDestination] == dest && e.ID[attrNextHopIP] == nextHop {
			return e
		}
	}
	t.Fatalf("route entity %s %s not found", dest, nextHop)
	return nil
}

func nextHopViaExistsFor(obs entity.Observation, routeID map[string]any) bool {
	for _, rel := range obs.Relations {
		if rel.Type != relNextHopVia {
			continue
		}
		if rel.FromID[idKeyNetworkDevice] == routeID[idKeyNetworkDevice] &&
			rel.FromID[idKeyRouteDestination] == routeID[idKeyRouteDestination] &&
			rel.FromID[attrNextHopIP] == routeID[attrNextHopIP] {
			return true
		}
	}
	return false
}

func TestAppendRouteEntities_Protocol(t *testing.T) {
	self := deviceIdentity{MgmtIP: "10.0.0.1"}
	ifaces := []ifaceRow{{Index: "5", Name: "Ethernet5"}}
	addrSeen := map[string]bool{}

	t.Run("ipCidrRouteProto 13 ospf", func(t *testing.T) {
		rk := "10.0.0.0.255.255.255.0.0.10.0.0.254"
		rows := parseRouteTable(ipCidrRouteSpec, []snmpRawBind{
			{OID: ipCidrRouteEntry + "." + colRouteNextHop + "." + rk, Value: "10.0.0.254"},
			{OID: ipCidrRouteEntry + "." + colRouteType + "." + rk, Value: routeTypeRemote},
			{OID: ipCidrRouteEntry + "." + colRouteProto + "." + rk, Value: 13},
		})
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, rows, ifaces, addrSeen)
		e := routeEntityByDestNextHop(t, obs, "10.0.0.0/24", "10.0.0.254")
		if got := e.Attributes[attrRouteProtocol]; got != "ospf" {
			t.Errorf("route.protocol = %v, want ospf", got)
		}
		if got := e.Attributes[attrRouteProtocolCode]; got != int64(13) {
			t.Errorf("network.route.protocol.code = %v, want 13", got)
		}
	})

	t.Run("inetCidrRouteProto 14 bgp", func(t *testing.T) {
		rk := inetIndex(inetAddrTypeIPv4, "10.0.0.0", 4, 24)
		rows := parseRouteTable(inetCidrRouteSpec, []snmpRawBind{
			{OID: inetCidrRouteEntry + "." + colInetRouteNextHop + "." + rk, Value: "10.0.0.254"},
			{OID: inetCidrRouteEntry + "." + colInetRouteType + "." + rk, Value: routeTypeRemote},
			{OID: inetCidrRouteEntry + "." + colInetRouteProto + "." + rk, Value: 14},
		})
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, rows, ifaces, addrSeen)
		e := routeEntityByDestNextHop(t, obs, "10.0.0.0/24", "10.0.0.254")
		if got := e.Attributes[attrRouteProtocol]; got != "bgp" {
			t.Errorf("route.protocol = %v, want bgp", got)
		}
		if got := e.Attributes[attrRouteProtocolCode]; got != int64(14) {
			t.Errorf("network.route.protocol.code = %v, want 14", got)
		}
	})

	t.Run("code 3 netmgmt", func(t *testing.T) {
		rk := "10.0.1.0.255.255.255.0.0.10.0.0.254"
		rows := parseRouteTable(ipCidrRouteSpec, []snmpRawBind{
			{OID: ipCidrRouteEntry + "." + colRouteNextHop + "." + rk, Value: "10.0.0.254"},
			{OID: ipCidrRouteEntry + "." + colRouteType + "." + rk, Value: routeTypeRemote},
			{OID: ipCidrRouteEntry + "." + colRouteProto + "." + rk, Value: 3},
		})
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, rows, ifaces, addrSeen)
		e := routeEntityByDestNextHop(t, obs, "10.0.1.0/24", "10.0.0.254")
		if got := e.Attributes[attrRouteProtocol]; got != "netmgmt" {
			t.Errorf("route.protocol = %v, want netmgmt", got)
		}
	})

	t.Run("unknown code", func(t *testing.T) {
		rk := "10.0.2.0.255.255.255.0.0.10.0.0.254"
		rows := parseRouteTable(ipCidrRouteSpec, []snmpRawBind{
			{OID: ipCidrRouteEntry + "." + colRouteNextHop + "." + rk, Value: "10.0.0.254"},
			{OID: ipCidrRouteEntry + "." + colRouteType + "." + rk, Value: routeTypeRemote},
			{OID: ipCidrRouteEntry + "." + colRouteProto + "." + rk, Value: 99},
		})
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, rows, ifaces, addrSeen)
		e := routeEntityByDestNextHop(t, obs, "10.0.2.0/24", "10.0.0.254")
		if val, exists := e.Attributes[attrRouteProtocol]; exists {
			t.Errorf("route.protocol should be absent for unknown code, got %v", val)
		}
		if got := e.Attributes[attrRouteProtocolCode]; got != int64(99) {
			t.Errorf("network.route.protocol.code = %v, want 99", got)
		}
	})
}

func TestAppendRouteEntities_EgressInterface(t *testing.T) {
	self := deviceIdentity{MgmtIP: "10.0.0.1"}
	ifaces := []ifaceRow{{Index: "5", Name: "Ethernet5"}}

	cases := []struct {
		name    string
		ifIndex string
		want    string
	}{
		{"known ifIndex", "5", "Ethernet5"},
		{"unknown ifIndex", "9", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := routeRow{Destination: "10.0.0.0/24", NextHop: "10.0.0.254", Type: routeTypeRemote, IfIndex: c.ifIndex}
			obs := entity.Observation{}
			appendRouteEntities(&obs, "dev1", self, []routeRow{row}, ifaces, map[string]bool{})
			e := routeEntityByDestNextHop(t, obs, "10.0.0.0/24", "10.0.0.254")
			got, exists := e.Attributes[idKeyInterfaceName]
			if c.want == "" {
				if exists {
					t.Errorf("network.interface.name = %v, want absent", got)
				}
			} else if got != c.want {
				t.Errorf("network.interface.name = %v, want %q", got, c.want)
			}
		})
	}
}

func TestAppendRouteEntities_ConnectedRoutes(t *testing.T) {
	self := deviceIdentity{MgmtIP: "10.0.0.1"}
	ifaces := []ifaceRow{{Index: "5", Name: "Ethernet5"}}

	t.Run("ipv4 connected", func(t *testing.T) {
		row := routeRow{Destination: "10.0.1.0/24", Type: routeTypeLocal, IfIndex: "5"}
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, []routeRow{row}, ifaces, map[string]bool{})
		e := routeEntityByDestNextHop(t, obs, "10.0.1.0/24", "0.0.0.0")
		if got := e.ID[attrNextHopIP]; got != "0.0.0.0" {
			t.Errorf("next_hop.ip = %v, want 0.0.0.0", got)
		}
		if got := e.Attributes[idKeyInterfaceName]; got != "Ethernet5" {
			t.Errorf("network.interface.name = %v, want Ethernet5", got)
		}
		if got := e.Attributes[attrRouteType]; got != "local" {
			t.Errorf("route.type = %v, want local", got)
		}
		if nextHopViaExistsFor(obs, e.ID) {
			t.Errorf("connected route must not have next_hop_via")
		}
	})

	t.Run("ipv6 connected", func(t *testing.T) {
		row := routeRow{Destination: "2001:db8:1::/64", Type: routeTypeLocal, IfIndex: "5"}
		obs := entity.Observation{}
		appendRouteEntities(&obs, "dev1", self, []routeRow{row}, ifaces, map[string]bool{})
		e := routeEntityByDestNextHop(t, obs, "2001:db8:1::/64", "::")
		if got := e.ID[attrNextHopIP]; got != "::" {
			t.Errorf("next_hop.ip = %v, want ::", got)
		}
		if got := e.Attributes[idKeyInterfaceName]; got != "Ethernet5" {
			t.Errorf("network.interface.name = %v, want Ethernet5", got)
		}
		if got := e.Attributes[attrRouteType]; got != "local" {
			t.Errorf("route.type = %v, want local", got)
		}
		if nextHopViaExistsFor(obs, e.ID) {
			t.Errorf("connected route must not have next_hop_via")
		}
	})
}

func TestSelectRoutes_Bound(t *testing.T) {
	self := deviceIdentity{MgmtIP: "192.0.2.1"}
	routes := []routeRow{
		{Destination: "10.0.1.0/24", Type: routeTypeLocal, IfIndex: "5"},
		{Destination: "10.0.2.0/24", Type: routeTypeLocal, IfIndex: "6"},
		{Destination: "0.0.0.0/0", Type: routeTypeRemote, NextHop: "10.0.0.254"},
	}
	for i := 0; i < 700; i++ {
		dest := fmt.Sprintf("10.128.%d.%d/32", i/256, i%256)
		routes = append(routes, routeRow{Destination: dest, Type: routeTypeRemote, NextHop: "10.0.0.254"})
	}

	sel := selectRoutes(routes, self.MgmtIP, 500, false, routeFilterContext{})
	kept, omitted := sel.kept, sel.omitted
	if omitted != 203 {
		t.Fatalf("omitted = %d, want 203", omitted)
	}
	if len(kept) != 500 {
		t.Fatalf("kept = %d, want 500", len(kept))
	}

	foundLocal1, foundLocal2, foundDefault := false, false, false
	for _, r := range kept {
		if r.Destination == "10.0.1.0/24" && r.NextHop == "0.0.0.0" {
			foundLocal1 = true
		}
		if r.Destination == "10.0.2.0/24" && r.NextHop == "0.0.0.0" {
			foundLocal2 = true
		}
		if r.Destination == "0.0.0.0/0" && r.NextHop == "10.0.0.254" {
			foundDefault = true
		}
	}
	if !foundLocal1 || !foundLocal2 || !foundDefault {
		t.Fatalf("bound selection did not keep all protected routes: local1=%v local2=%v default=%v", foundLocal1, foundLocal2, foundDefault)
	}

	obs := entity.Observation{}
	obs.Entities = append(obs.Entities, deviceEntity("dev1", nil, entity.ScopeSNMPIFMIB))
	appendRouteEntities(&obs, "dev1", self, kept, nil, map[string]bool{})
	applyRouteCounters(&obs, "dev1", omitted, 0, 500)

	dev := obs.Entities[0]
	if got := dev.Attributes[attrRoutesOmitted]; got != int64(203) {
		t.Errorf("senhub.routes.omitted = %v, want 203", got)
	}
	if got := dev.Attributes[attrRoutesLimit]; got != int64(500) {
		t.Errorf("senhub.routes.limit = %v, want 500", got)
	}

	routeCount := 0
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkRoute {
			routeCount++
		}
	}
	if routeCount != 500 {
		t.Errorf("route entity count = %d, want 500", routeCount)
	}
}

func TestRouteCountersZeroWhenNoOmission(t *testing.T) {
	obs := entity.Observation{}
	obs.Entities = append(obs.Entities, deviceEntity("dev1", nil, entity.ScopeSNMPIFMIB))
	appendRouteEntities(&obs, "dev1", deviceIdentity{MgmtIP: "192.0.2.1"}, []routeRow{
		{Destination: "10.0.1.0/24", Type: routeTypeLocal, IfIndex: "5"},
	}, nil, map[string]bool{})
	applyRouteCounters(&obs, "dev1", 0, 0, 500)

	dev := obs.Entities[0]
	if got := dev.Attributes[attrRoutesOmitted]; got != int64(0) {
		t.Errorf("senhub.routes.omitted = %v, want 0", got)
	}
	if got := dev.Attributes[attrRoutesLimit]; got != int64(500) {
		t.Errorf("senhub.routes.limit = %v, want 500", got)
	}
}

func TestParseConfigMaxRoutes(t *testing.T) {
	base := map[string]interface{}{
		"target": "127.0.0.1",
		"mibs":   []interface{}{"if-mib"},
	}
	cfg, err := parseConfig(base)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MaxRoutes != defaultMaxRoutes {
		t.Errorf("MaxRoutes = %d, want %d", cfg.MaxRoutes, defaultMaxRoutes)
	}

	invalid := map[string]interface{}{
		"target":     "127.0.0.1",
		"mibs":       []interface{}{"if-mib"},
		"max_routes": 0,
	}
	_, err = parseConfig(invalid)
	if err == nil {
		t.Fatal("parseConfig with max_routes=0 succeeded, want error")
	}
	if !strings.Contains(err.Error(), "max_routes must be at least 1") {
		t.Errorf("error %q should mention max_routes must be at least 1", err)
	}
}

// TestRouteEntities_DropRoutesCarryNoEgress checks a reject or blackhole
// route never names an egress interface, even when the device gives one.
func TestRouteEntities_DropRoutesCarryNoEgress(t *testing.T) {
	var obs entity.Observation
	routes := []routeRow{
		{Destination: "10.9.0.0/16", Type: routeTypeBlackhole, IfIndex: "1"},
		{Destination: "10.8.0.0/16", Type: routeTypeReject, IfIndex: "1"},
		{Destination: "10.7.0.0/16", Type: routeTypeLocal, IfIndex: "1"},
	}
	appendRouteEntities(&obs, "serial:9:S1", deviceIdentity{MgmtIP: "10.0.0.1"}, routes,
		[]ifaceRow{{Index: "1", Name: "Ethernet1"}}, nil)
	for _, e := range obs.Entities {
		if e.Type != entityTypeNetworkRoute {
			continue
		}
		_, named := e.Attributes[idKeyInterfaceName]
		switch e.Attributes[attrRouteType] {
		case "reject", "blackhole":
			if named {
				t.Errorf("%v route names an egress interface: %v", e.Attributes[attrRouteType], e.Attributes)
			}
		case "local":
			if !named {
				t.Errorf("connected route lost its egress interface: %v", e.Attributes)
			}
		}
		for _, r := range obs.Relations {
			if r.Type == relNextHopVia && r.FromID[idKeyRouteDestination] == e.ID[idKeyRouteDestination] {
				t.Errorf("route without a next hop has next_hop_via: %+v", r)
			}
		}
	}
}

// TestLinkPrivateNextHops checks a route to a private gateway gains its
// next_hop_via only when the observation holds that address bound to a
// polled device's interface.
func TestLinkPrivateNextHops(t *testing.T) {
	route := func(dst, nh string) entity.Entity {
		return entity.Entity{Type: entityTypeNetworkRoute,
			ID:         map[string]any{idKeyNetworkDevice: "serial:9:R1", idKeyRouteDestination: dst, attrNextHopIP: nh},
			Attributes: map[string]any{attrRouteType: "remote"}}
	}
	obs := entity.Observation{
		Entities: []entity.Entity{
			route("10.98.0.0/24", "10.0.12.2"), // R2's address, polled by this agent
			route("10.97.0.0/24", "10.0.13.3"), // unknown private gateway
			route("10.96.0.0/24", "10.0.14.4"), // bound to a HOST interface only
		},
		Relations: []entity.Relation{
			{Type: relBoundTo, FromType: entityTypeNetworkAddress, FromID: map[string]any{idKeyNetworkAddress: "10.0.12.2"},
				ToType: entityTypeNetworkInterface, ToID: map[string]any{idKeyNetworkDevice: "serial:9:R2", idKeyInterfaceName: "Ethernet1"}},
			{Type: relBoundTo, FromType: entityTypeNetworkAddress, FromID: map[string]any{idKeyNetworkAddress: "10.0.14.4"},
				ToType: entityTypeNetworkInterface, ToID: map[string]any{"host.id": "h1", idKeyInterfaceName: "eth0"}},
		},
	}
	linkPrivateNextHops(&obs)
	var via []string
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia {
			via = append(via, r.ToID[idKeyNetworkAddress].(string))
		}
	}
	if len(via) != 1 || via[0] != "10.0.12.2" {
		t.Fatalf("next_hop_via targets = %v, want only 10.0.12.2", via)
	}
	linkPrivateNextHops(&obs)
	n := 0
	for _, r := range obs.Relations {
		if r.Type == relNextHopVia {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a second pass must not duplicate the edge, got %d", n)
	}
}
