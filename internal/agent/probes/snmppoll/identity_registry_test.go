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
	r.recordPolled(deviceIdentity{ChassisMAC: mac, EngineID: []byte{0xde, 0xad}}, "engine:dead", time.Minute, t0)

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
	r.recordPolled(deviceIdentity{MgmtIP: "10.0.0.1"}, "mgmt:10.0.0.1", time.Minute, t0)
	if len(r.byMAC) != 0 {
		t.Errorf("MAC-less device must not be recorded, got %v", r.byMAC)
	}
	// An empty canonical id is not recorded either.
	r.recordPolled(deviceIdentity{ChassisMAC: []byte{0x01, 0x02}}, "", time.Minute, t0)
	if len(r.byMAC) != 0 {
		t.Errorf("empty id must not be recorded, got %v", r.byMAC)
	}
}

// An entry's freshness follows the cadence of the device that recorded it: a
// slow-swept device stays resolvable between two of its sweeps, and the default
// 1 min cadence keeps the previous 3 min behaviour.
func TestPolledRegistry_EntryFreshnessFollowsItsCadence(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	mac := []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}

	// Recorded with a 15 min cadence: still resolved between two sweeps.
	slow := newPolledRegistry()
	slow.recordPolled(deviceIdentity{ChassisMAC: mac}, "engine:x", 15*time.Minute, t0)
	if _, ok := slow.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(15*time.Minute+time.Second)); !ok {
		t.Error("entry recorded with a 15 min cadence must still resolve at t0+15min+1s")
	}
	if _, ok := slow.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(freshFor(15*time.Minute)+time.Second)); ok {
		t.Error("entry recorded with a 15 min cadence must be gone past freshFor(15 min)")
	}

	// Recorded with the default 1 min cadence: unchanged behaviour.
	fast := newPolledRegistry()
	fast.recordPolled(deviceIdentity{ChassisMAC: mac}, "engine:x", time.Minute, t0)
	if _, ok := fast.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(3*time.Minute)); !ok {
		t.Error("entry must still be fresh at exactly freshFor(1 min)")
	}
	if _, ok := fast.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(3*time.Minute+time.Second)); ok {
		t.Error("entry recorded with a 1 min cadence must be gone past 3 min")
	}
	// A later write evicts the stale entry.
	fast.recordPolled(deviceIdentity{ChassisMAC: []byte{0x01}}, "engine:y", time.Minute, t0.Add(4*time.Minute))
	if _, present := fast.byMAC[macHex(mac)]; present {
		t.Error("stale entry must be evicted on a later write")
	}
}

// The effective sweep cadence is the coarser of the probe interval and the
// topology interval, because a sweep only runs inside a metric collection.
func TestEffectiveSweepCadenceIsTheCoarserInterval(t *testing.T) {
	if got := effectiveSweepCadence(300*time.Second, 2*time.Minute); got != 300*time.Second {
		t.Errorf("effectiveSweepCadence(300s, 2min) = %v, want 300s", got)
	}
	if got := effectiveSweepCadence(time.Minute, 15*time.Minute); got != 15*time.Minute {
		t.Errorf("effectiveSweepCadence(1min, 15min) = %v, want 15min", got)
	}
	// Entries recorded at that cadence stay fresh for freshFor of it.
	if got, want := freshFor(effectiveSweepCadence(300*time.Second, 2*time.Minute)), freshFor(300*time.Second); got != want {
		t.Errorf("freshFor(effectiveSweepCadence(300s, 2min)) = %v, want %v", got, want)
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
		{"far port down, stale", ifOperDown, silentAfterSweeps*time.Minute + time.Second, 1},
		{"far port unread", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPolledRegistry()
			r.recordPortStates("sw1", []ifaceRow{{Index: "2", Name: "Ethernet2", OperStatus: tc.state}}, time.Minute, now)
			obs := link()
			dropLinksToDownPorts(&obs, r, now.Add(tc.age))
			if len(obs.Relations) != tc.want {
				t.Errorf("relations = %d, want %d", len(obs.Relations), tc.want)
			}
		})
	}

	r := newPolledRegistry()
	r.recordPortStates("sw1", []ifaceRow{{Name: "Ethernet2", OperStatus: ifOperDown}}, time.Minute, now)
	r.recordPortStates("sw1", []ifaceRow{{Name: "Ethernet2", OperStatus: ifOperUp}}, time.Minute, now.Add(time.Minute))
	obs := link()
	dropLinksToDownPorts(&obs, r, now.Add(time.Minute))
	if len(obs.Relations) != 1 {
		t.Errorf("a port read up again must carry the link again")
	}
}

// TestAnnotatePeerState checks the local interface of a link says how much
// evidence stands behind the far end, and says nothing when both ends were read.
func TestAnnotatePeerState(t *testing.T) {
	now := time.Unix(1780272000, 0)
	const iv = 10 * time.Minute
	obs := func() entity.Observation {
		return entity.Observation{
			Entities: []entity.Entity{{Type: entityTypeNetworkInterface, ID: interfacePortKey("sw2", "Ethernet1")}},
			Relations: []entity.Relation{{
				Type:     relConnectedTo,
				FromType: entityTypeNetworkInterface, FromID: interfacePortKey("sw2", "Ethernet1"),
				ToType: entityTypeNetworkInterface, ToID: interfacePortKey("sw1", "Ethernet2"),
			}},
		}
	}
	up := []ifaceRow{{Name: "Ethernet2", OperStatus: ifOperUp}}
	for _, tc := range []struct {
		name string
		set  func(r *polledRegistry)
		at   time.Duration
		want any
	}{
		{"far end never read", func(*polledRegistry) {}, 0, peerStateUnread},
		{"far end read", func(r *polledRegistry) { r.recordPortStates("sw1", up, iv, now) }, 0, nil},
		{"slow far end between two sweeps", func(r *polledRegistry) { r.recordPortStates("sw1", up, iv, now) }, iv + time.Minute, nil},
		{"far end read too long ago", func(r *polledRegistry) { r.recordPortStates("sw1", up, iv, now) }, silentAfterSweeps*iv + time.Second, peerStateStale},
		{"far end stopped answering", func(r *polledRegistry) {
			r.recordPortStates("sw1", up, iv, now)
			r.recordUnreachable("sw1", iv, now)
		}, 0, peerStateUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPolledRegistry()
			tc.set(r)
			o := obs()
			annotatePeerState(&o, r, now.Add(tc.at))
			if got := o.Entities[0].Attributes[attrLinkPeerState]; got != tc.want {
				t.Errorf("%s = %v, want %v", attrLinkPeerState, got, tc.want)
			}
		})
	}
}
