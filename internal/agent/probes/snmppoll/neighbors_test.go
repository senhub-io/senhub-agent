package snmppoll

import (
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestNeighbors_EveryLearnedMACIsEmitted(t *testing.T) {
	s := resolveTestSource(t, false)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())

	var neighbors []entity.Entity
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkNeighbor {
			neighbors = append(neighbors, e)
		}
	}
	if len(neighbors) != 2 {
		t.Fatalf("neighbors = %+v, want both own and foreign MAC", neighbors)
	}
	got := map[string]bool{}
	for _, e := range neighbors {
		got[e.ID[idKeyNeighborMAC].(string)] = true
		if e.ID[idKeyNetworkDevice] != switchID {
			t.Errorf("device id = %v, want %s", e.ID[idKeyNetworkDevice], switchID)
		}
	}
	if !got[ownMAC] || !got[foreignMAC] {
		t.Fatalf("neighbor MACs = %v, want %s and %s", got, ownMAC, foreignMAC)
	}

	neighborOf, resolves := 0, 0
	for _, r := range obs.Relations {
		switch r.Type {
		case relNeighborOf:
			neighborOf++
			mac := r.FromID[idKeyNeighborMAC].(string)
			iface := r.ToID[idKeyInterfaceName].(string)
			switch mac {
			case ownMAC:
				if iface != "Ethernet7" {
					t.Errorf("ownMAC neighbor_of -> %s, want Ethernet7", iface)
				}
			case foreignMAC:
				if iface != "Ethernet8" {
					t.Errorf("foreignMAC neighbor_of -> %s, want Ethernet8", iface)
				}
			}
		case relResolvesTo:
			resolves++
		}
	}
	if neighborOf != 2 || resolves != 0 {
		t.Fatalf("neighbor_of=%d resolves_to=%d, want 2 and 0", neighborOf, resolves)
	}
}

func TestNeighbors_ResolvesToHostNIC(t *testing.T) {
	s := resolveTestSource(t, true)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())

	var found bool
	for _, r := range obs.Relations {
		if r.Type != relResolvesTo {
			continue
		}
		mac := r.FromID[idKeyNeighborMAC].(string)
		if mac != ownMAC {
			continue
		}
		found = true
		if r.ToID[idKeyHost] != "host-1" || r.ToID[idKeyInterfaceName] != "en0" {
			t.Errorf("resolves_to target = %v, want host-1/en0", r.ToID)
		}
		if r.Attributes[attrBasis] != "fdb" || r.Attributes[attrConfidence] != resolveConfidenceFDB {
			t.Errorf("resolves_to attrs = %v, want fdb/0.7", r.Attributes)
		}
	}
	if !found {
		t.Fatal("ownMAC did not resolve to host NIC")
	}

	if len(obs.Relations) == 0 {
		t.Fatal("no relations emitted")
	}
	for _, r := range obs.Relations {
		if r.Type == relResolvesTo && r.FromID[idKeyNeighborMAC].(string) == foreignMAC {
			t.Errorf("foreignMAC unexpectedly resolved to %v", r.ToID)
		}
	}
}

func TestNeighbors_ResolvesToPolledDeviceInterface(t *testing.T) {
	s := resolveTestSource(t, true)
	s.macIndex.Register(foreignMAC, "serial:9:R1", "Gi0/1")
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())

	var found bool
	for _, r := range obs.Relations {
		if r.Type != relResolvesTo || r.FromID[idKeyNeighborMAC].(string) != foreignMAC {
			continue
		}
		found = true
		if r.ToID[idKeyNetworkDevice] != "serial:9:R1" || r.ToID[idKeyInterfaceName] != "Gi0/1" {
			t.Errorf("resolves_to target = %v, want serial:9:R1/Gi0/1", r.ToID)
		}
	}
	if !found {
		t.Fatal("foreignMAC did not resolve to polled device interface")
	}
}

func TestNeighbors_SkipsOwnDeviceMAC(t *testing.T) {
	t.Run("interface MAC", func(t *testing.T) {
		s := resolveTestSource(t, false)
		res := switchSweep()
		res.ifaces = []ifaceRow{{Index: "7", Name: "Ethernet7", Mac: foreignMAC}}
		obs := s.appendResolvesNeighbors(entity.Observation{}, res)
		for _, e := range obs.Entities {
			if e.ID[idKeyNeighborMAC].(string) == foreignMAC {
				t.Fatalf("own interface MAC emitted as neighbor: %+v", e)
			}
		}
	})

	t.Run("macIndex own device", func(t *testing.T) {
		s := resolveTestSource(t, true)
		s.macIndex.Register(foreignMAC, switchID, "Ethernet7")
		obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())
		for _, e := range obs.Entities {
			if e.ID[idKeyNeighborMAC].(string) == foreignMAC {
				t.Fatalf("own device MAC emitted as neighbor: %+v", e)
			}
		}
	})
}

