package snmppoll

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// After a restart the registry is empty and fills in the order the probe
// instances sweep. A device swept before its neighbour could not resolve
// it, and the link was left out of what it published for a whole topology
// interval; the consumer read the omission as the link being gone (#991).
// Measured on a lab fleet: every link pointing down the hierarchy vanished
// at the restart and came back 300 s later.

var (
	coldMACA = []byte{0x02, 0, 0, 0, 0, 0xa1}
	coldMACB = []byte{0x02, 0, 0, 0, 0, 0xb2}
)

func coldSource(t *testing.T, reg *polledRegistry, clock *time.Time, res sweepResult) *snmpEntitySource {
	t.Helper()
	s := newEntitySource(&config{Target: "192.0.2.1"}, testLogger(t))
	s.registry = reg
	s.now = func() time.Time { return *clock }
	s.cache, s.swept = res, true
	return s
}

// coreSweep is a device whose LLDP table sees coldMACB on its eth1.
func coreSweep() sweepResult {
	self := deviceIdentity{EngineID: []byte{0xa1}, ChassisMAC: coldMACA, SysName: "core1"}
	return sweepResult{
		self: self,
		topo: lldpTopology{Neighbors: []lldpNeighbor{{
			LocalPortNum:     "1",
			ChassisIdSubtype: subtypeMacAddress,
			ChassisId:        coldMACB,
			PortIdSubtype:    portSubtypeIfName,
			PortId:           []byte("eth2"),
			SysName:          "dist1",
		}}},
		ifaces:   []ifaceRow{{Index: "1", Name: "eth1"}},
		deviceID: resolveDeviceID(self),
	}
}

func linksTo(obs entity.Observation, deviceID string) int {
	n := 0
	for _, r := range obs.Relations {
		if r.Type == relConnectedTo && r.ToID[idKeyNetworkDevice] == deviceID {
			n++
		}
	}
	return n
}

func TestAColdRegistryHoldsBackALinkUntilTheNeighbourIsPolled(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 6, 2, 46, 0, time.UTC)
	clock := t0
	reg := &polledRegistry{byMAC: map[string]registryEntry{}, started: t0, warmupOverride: 2 * time.Minute}
	core := coldSource(t, reg, &clock, coreSweep())
	reg.recordPolled(core.cache.self, core.cache.deviceID, t0)

	// The neighbour has not been polled yet: publishing now would retract
	// the link, so the source holds its observation back.
	if obs, ok := core.Observe(); ok {
		t.Fatalf("published before the neighbour was polled: %d links", linksTo(obs, "engine:b2"))
	}

	// The neighbour's own sweep registers it; the core links to it at the
	// next read, without a sweep of its own.
	clock = t0.Add(20 * time.Second)
	reg.recordPolled(deviceIdentity{EngineID: []byte{0xb2}, ChassisMAC: coldMACB}, "engine:b2", clock)
	obs, ok := core.Observe()
	if !ok {
		t.Fatal("held back after the neighbour was polled")
	}
	if linksTo(obs, "engine:b2") != 1 {
		t.Fatalf("relations = %+v, want connected_to the neighbour's canonical port", obs.Relations)
	}
}

// A neighbour no instance polls (a device outside the configured fleet)
// holds nothing back once the warm-up is over.
func TestANeighbourNobodyPollsHoldsNothingBackAfterWarmUp(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 6, 2, 46, 0, time.UTC)
	clock := t0.Add(2 * time.Minute)
	reg := &polledRegistry{byMAC: map[string]registryEntry{}, started: t0, warmupOverride: 2 * time.Minute}
	core := coldSource(t, reg, &clock, coreSweep())

	obs, ok := core.Observe()
	if !ok {
		t.Fatal("held back after the warm-up")
	}
	if linksTo(obs, "engine:b2") != 0 {
		t.Fatalf("linked to a device nobody polled: %+v", obs.Relations)
	}
}

// The hold-back window is a third of the liveness interval the detector
// announces (entity.ReportInterval), so the two cannot drift apart; with no
// detector announcing one there is nothing to hold back for.
func TestTheHoldBackWindowIsAThirdOfTheAnnouncedInterval(t *testing.T) {
	reg := newPolledRegistry()
	if got, want := reg.warmup(), entity.ReportInterval()/3; got != want {
		t.Errorf("window %v, want a third of the announced interval %v", got, entity.ReportInterval())
	}
}
