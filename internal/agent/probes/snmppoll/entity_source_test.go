package snmppoll

import (
	"testing"
	"time"
)

func TestObserve_EmptyBeforeSweep(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.10"}, testLogger(t))
	obs := s.Observe()
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected empty before sweep, got %+v", obs)
	}
}

func TestBuildObservation_ConnectedTo(t *testing.T) {
	self := deviceIdentity{Serial: "FOC1", VendorPEN: "9", SysName: "core-sw"}
	topo := lldpTopology{
		Local: lldpLocal{Ports: map[string]string{"5": "Gi1/0/5"}}, // local port name for port num 5
		Neighbors: []lldpNeighbor{{
			LocalPortNum:     "5",
			ChassisIdSubtype: subtypeMacAddress,
			ChassisId:        []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			PortIdSubtype:    portSubtypeIfName,
			PortId:           []byte("Gi0/1"),
			SysName:          "neigh",
		}},
	}
	obs := buildObservation(self, topo, nil, nil, nil)

	// self device + neighbour device (the remote port entity is referenced by
	// the edge, not emitted here — the neighbour's own poll emits it).
	if len(obs.Entities) != 2 {
		t.Fatalf("want 2 entities, got %d (%+v)", len(obs.Entities), obs.Entities)
	}
	if obs.Entities[0].ID[idKeyNetworkDevice] != "serial:9:FOC1" {
		t.Errorf("self id = %v", obs.Entities[0].ID)
	}
	if obs.Entities[1].ID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("neighbor id = %v", obs.Entities[1].ID)
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("want 1 relation, got %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != relConnectedTo ||
		r.FromType != entityTypeNetworkInterface || r.ToType != entityTypeNetworkInterface {
		t.Errorf("relation type/endpoints wrong: %+v", r)
	}
	if r.FromID[idKeyNetworkDevice] != "serial:9:FOC1" || r.FromID[idKeyInterfaceName] != "Gi1/0/5" {
		t.Errorf("local port = %v, want serial:9:FOC1 / Gi1/0/5", r.FromID)
	}
	if r.ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" || r.ToID[idKeyInterfaceName] != "Gi0/1" {
		t.Errorf("remote port = %v, want mac:aa:bb:cc:dd:ee:ff / Gi0/1", r.ToID)
	}
	if len(r.Attributes) != 0 {
		t.Errorf("connected_to should be a bare edge, got attrs %v", r.Attributes)
	}
}

func TestBuildObservation_ConnectedTo_Gating(t *testing.T) {
	self := deviceIdentity{Serial: "S1", VendorPEN: "9"}
	// ifaces give the local port name via ifIndex == lldpLocPortNum.
	ifaces := []ifaceRow{{Index: "1", Name: "Gi0/1", OperStatus: ifOperUp}}
	topo := lldpTopology{Neighbors: []lldpNeighbor{
		{ // remote port is MAC-only → no phantom port, skip the link (point 7)
			LocalPortNum: "1", ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			PortIdSubtype: 3 /* macAddress */, PortId: []byte{0x0a, 0x0b}, SysName: "n1",
		},
		{ // local port unknown (no ifIndex 9, no LLDP loc port) → cannot anchor, skip
			LocalPortNum: "9", ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
			PortIdSubtype: portSubtypeIfName, PortId: []byte("Gi0/2"), SysName: "n2",
		},
	}}
	obs := buildObservation(self, topo, nil, ifaces, nil)

	for _, r := range obs.Relations {
		if r.Type == relConnectedTo {
			t.Errorf("no connected_to expected (MAC-only remote + unanchored local), got %+v", r)
		}
	}
	// Both neighbours are still discovered as network.device entities.
	var devs int
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkDevice {
			devs++
		}
	}
	if devs != 3 { // self + 2 neighbours
		t.Errorf("device entities = %d, want 3 (self + 2 neighbours)", devs)
	}
}

func TestBuildObservation_NoNeighbors(t *testing.T) {
	obs := buildObservation(deviceIdentity{Serial: "X", VendorPEN: "9"}, lldpTopology{}, nil, nil, nil)
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-only expected, got %+v", obs)
	}
}

func TestBuildObservation_NoIdentity(t *testing.T) {
	obs := buildObservation(deviceIdentity{}, lldpTopology{}, nil, nil, nil)
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected nothing when device unidentifiable, got %+v", obs)
	}
}

func TestBuildObservation_SkipsSelfLoop(t *testing.T) {
	self := deviceIdentity{ChassisMAC: []byte{0x01, 0x02}}
	topo := lldpTopology{Neighbors: []lldpNeighbor{
		{ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0x01, 0x02}},
	}}
	obs := buildObservation(self, topo, nil, nil, nil)
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-loop should be skipped, got %+v", obs)
	}
}

