package snmppoll

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/macindex"
)

const (
	ownMAC     = "00-50-56-AA-BB-01"
	foreignMAC = "00-50-56-CC-DD-02"
	switchID   = "serial:9:SW1"
)

func resolveTestSource(t *testing.T, enabled bool) *snmpEntitySource {
	t.Helper()
	s := newEntitySource(&config{Target: "192.0.2.20"}, testLogger(t))
	s.hostID = func() string { return "host-1" }
	s.hostInterfaceMACs = func() (map[string]string, error) {
		return map[string]string{ownMAC: "en0"}, nil
	}
	idx := macindex.New(time.Hour)
	idx.SetEnabled(enabled)
	s.macIndex = idx
	return s
}

func switchSweep() sweepResult {
	return sweepResult{
		deviceID: switchID,
		ifNames:  map[string]string{"7": "Ethernet7", "8": "Ethernet8"},
		fdb: []fdbEntry{
			{MAC: ownMAC, IfIndex: 7},
			{MAC: foreignMAC, IfIndex: 8},
		},
	}
}

// TestResolvesTo_OwnMACOnAccessPort checks the access switch's FDB entry
// for this host's own MAC becomes one device-scoped neighbour, heard on the
// port and resolving to this host's interface; another host's MAC is not
// emitted, since this agent holds only one end of it.
func TestResolvesTo_OwnMACOnAccessPort(t *testing.T) {
	s := resolveTestSource(t, true)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())

	var neighbors []entity.Entity
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkNeighbor {
			neighbors = append(neighbors, e)
		}
	}
	if len(neighbors) != 1 {
		t.Fatalf("neighbours = %+v, want only the host's own MAC", neighbors)
	}
	if neighbors[0].ID[idKeyNetworkDevice] != switchID || neighbors[0].ID[idKeyNeighborMAC] != ownMAC {
		t.Fatalf("neighbour identity = %v", neighbors[0].ID)
	}

	var neighborOf, resolves int
	for _, r := range obs.Relations {
		switch r.Type {
		case relNeighborOf:
			neighborOf++
			if r.ToID[idKeyNetworkDevice] != switchID || r.ToID[idKeyInterfaceName] != "Ethernet7" {
				t.Errorf("neighbor_of → %v, want the switch port Ethernet7", r.ToID)
			}
		case relResolvesTo:
			resolves++
			if r.ToID[idKeyHost] != "host-1" || r.ToID[idKeyInterfaceName] != "en0" {
				t.Errorf("resolves_to → %v, want the host's own en0", r.ToID)
			}
			if r.Attributes[attrBasis] != "fdb" || r.Attributes[attrConfidence] != resolveConfidenceFDB {
				t.Errorf("resolves_to attributes = %v", r.Attributes)
			}
		}
	}
	if neighborOf != 1 || resolves != 1 {
		t.Fatalf("neighbor_of=%d resolves_to=%d, want 1 and 1", neighborOf, resolves)
	}
}

// TestResolvesTo_OffByDefault checks nothing is emitted while the flag is off.
func TestResolvesTo_OffByDefault(t *testing.T) {
	s := resolveTestSource(t, false)
	obs := s.appendResolvesNeighbors(entity.Observation{}, switchSweep())
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Fatalf("flag off emitted %+v", obs)
	}
	if macindex.New(time.Hour).Enabled() {
		t.Error("a new index must start disabled")
	}
}

// TestResolvesTo_DiscoveredDeviceRegistersItsMACs checks interface MACs of a
// device are registered so the host's ARP neighbour can resolve to them.
func TestResolvesTo_DiscoveredDeviceRegistersItsMACs(t *testing.T) {
	s := resolveTestSource(t, true)
	s.registerInterfaceMacs("serial:9:R1", []ifaceRow{{Index: "1", Name: "Ethernet1", Mac: "00-1C-73-00-00-01"}})
	got, ok := s.macIndex.Lookup("00-1C-73-00-00-01")
	if !ok || got.DeviceID != "serial:9:R1" || got.Name != "Ethernet1" {
		t.Fatalf("lookup = %+v, %v", got, ok)
	}
}
