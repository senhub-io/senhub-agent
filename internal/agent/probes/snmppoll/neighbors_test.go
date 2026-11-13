package snmppoll

import (
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

// learnedTestSource has the learned-neighbour inventory on and the shared
// resolves_to flag off: resolution must not need a second opt-in.
func learnedTestSource(t *testing.T) *snmpEntitySource {
	t.Helper()
	s := resolveTestSource(t, false)
	s.cfg.Neighbors = true
	return s
}

func neighborsOf(obs entity.Observation) map[string]entity.Entity {
	out := map[string]entity.Entity{}
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkNeighbor {
			out[e.ID[idKeyNeighborMAC].(string)] = e
		}
	}
	return out
}

func relationsFrom(obs entity.Observation, typ, mac string) []entity.Relation {
	var out []entity.Relation
	for _, r := range obs.Relations {
		if r.Type == typ && r.FromID[idKeyNeighborMAC] == mac {
			out = append(out, r)
		}
	}
	return out
}

func TestNeighbors_EveryLearnedMACIsEmitted(t *testing.T) {
	s := learnedTestSource(t)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())

	got := neighborsOf(obs)
	if len(got) != 2 {
		t.Fatalf("neighbours = %+v, want both learned MACs", got)
	}
	for mac, port := range map[string]string{ownMAC: "Ethernet7", foreignMAC: "Ethernet8"} {
		e, ok := got[mac]
		if !ok {
			t.Fatalf("%s not emitted", mac)
		}
		if e.ID[idKeyNetworkDevice] != switchID {
			t.Errorf("%s identity = %v", mac, e.ID)
		}
		if e.Scope != entity.ScopeSNMPFDB {
			t.Errorf("%s scope = %q, want %q", mac, e.Scope, entity.ScopeSNMPFDB)
		}
		if _, ok := e.Attributes["network.neighbor.observed.interface"]; ok {
			t.Errorf("%s carries observed.interface; neighbor_of carries the port", mac)
		}
		rels := relationsFrom(obs, relNeighborOf, mac)
		if len(rels) != 1 || rels[0].ToID[idKeyNetworkDevice] != switchID || rels[0].ToID[idKeyInterfaceName] != port {
			t.Errorf("%s neighbor_of = %+v, want %s", mac, rels, port)
		}
	}
	if r := relationsFrom(obs, relResolvesTo, foreignMAC); len(r) != 0 {
		t.Errorf("foreign MAC resolved to %+v without a known owner", r)
	}
}

func TestNeighbors_ResolvesToHostNICWithoutSecondOptIn(t *testing.T) {
	s := learnedTestSource(t)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())
	rels := relationsFrom(obs, relResolvesTo, ownMAC)
	if len(rels) != 1 {
		t.Fatalf("resolves_to of the host MAC = %+v, want 1", rels)
	}
	if rels[0].ToID[idKeyHost] != "host-1" || rels[0].ToID[idKeyInterfaceName] != "en0" {
		t.Errorf("resolves_to target = %v, want host-1/en0", rels[0].ToID)
	}
	if rels[0].Attributes[attrBasis] != "fdb" || rels[0].Attributes[attrConfidence] != resolveConfidenceFDB {
		t.Errorf("resolves_to attributes = %v", rels[0].Attributes)
	}
}

func TestNeighbors_ResolvesToPolledDeviceInterface(t *testing.T) {
	s := learnedTestSource(t)
	s.macIndex.Register(foreignMAC, "serial:9:R1", "Gi0/1")
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())
	rels := relationsFrom(obs, relResolvesTo, foreignMAC)
	if len(rels) != 1 || rels[0].ToID[idKeyNetworkDevice] != "serial:9:R1" || rels[0].ToID[idKeyInterfaceName] != "Gi0/1" {
		t.Fatalf("resolves_to = %+v, want serial:9:R1/Gi0/1", rels)
	}
}