func TestBuildObservation_NetworkRoute(t *testing.T) {
	self := deviceIdentity{Serial: "S1", VendorPEN: "9", MgmtIP: "10.0.0.1"}
	routes := []routeRow{
		{Destination: "10.20.0.0/16", NextHop: "10.0.0.254", Type: routeTypeRemote, Metric: 10},
		{Destination: "10.20.0.0/16", NextHop: "10.0.0.2", Type: routeTypeRemote}, // same dest (ECMP) → keep first
		{Destination: "0.0.0.0/0", NextHop: "10.0.0.254", Type: routeTypeRemote},  // default route
		{Destination: "10.30.0.0/16", NextHop: "0.0.0.0", Type: routeTypeRemote},  // unspecified next-hop → skip
		{Destination: "10.40.0.0/16", NextHop: "10.0.0.1", Type: routeTypeRemote}, // == self mgmt → skip
		{Destination: "10.50.0.0/16", NextHop: "10.0.0.9", Type: 3},               // not remote → skip
		{Destination: "", NextHop: "10.0.0.7", Type: routeTypeRemote},             // unparseable index → skip
	}
	obs := buildObservation(self, lldpTopology{}, routes, nil, nil)

	// self device + 2 distinct route destinations (10.20.0.0/16, 0.0.0.0/0)
	if len(obs.Entities) != 3 {
		t.Fatalf("entities = %d (%+v)", len(obs.Entities), obs.Entities)
	}
	var routeEnts, hasRoute int
	for _, e := range obs.Entities {
		if e.Type != entityTypeNetworkRoute {
			continue
		}
		routeEnts++
		if e.ID[idKeyNetworkDevice] != "serial:9:S1" {
			t.Errorf("route owner = %v, want serial:9:S1", e.ID[idKeyNetworkDevice])
		}
		if e.ID[idKeyRouteDestination] == "10.20.0.0/16" {
			if e.Attributes[attrNextHopIP] != "10.0.0.254" || e.Attributes[attrRouteMetric] != int64(10) {
				t.Errorf("route 10.20.0.0/16 attrs = %+v", e.Attributes)
			}
		}
	}
	for _, r := range obs.Relations {
		if r.Type != relHasRoute {
			continue
		}
		hasRoute++
		if r.FromType != entityTypeNetworkDevice || r.FromID[idKeyNetworkDevice] != "serial:9:S1" ||
			r.ToType != entityTypeNetworkRoute {
			t.Errorf("has_route wrong: %+v", r)
		}
		if len(r.Attributes) != 0 {
			t.Errorf("has_route should be a bare edge, got attrs %v", r.Attributes)
		}
	}
	if routeEnts != 2 || hasRoute != 2 {
		t.Fatalf("routeEnts=%d hasRoute=%d, want 2/2", routeEnts, hasRoute)
	}
}

func TestBuildObservation_NetworkInterface(t *testing.T) {
	self := deviceIdentity{Serial: "S1", VendorPEN: "9"}
	ifaces := []ifaceRow{
		{Index: "1", Name: "Gi0/1", OperStatus: ifOperUp, SpeedMbps: 1000},
		{Index: "2", Name: "Gi0/2", OperStatus: ifOperDown},     // no speed → omitted
		{Index: "3", Name: "Gi0/1", OperStatus: ifOperUp},       // dup name → skip
		{Index: "4", Name: "", OperStatus: ifOperUp},            // unnamed → skip
		{Index: "5", Name: "Lo0", OperStatus: ifOperNotPresent}, // notPresent → skip
	}
	obs := buildObservation(self, lldpTopology{}, nil, ifaces, nil)

	// self device + 2 named present interfaces (Gi0/1, Gi0/2)
	if len(obs.Entities) != 3 {
		t.Fatalf("entities = %d (%+v)", len(obs.Entities), obs.Entities)
	}
	var portEnts, hasIface int
	for _, e := range obs.Entities {
		if e.Type != entityTypeNetworkInterface {
			continue
		}
		portEnts++
		if e.ID[idKeyNetworkDevice] != "serial:9:S1" {
			t.Errorf("port owner = %v, want serial:9:S1", e.ID[idKeyNetworkDevice])
		}
		switch e.ID[idKeyInterfaceName] {
		case "Gi0/1":
			if e.Attributes[attrOperState] != "up" || e.Attributes[attrSpeed] != int64(1_000_000_000) {
				t.Errorf("Gi0/1 attrs = %+v, want up/1e9 bit/s", e.Attributes)
			}
		case "Gi0/2":
			if e.Attributes[attrOperState] != "down" {
				t.Errorf("Gi0/2 oper.state = %v, want down", e.Attributes[attrOperState])
			}
			if _, ok := e.Attributes[attrSpeed]; ok {
				t.Errorf("Gi0/2 speed should be omitted (0), got %v", e.Attributes[attrSpeed])
			}
		}
	}
	for _, r := range obs.Relations {
		if r.Type != relHasInterface {
			continue
		}
		hasIface++
		if r.FromType != entityTypeNetworkDevice || r.FromID[idKeyNetworkDevice] != "serial:9:S1" ||
			r.ToType != entityTypeNetworkInterface {
			t.Errorf("has_interface wrong: %+v", r)
		}
		if len(r.Attributes) != 0 {
			t.Errorf("has_interface should be a bare edge, got attrs %v", r.Attributes)
		}
	}
	if portEnts != 2 || hasIface != 2 {
		t.Fatalf("portEnts=%d hasIface=%d, want 2/2", portEnts, hasIface)
	}
}