func TestNeighbors_ARPAddressesAndPorts(t *testing.T) {
	s := resolveTestSource(t, false)
	res := sweepResult{
		deviceID: switchID,
		ifNames:  map[string]string{"8": "Ethernet8", "20": "Vlan20"},
		fdb:      []fdbEntry{{MAC: foreignMAC, IfIndex: 8}},
		arp: []arpEntry{
			{IfIndex: 20, IP: "192.0.2.10", MAC: foreignMAC},
			{IfIndex: 20, IP: "2001:db8::10", MAC: foreignMAC},
			{IfIndex: 20, IP: "169.254.1.1", MAC: "00-50-56-EE-EE-03"},
		},
	}
	obs := s.appendResolvesNeighbors(entity.Observation{}, res)

	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %+v, want exactly one neighbor", obs.Entities)
	}
	neighbor := obs.Entities[0]
	if neighbor.ID[idKeyNeighborMAC].(string) != foreignMAC {
		t.Fatalf("neighbor MAC = %v, want %s", neighbor.ID[idKeyNeighborMAC], foreignMAC)
	}
	addrs, ok := neighbor.Attributes[attrNeighborAddresses].([]string)
	if !ok || !reflect.DeepEqual(addrs, []string{"192.0.2.10", "2001:db8::10"}) {
		t.Fatalf("addresses = %v (%T), want [192.0.2.10 2001:db8::10]", neighbor.Attributes[attrNeighborAddresses], neighbor.Attributes[attrNeighborAddresses])
	}
	observed := neighbor.Attributes[attrNeighborObservedInterface]
	if observed != "Ethernet8" {
		t.Fatalf("observed interface = %v, want Ethernet8", observed)
	}

	ports := map[string]bool{}
	neighborOf := 0
	for _, r := range obs.Relations {
		if r.Type == relNeighborOf {
			neighborOf++
			ports[r.ToID[idKeyInterfaceName].(string)] = true
		}
	}
	if neighborOf != 2 || !ports["Ethernet8"] || !ports["Vlan20"] {
		t.Fatalf("neighbor_of = %d ports=%v, want Ethernet8 and Vlan20", neighborOf, ports)
	}
}

func TestNeighbors_CapKeepsResolvedThenAddressed(t *testing.T) {
	s := resolveTestSource(t, true)
	s.cfg.MaxNeighbors = 2
	s.macIndex.Register(foreignMAC, "serial:9:R1", "Gi0/1")

	macA := "00-50-56-00-00-0A"
	macB := "00-50-56-00-00-0B"
	macC := "00-50-56-00-00-0C"
	res := switchSweep()
	res.fdb = []fdbEntry{
		{MAC: foreignMAC, IfIndex: 8},
		{MAC: macA, IfIndex: 8},
		{MAC: macB, IfIndex: 8},
		{MAC: macC, IfIndex: 8},
	}
	res.arp = []arpEntry{{IfIndex: 8, IP: "192.0.2.10", MAC: macA}}

	obs := s.appendResolvesNeighbors(entity.Observation{}, res)
	if len(obs.Entities) != 2 {
		t.Fatalf("entities = %+v, want exactly two kept", obs.Entities)
	}
	got := map[string]bool{}
	for _, e := range obs.Entities {
		got[e.ID[idKeyNeighborMAC].(string)] = true
	}
	if !got[foreignMAC] || !got[macA] {
		t.Fatalf("kept MACs = %v, want %s and %s", got, foreignMAC, macA)
	}
	if got[macB] || got[macC] {
		t.Fatalf("unexpected MACs kept: %v", got)
	}
}

func TestNeighbors_DisabledByZero(t *testing.T) {
	s := resolveTestSource(t, true)
	s.cfg.NeighborsDisabled = true
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Fatalf("disabled config emitted %+v", obs)
	}
}

func TestParseConfig_MaxNeighbors(t *testing.T) {
	t.Run("absent default", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{"mibs": []interface{}{"mib-2"}, "target": "192.0.2.10"})
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.MaxNeighbors != defaultMaxNeighbors || cfg.NeighborsDisabled {
			t.Fatalf("MaxNeighbors=%d disabled=%v, want %d disabled=false", cfg.MaxNeighbors, cfg.NeighborsDisabled, defaultMaxNeighbors)
		}
	})

	t.Run("zero disables", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{"mibs": []interface{}{"mib-2"}, "target": "192.0.2.10", "max_neighbors": 0})
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if !cfg.NeighborsDisabled {
			t.Fatal("NeighborsDisabled = false, want true")
		}
	})

	t.Run("value", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{"mibs": []interface{}{"mib-2"}, "target": "192.0.2.10", "max_neighbors": 50})
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.MaxNeighbors != 50 || cfg.NeighborsDisabled {
			t.Fatalf("MaxNeighbors=%d disabled=%v, want 50 and false", cfg.MaxNeighbors, cfg.NeighborsDisabled)
		}
	})

	t.Run("negative", func(t *testing.T) {
		_, err := parseConfig(map[string]interface{}{"mibs": []interface{}{"mib-2"}, "target": "192.0.2.10", "max_neighbors": -1})
		if err == nil {
			t.Fatal("parseConfig accepted negative max_neighbors")
		}
	})
}

func TestParseARPDropsLocalRows(t *testing.T) {
	binds := []snmpRawBind{
		{OID: ipNetToPhysicalEntry + ".4.20.1.4.192.0.2.10", Value: []byte{0x00, 0x50, 0x56, 0xAA, 0xBB, 0x01}},
		{OID: ipNetToPhysicalEntry + ".6.20.1.4.192.0.2.10", Value: 5},
	}
	got := parseIPNetToPhysicalTable(binds)
	if len(got) != 0 {
		t.Fatalf("local ARP row not dropped: %+v", got)
	}
}