func TestNeighbors_SkipsOwnDeviceMAC(t *testing.T) {
	t.Run("interface MAC", func(t *testing.T) {
		s := learnedTestSource(t)
		res := switchSweep()
		res.ifaces = []ifaceRow{{Index: "7", Name: "Ethernet7", Mac: "00:50:56:cc:dd:02"}}
		if _, ok := neighborsOf(s.appendResolvesNeighbors(entity.Observation{}, res))[foreignMAC]; ok {
			t.Fatal("the device's own interface MAC was emitted as a neighbour")
		}
	})
	t.Run("index entry of the same device", func(t *testing.T) {
		s := learnedTestSource(t)
		s.macIndex.Register(foreignMAC, switchID, "Ethernet7")
		if _, ok := neighborsOf(s.appendResolvesNeighbors(entity.Observation{}, switchSweep()))[foreignMAC]; ok {
			t.Fatal("a MAC the device itself owns was emitted as a neighbour")
		}
	})
}

func TestNeighbors_ARPAddressesPortsAndScope(t *testing.T) {
	const arpOnlyMAC = "00-50-56-EE-EE-04"
	s := learnedTestSource(t)
	res := sweepResult{
		deviceID: switchID,
		ifNames:  map[string]string{"8": "Ethernet8", "20": "Vlan20"},
		fdb:      []fdbEntry{{MAC: foreignMAC, IfIndex: 8}},
		arp: []arpEntry{
			{IfIndex: 20, IP: "192.0.2.10", MAC: foreignMAC},
			{IfIndex: 20, IP: "2001:db8::10", MAC: foreignMAC},
			{IfIndex: 20, IP: "169.254.1.1", MAC: "00-50-56-EE-EE-03"},
			{IfIndex: 20, IP: "192.0.2.11", MAC: arpOnlyMAC},
		},
	}
	obs := s.appendResolvesNeighbors(entity.Observation{}, res)
	got := neighborsOf(obs)
	if len(got) != 2 {
		t.Fatalf("neighbours = %+v, want the FDB+ARP MAC and the ARP-only MAC (link-local-only left out)", got)
	}
	if addrs := got[foreignMAC].Attributes[attrNeighborAddresses]; !reflect.DeepEqual(addrs, []string{"192.0.2.10", "2001:db8::10"}) {
		t.Errorf("addresses = %#v", addrs)
	}
	if got[foreignMAC].Scope != entity.ScopeSNMPFDB {
		t.Errorf("FDB+ARP neighbour scope = %q, want %q", got[foreignMAC].Scope, entity.ScopeSNMPFDB)
	}
	if got[arpOnlyMAC].Scope != entity.ScopeSNMPARP {
		t.Errorf("ARP-only neighbour scope = %q, want %q", got[arpOnlyMAC].Scope, entity.ScopeSNMPARP)
	}
	ports := map[string]bool{}
	for _, r := range relationsFrom(obs, relNeighborOf, foreignMAC) {
		ports[r.ToID[idKeyInterfaceName].(string)] = true
	}
	if len(ports) != 2 || !ports["Ethernet8"] || !ports["Vlan20"] {
		t.Errorf("neighbor_of ports = %v, want Ethernet8 and Vlan20", ports)
	}
}

func TestNeighbors_CapKeepsResolvedThenAddressedAndCountsOnDevice(t *testing.T) {
	s := learnedTestSource(t)
	s.cfg.MaxNeighbors = 2
	s.macIndex.Register(foreignMAC, "serial:9:R1", "Gi0/1")
	const macA, macB, macC = "00-50-56-00-00-0A", "00-50-56-00-00-0B", "00-50-56-00-00-0C"
	res := switchSweep()
	res.fdb = []fdbEntry{{MAC: foreignMAC, IfIndex: 8}, {MAC: macA, IfIndex: 8}, {MAC: macB, IfIndex: 8}, {MAC: macC, IfIndex: 8}}
	res.arp = []arpEntry{{IfIndex: 8, IP: "192.0.2.10", MAC: macA}}

	in := entity.Observation{Entities: []entity.Entity{deviceEntity(switchID, nil, entity.ScopeSNMPIFMIB)}}
	obs := s.appendResolvesNeighbors(in, res)
	got := neighborsOf(obs)
	if len(got) != 2 {
		t.Fatalf("neighbours = %v, want 2 kept", got)
	}
	if _, ok := got[foreignMAC]; !ok {
		t.Error("the resolved neighbour was not kept first")
	}
	if _, ok := got[macA]; !ok {
		t.Error("the addressed neighbour was not kept second")
	}
	dev := obs.Entities[0]
	if dev.Attributes[attrNeighborsOmitted] != int64(2) || dev.Attributes[attrNeighborsLimit] != int64(2) {
		t.Errorf("device counters = %v, want omitted 2 limit 2", dev.Attributes)
	}
}