func TestBuildObservation_NetworkAddress(t *testing.T) {
	self := deviceIdentity{Serial: "S1", VendorPEN: "9"}
	ifaces := []ifaceRow{{Index: "1", Name: "Gi0/1", OperStatus: ifOperUp}}
	addrs := []ipAddr{
		{IP: "10.0.0.1", IfIndex: "1"},    // bound to Gi0/1 (also a host's gateway → the join)
		{IP: "10.0.0.1", IfIndex: "1"},    // dup IP → skip
		{IP: "192.168.9.9", IfIndex: "9"}, // ifIndex 9 not among ifaces → can't bind, skip
	}
	obs := buildObservation(self, lldpTopology{}, nil, ifaces, addrs)

	var addrEnts, boundTo int
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkAddress {
			addrEnts++
			if e.ID[idKeyNetworkAddress] != "10.0.0.1" {
				t.Errorf("address id = %v, want 10.0.0.1", e.ID[idKeyNetworkAddress])
			}
		}
	}
	for _, r := range obs.Relations {
		if r.Type != relBoundTo {
			continue
		}
		boundTo++
		if r.FromType != entityTypeNetworkAddress || r.FromID[idKeyNetworkAddress] != "10.0.0.1" {
			t.Errorf("bound_to source = %v, want network.address 10.0.0.1", r.FromID)
		}
		if r.ToType != entityTypeNetworkInterface ||
			r.ToID[idKeyNetworkDevice] != "serial:9:S1" || r.ToID[idKeyInterfaceName] != "Gi0/1" {
			t.Errorf("bound_to target = %v, want network.interface serial:9:S1/Gi0/1", r.ToID)
		}
		if len(r.Attributes) != 0 {
			t.Errorf("bound_to should be a bare edge, got attrs %v", r.Attributes)
		}
	}
	if addrEnts != 1 || boundTo != 1 {
		t.Fatalf("addrEnts=%d boundTo=%d, want 1/1 (dup + unbindable skipped)", addrEnts, boundTo)
	}
}

// TestConformanceFixture_CiscoSerial reproduces the Toise conformance fixture
// token serial:9:FOC2150X0AB (Cisco PEN 9), validating the frozen identity.
func TestConformanceFixture_CiscoSerial(t *testing.T) {
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass:     {{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis}},
		oidEntPhysicalSerialNum: {{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("FOC2150X0AB")}},
		oidSysObjectIDBase:      {{OID: oidSysObjectID, Value: "1.3.6.1.4.1.9.1.2068"}}, // Cisco
	}}
	di := readSelfIdentity(fc, "10.0.0.1", lldpLocal{})
	if got := resolveDeviceID(di); got != "serial:9:FOC2150X0AB" {
		t.Errorf("conformance token = %q, want serial:9:FOC2150X0AB", got)
	}
}

func TestReadSelfIdentity_SingleChassis(t *testing.T) {
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass: {
			{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis}, // chassis
			{OID: oidEntPhysicalClass + ".2", Value: 9},                       // module → ignored
		},
		oidEntPhysicalSerialNum: {
			{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("ABC123")}, // chassis serial
			{OID: oidEntPhysicalSerialNum + ".2", Value: []byte("MODSER")}, // module serial → ignored
		},
		oidSysObjectIDBase:  {{OID: oidSysObjectID, Value: "1.3.6.1.4.1.9.1.1"}}, // Cisco PEN 9
		oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: []byte{0x80, 0x00}}},
		oidSysNameBase:      {{OID: oidSysName, Value: []byte("sw1")}},
	}}
	di := readSelfIdentity(fc, "192.0.2.10", lldpLocal{})
	if di.Serial != "ABC123" || di.VendorPEN != "9" || di.SysName != "sw1" {
		t.Fatalf("identity = %+v", di)
	}
	if got := resolveDeviceID(di); got != "serial:9:ABC123" {
		t.Errorf("resolved = %q, want serial:9:ABC123", got)
	}
}

