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

func TestBuildObservation(t *testing.T) {
	self := deviceIdentity{Serial: "FOC1", VendorPEN: "9", SysName: "core-sw"}
	topo := lldpTopology{
		Neighbors: []lldpNeighbor{{
			LocalPortNum:     "5",
			ChassisIdSubtype: subtypeMacAddress,
			ChassisId:        []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			PortIdSubtype:    portSubtypeIfName,
			PortId:           []byte("Gi0/1"),
			SysName:          "neigh",
		}},
	}
	obs := buildObservation(self, topo, nil, nil)

	if len(obs.Entities) != 2 {
		t.Fatalf("want 2 entities, got %d", len(obs.Entities))
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
	if r.Type != relAdjacentTo ||
		r.FromID[idKeyNetworkDevice] != "serial:9:FOC1" ||
		r.ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("relation wrong: %+v", r)
	}
	if r.Attributes["local_port"] != "5" || r.Attributes["remote_port"] != "Gi0/1" {
		t.Errorf("relation attrs: %+v", r.Attributes)
	}
}

func TestBuildObservation_NoNeighbors(t *testing.T) {
	obs := buildObservation(deviceIdentity{Serial: "X", VendorPEN: "9"}, lldpTopology{}, nil, nil)
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-only expected, got %+v", obs)
	}
}

func TestBuildObservation_NoIdentity(t *testing.T) {
	obs := buildObservation(deviceIdentity{}, lldpTopology{}, nil, nil)
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected nothing when device unidentifiable, got %+v", obs)
	}
}

func TestBuildObservation_SkipsSelfLoop(t *testing.T) {
	self := deviceIdentity{ChassisMAC: []byte{0x01, 0x02}}
	topo := lldpTopology{Neighbors: []lldpNeighbor{
		{ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0x01, 0x02}},
	}}
	obs := buildObservation(self, topo, nil, nil)
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-loop should be skipped, got %+v", obs)
	}
}

func TestBuildObservation_RoutesVia(t *testing.T) {
	self := deviceIdentity{Serial: "S1", VendorPEN: "9", MgmtIP: "10.0.0.1"}
	routes := []routeRow{
		{NextHop: "10.0.0.254", Type: routeTypeRemote, Metric: 1}, // gateway
		{NextHop: "10.0.0.254", Type: routeTypeRemote, Metric: 1}, // same next-hop → deduped
		{NextHop: "10.0.0.2", Type: routeTypeRemote},              // another next-hop
		{NextHop: "0.0.0.0", Type: routeTypeRemote},               // unspecified → skip
		{NextHop: "10.0.0.1", Type: routeTypeRemote},              // == self mgmt → skip
		{NextHop: "10.0.0.99", Type: 3},                           // not remote → skip
	}
	obs := buildObservation(self, lldpTopology{}, routes, nil)

	// self + 2 distinct next-hop devices
	if len(obs.Entities) != 3 {
		t.Fatalf("entities = %d (%+v)", len(obs.Entities), obs.Entities)
	}
	via := 0
	for _, r := range obs.Relations {
		if r.Type != relRoutesVia {
			continue
		}
		via++
		if r.FromID[idKeyNetworkDevice] != "serial:9:S1" {
			t.Errorf("routes_via from = %v", r.FromID)
		}
		if r.ToID[idKeyNetworkDevice] == "mgmt:10.0.0.254" && r.Attributes["destinations"] != int64(2) {
			t.Errorf("expected 2 destinations via gateway, got %v", r.Attributes["destinations"])
		}
	}
	if via != 2 {
		t.Fatalf("expected 2 routes_via edges, got %d", via)
	}
}

func TestBuildObservation_ForwardsTo(t *testing.T) {
	neighMAC := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	self := deviceIdentity{Serial: "S1", VendorPEN: "9"}
	topo := lldpTopology{Neighbors: []lldpNeighbor{
		{LocalPortNum: "5", ChassisIdSubtype: subtypeMacAddress, ChassisId: neighMAC, SysName: "neigh"},
	}}
	fdb := []fdbEntry{
		{MAC: "aa:bb:cc:dd:ee:ff", BridgePort: "5"}, // known device (LLDP neighbour) → forwards_to
		{MAC: "11:22:33:44:55:66", BridgePort: "9"}, // unknown (host) → filtered out
	}
	obs := buildObservation(self, topo, nil, fdb)

	fwd := 0
	for _, r := range obs.Relations {
		if r.Type != relForwardsTo {
			continue
		}
		fwd++
		if r.ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" || r.Attributes["bridge_port"] != "5" {
			t.Errorf("forwards_to wrong: %+v", r)
		}
	}
	if fwd != 1 {
		t.Fatalf("expected 1 forwards_to (host MAC filtered), got %d", fwd)
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
	if len(obs.Entities) != 1 || obs.Entities[0].Attributes["sys_name"] != "first" {
		t.Fatalf("after first sweep: %+v", obs)
	}
	if obs.Entities[0].ID[idKeyNetworkDevice] != "name:first" {
		t.Errorf("self id = %v (want name:first)", obs.Entities[0].ID)
	}

	// Within the interval → no re-sweep; cache unchanged even with fresh data.
	s.maybeSweep(mk("second"), t0.Add(1*time.Second))
	if s.Observe().Entities[0].Attributes["sys_name"] != "first" {
		t.Errorf("should not re-sweep within interval")
	}

	// After the interval → re-sweep.
	s.maybeSweep(mk("third"), t0.Add(11*time.Minute))
	if s.Observe().Entities[0].Attributes["sys_name"] != "third" {
		t.Errorf("should re-sweep after interval")
	}
}
