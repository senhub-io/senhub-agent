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
	self := deviceIdentity{Serial: "FOC1", SysName: "core-sw"}
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
	obs := buildObservation(self, topo)

	if len(obs.Entities) != 2 {
		t.Fatalf("want 2 entities, got %d", len(obs.Entities))
	}
	if obs.Entities[0].ID[idKeyNetworkDevice] != "serial:FOC1" {
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
		r.FromID[idKeyNetworkDevice] != "serial:FOC1" ||
		r.ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("relation wrong: %+v", r)
	}
	if r.Attributes["local_port"] != "5" || r.Attributes["remote_port"] != "Gi0/1" {
		t.Errorf("relation attrs: %+v", r.Attributes)
	}
}

func TestBuildObservation_NoNeighbors(t *testing.T) {
	obs := buildObservation(deviceIdentity{Serial: "X"}, lldpTopology{})
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-only expected, got %+v", obs)
	}
}

func TestBuildObservation_NoIdentity(t *testing.T) {
	obs := buildObservation(deviceIdentity{}, lldpTopology{})
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected nothing when device unidentifiable, got %+v", obs)
	}
}

func TestBuildObservation_SkipsSelfLoop(t *testing.T) {
	self := deviceIdentity{ChassisMAC: []byte{0x01, 0x02}}
	topo := lldpTopology{Neighbors: []lldpNeighbor{
		{ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0x01, 0x02}},
	}}
	obs := buildObservation(self, topo)
	if len(obs.Entities) != 1 || len(obs.Relations) != 0 {
		t.Errorf("self-loop should be skipped, got %+v", obs)
	}
}

func TestReadSelfIdentity_Precedence(t *testing.T) {
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalSerialNum: {
			{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("   ")},    // blank → skipped
			{OID: oidEntPhysicalSerialNum + ".2", Value: []byte("ABC123")}, // first real serial
		},
		oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: []byte{0x80, 0x00}}},
		oidSysNameBase:      {{OID: oidSysName, Value: []byte("sw1")}},
	}}
	di := readSelfIdentity(fc, "192.0.2.10", lldpLocal{})
	if di.Serial != "ABC123" || di.SysName != "sw1" {
		t.Fatalf("identity = %+v", di)
	}
	if got := resolveDeviceID(di); got != "serial:ABC123" {
		t.Errorf("resolved = %q, want serial:ABC123", got)
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