func TestReadSelfIdentity_StackFallsToEngine(t *testing.T) {
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass: {
			{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis},
			{OID: oidEntPhysicalClass + ".2", Value: entPhysicalClassChassis}, // 2nd chassis → stack
		},
		oidEntPhysicalSerialNum: {
			{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("M1")},
			{OID: oidEntPhysicalSerialNum + ".2", Value: []byte("M2")},
		},
		oidSysObjectIDBase:  {{OID: oidSysObjectID, Value: "1.3.6.1.4.1.9.1.1"}},
		oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: []byte{0xab, 0xcd}}},
	}}
	di := readSelfIdentity(fc, "10.0.0.1", lldpLocal{})
	if di.Serial != "" {
		t.Errorf("a stack must not set a member serial, got %q", di.Serial)
	}
	if got := resolveDeviceID(di); got != "engine:abcd" {
		t.Errorf("stack id = %q, want engine:abcd", got)
	}
}

func TestMaybeSweep_PopulatesAndRateLimits(t *testing.T) {
	mk := func(sys string) *fakeClient {
		return &fakeClient{walkRawResult: map[string][]snmpRawBind{
			oidSysNameBase: {{OID: oidSysName, Value: []byte(sys)}},
		}}
	}
	cfg := &config{Target: "192.0.2.10", TopologyInterval: 10 * time.Minute}
	s := newEntitySource(cfg, testLogger(t))

	t0 := time.Now()
	s.maybeSweep(mk("first"), t0)
	obs := s.Observe()
	if len(obs.Entities) != 1 || obs.Entities[0].Attributes["sys.name"] != "first" {
		t.Fatalf("after first sweep: %+v", obs)
	}
	if obs.Entities[0].ID[idKeyNetworkDevice] != "name:first" {
		t.Errorf("self id = %v (want name:first)", obs.Entities[0].ID)
	}

	// Within the interval → no re-sweep; cache unchanged even with fresh data.
	s.maybeSweep(mk("second"), t0.Add(1*time.Second))
	if s.Observe().Entities[0].Attributes["sys.name"] != "first" {
		t.Errorf("should not re-sweep within interval")
	}

	// After the interval → re-sweep.
	s.maybeSweep(mk("third"), t0.Add(11*time.Minute))
	if s.Observe().Entities[0].Attributes["sys.name"] != "third" {
		t.Errorf("should re-sweep after interval")
	}
}

func TestSelfAttrs_Readable(t *testing.T) {
	// Descriptive attributes use the frozen dotted casing so a backend shows a
	// readable device, not just the cryptic id.
	self := deviceIdentity{SysName: "core-sw-01", MgmtIP: "10.0.0.1", VendorPEN: "9", Services: 0x06}
	a := selfAttrs(self)
	if a["sys.name"] != "core-sw-01" {
		t.Errorf("sys.name = %v, want core-sw-01 (dotted key, not sys_name)", a["sys.name"])
	}
	if a["mgmt.ip"] != "10.0.0.1" {
		t.Errorf("mgmt.ip = %v", a["mgmt.ip"])
	}
	if a["device.role"] != "router" {
		t.Errorf("device.role = %v, want router (L2+L3)", a["device.role"])
	}
	if a["vendor"] != "cisco" {
		t.Errorf("vendor = %v, want cisco (PEN 9)", a["vendor"])
	}
	if _, ok := a["sys_name"]; ok {
		t.Error("legacy sys_name (underscore) must not be emitted")
	}
}

func TestDeviceRole(t *testing.T) {
	cases := map[int]string{
		0x04: "router", // layer 3
		0x06: "router", // layer 2+3 → router
		0x02: "switch", // layer 2 only
		0x00: "",       // unread / none
		0x40: "",       // application layer only
	}
	for in, want := range cases {
		if got := deviceRole(in); got != want {
			t.Errorf("deviceRole(%#x) = %q, want %q", in, got, want)
		}
	}
}

func TestVendorName(t *testing.T) {
	if vendorName("9") != "cisco" {
		t.Error("PEN 9 → cisco")
	}
	if vendorName("2636") != "juniper" {
		t.Error("PEN 2636 → juniper")
	}
	if vendorName("99999") != "" {
		t.Error("unknown PEN → empty (PEN still lives in the serial: identity)")
	}
}