func TestNeighbors_DeviceCountsZeroWhenNotCut(t *testing.T) {
	s := learnedTestSource(t)
	in := entity.Observation{Entities: []entity.Entity{deviceEntity(switchID, nil, entity.ScopeSNMPIFMIB)}}
	dev := s.appendResolvesNeighbors(in, switchSweep()).Entities[0]
	if dev.Attributes[attrNeighborsOmitted] != int64(0) || dev.Attributes[attrNeighborsLimit] != int64(defaultMaxNeighbors) {
		t.Errorf("device counters = %v, want omitted 0 limit %d", dev.Attributes, defaultMaxNeighbors)
	}
}

// TestNeighbors_OffKeepsHostNICPath checks that without the neighbors
// parameter only the host's own MAC is emitted, and only with resolves_to
// enabled, as before the learned-neighbour inventory existed.
func TestNeighbors_OffKeepsHostNICPath(t *testing.T) {
	obs := resolveTestSource(t, true).appendResolvesNeighbors(entity.Observation{}, switchSweep())
	got := neighborsOf(obs)
	if len(got) != 1 {
		t.Fatalf("neighbours = %v, want only the host's own MAC", got)
	}
	if got[ownMAC].Scope != entity.ScopeSNMPIFMIB || len(got[ownMAC].Attributes) != 0 {
		t.Errorf("host-NIC neighbour = %+v, want the previous bare snmp-ifmib entity", got[ownMAC])
	}
	if r := relationsFrom(obs, relResolvesTo, ownMAC); len(r) != 1 || r[0].ToID[idKeyHost] != "host-1" {
		t.Errorf("resolves_to = %+v", r)
	}
	if obs := resolveTestSource(t, false).appendResolvesNeighbors(entity.Observation{}, switchSweep()); len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Fatalf("both flags off emitted %+v", obs)
	}
}

func TestParseConfig_Neighbors(t *testing.T) {
	base := func(extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"mibs": []interface{}{"mib-2"}, "target": "192.0.2.10"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cfg, err := parseConfig(base(nil))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Neighbors || cfg.MaxNeighbors != defaultMaxNeighbors {
		t.Errorf("defaults: neighbors=%v max=%d, want off and %d", cfg.Neighbors, cfg.MaxNeighbors, defaultMaxNeighbors)
	}
	cfg, err = parseConfig(base(map[string]interface{}{"neighbors": true, "max_neighbors": 50}))
	if err != nil || !cfg.Neighbors || cfg.MaxNeighbors != 50 {
		t.Errorf("neighbors on, max 50: cfg=%+v err=%v", cfg, err)
	}
	for _, bad := range []int{0, -1} {
		if _, err := parseConfig(base(map[string]interface{}{"max_neighbors": bad})); err == nil {
			t.Errorf("max_neighbors %d accepted", bad)
		}
	}
}

func TestParseARPDropsLocalRows(t *testing.T) {
	binds := []snmpRawBind{
		{OID: ipNetToPhysicalEntry + ".4.20.1.4.192.0.2.10", Value: []byte{0x00, 0x50, 0x56, 0xAA, 0xBB, 0x01}},
		{OID: ipNetToPhysicalEntry + ".6.20.1.4.192.0.2.10", Value: 5},
	}
	if got := parseIPNetToPhysicalTable(binds); len(got) != 0 {
		t.Fatalf("local ARP row not dropped: %+v", got)
	}
}
