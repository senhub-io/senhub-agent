package snmppoll

import (
	"senhub-agent.go/internal/agent/services/entity"
	"testing"
	"time"
)

func TestPolledRegistry_ReconcilesByMAC(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	r := newPolledRegistry()

	mac := []byte{0xaa, 0xbb, 0xcc, 0x00, 0x11, 0x22}
	// A device polled directly: strong canonical id, exposes its chassis MAC.
	r.recordPolled(deviceIdentity{ChassisMAC: mac, EngineID: []byte{0xde, 0xad}}, "engine:dead", t0)

	// The same device seen as a neighbour (LLDP chassis-id = that MAC) resolves
	// to the canonical id, not a mac: shadow.
	got, ok := r.canonicalFor(deviceIdentity{ChassisMAC: mac, SysName: "neigh"}, t0)
	if !ok || got != "engine:dead" {
		t.Errorf("canonicalFor = %q,%v, want engine:dead,true", got, ok)
	}

	// A different MAC does not match.
	if _, ok := r.canonicalFor(deviceIdentity{ChassisMAC: []byte{0x99}}, t0); ok {
		t.Error("unknown MAC must not reconcile")
	}
	// A neighbour with no MAC cannot reconcile.
	if _, ok := r.canonicalFor(deviceIdentity{SysName: "neigh"}, t0); ok {
		t.Error("MAC-less neighbour must not reconcile (sysName is not a key)")
	}
}

func TestPolledRegistry_NoMACNotRecorded(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	r := newPolledRegistry()
	// A device with no chassis MAC (e.g. resolved by mgmt: only) registers nothing.
	r.recordPolled(deviceIdentity{MgmtIP: "10.0.0.1"}, "mgmt:10.0.0.1", t0)
	if len(r.byMAC) != 0 {
		t.Errorf("MAC-less device must not be recorded, got %v", r.byMAC)
	}
	// An empty canonical id is not recorded either.
	r.recordPolled(deviceIdentity{ChassisMAC: []byte{0x01, 0x02}}, "", t0)
	if len(r.byMAC) != 0 {
		t.Errorf("empty id must not be recorded, got %v", r.byMAC)
	}
}

func TestPolledRegistry_TTLExpiry(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	r := newPolledRegistry()
	mac := []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	r.recordPolled(deviceIdentity{ChassisMAC: mac}, "engine:x", t0)

	// Within TTL → hit.
	if _, ok := r.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(polledRegistryTTL)); !ok {
		t.Error("entry must still be fresh at exactly the TTL boundary")
	}
	// Past TTL → miss.
	if _, ok := r.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(polledRegistryTTL+time.Second)); ok {
		t.Error("stale entry past TTL must not reconcile")
	}
	// A later write evicts the stale entry.
	r.recordPolled(deviceIdentity{ChassisMAC: []byte{0x01}}, "engine:y", t0.Add(2*polledRegistryTTL))
	if _, present := r.byMAC[macHex(mac)]; present {
		t.Error("stale entry must be evicted on a later write")
	}
}

// TestBuildObservation_NeighborReconciledViaRegistry pins the end-to-end fix:
// a neighbour known by a MAC that the registry maps to a polled device's
// canonical id is linked under that canonical id: the connected_to remote
// endpoint rides the id the neighbour's own poll assigned, not a mac: shadow.
func TestBuildObservation_NeighborReconciledViaRegistry(t *testing.T) {
	neighMAC := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	resolve := func(n deviceIdentity) string {
		if len(n.ChassisMAC) > 0 && macHex(n.ChassisMAC) == macHex(neighMAC) {
			return "serial:9:NEIGH" // the canonical id the neighbour's own poll assigned
		}
		return resolveDeviceID(n)
	}

	self := deviceIdentity{Serial: "FOC1", VendorPEN: "9", SysName: "core-sw"}
	topo := lldpTopology{
		Local: lldpLocal{Ports: map[string]string{"5": "Gi1/0/5"}},
		Neighbors: []lldpNeighbor{{
			LocalPortNum:     "5",
			ChassisIdSubtype: subtypeMacAddress,
			ChassisId:        neighMAC,
			PortIdSubtype:    portSubtypeIfName,
			PortId:           []byte("Gi0/1"),
			SysName:          "neigh",
		}},
	}
	obs := buildObservation(self, topo, nil, nil, nil, resolve)

	// connected_to remote endpoint uses the canonical device id.
	var found bool
	for _, r := range obs.Relations {
		if r.Type == relConnectedTo {
			found = true
			if r.ToID[idKeyNetworkDevice] != "serial:9:NEIGH" {
				t.Errorf("connected_to remote device = %v, want serial:9:NEIGH", r.ToID[idKeyNetworkDevice])
			}
		}
	}
	if !found {
		t.Error("expected a connected_to edge to the reconciled neighbour")
	}
}

// TestDropLinksToDownPorts checks a link a neighbour still lists is dropped
// when the far end's own sweep read its port down (an admin shutdown on one
// side of a virtual link leaves the other side up), and kept otherwise.
func TestDropLinksToDownPorts(t *testing.T) {
	now := time.Unix(1780272000, 0)
	link := func() entity.Observation {
		return entity.Observation{Relations: []entity.Relation{{
			Type:     relConnectedTo,
			FromType: entityTypeNetworkInterface, FromID: interfacePortKey("sw2", "Ethernet1"),
			ToType: entityTypeNetworkInterface, ToID: interfacePortKey("sw1", "Ethernet2"),
		}}}
	}
	for _, tc := range []struct {
		name  string
		state int
		age   time.Duration
		want  int
	}{
		{"far port down", ifOperDown, 0, 0},
		{"far port up", ifOperUp, 0, 1},
		{"far port down, stale", ifOperDown, polledRegistryTTL + time.Second, 1},
		{"far port unread", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPolledRegistry()
			r.recordPortStates("sw1", []ifaceRow{{Index: "2", Name: "Ethernet2", OperStatus: tc.state}}, now)
			obs := link()
			dropLinksToDownPorts(&obs, r, now.Add(tc.age))
			if len(obs.Relations) != tc.want {
				t.Errorf("relations = %d, want %d", len(obs.Relations), tc.want)
			}
		})
	}

	r := newPolledRegistry()
	r.recordPortStates("sw1", []ifaceRow{{Name: "Ethernet2", OperStatus: ifOperDown}}, now)
	r.recordPortStates("sw1", []ifaceRow{{Name: "Ethernet2", OperStatus: ifOperUp}}, now.Add(time.Minute))
	obs := link()
	dropLinksToDownPorts(&obs, r, now.Add(time.Minute))
	if len(obs.Relations) != 1 {
		t.Errorf("a port read up again must carry the link again")
	}
}
