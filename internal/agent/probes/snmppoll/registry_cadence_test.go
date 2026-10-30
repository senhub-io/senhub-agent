package snmppoll

import (
	"testing"
	"time"
)

// TestASweepRecordsItsDeviceForTheEffectiveCadence drives a real sweep with a
// probe interval slower than the topology interval: the device is only swept
// every 15 min, so its registry entry must stay resolvable for freshFor(15 min),
// not for freshFor(2 min). A neighbour of a slow-swept device otherwise loses
// its canonical id between two sweeps, and every link to it flaps.
func TestASweepRecordsItsDeviceForTheEffectiveCadence(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.1", Interval: 15 * time.Minute, TopologyInterval: 2 * time.Minute}, testLogger(t))
	s.registry = newPolledRegistry()
	s.registry.warmupOverride = time.Nanosecond
	mac := []byte{0x70, 0x10, 0x6f, 0x0a, 0x0a, 0x0a}
	t0 := time.Now()

	s.maybeSweep(stackClient(mac), t0)
	id, ok := s.registry.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0)
	if !ok {
		t.Fatal("the swept device is not in the registry")
	}

	between := t0.Add(freshFor(2*time.Minute) + time.Second)
	if got, ok := s.registry.canonicalFor(deviceIdentity{ChassisMAC: mac}, between); !ok || got != id {
		t.Fatalf("entry gone at %v after the sweep, before the next sweep 15 min later", between.Sub(t0))
	}
	after := t0.Add(freshFor(15*time.Minute) + time.Second)
	if _, ok := s.registry.canonicalFor(deviceIdentity{ChassisMAC: mac}, after); ok {
		t.Fatalf("entry still trusted %v after the sweep, past freshFor of the effective cadence", after.Sub(t0))
	}
}

// TestAFasterRecorderDoesNotShortenAnEntry: a device recorded by its own slow
// sweep and by another instance's fast crawl keeps the longer freshness.
func TestAFasterRecorderDoesNotShortenAnEntry(t *testing.T) {
	r := newPolledRegistry()
	mac := []byte{0x70, 0x10, 0x6f, 0x0c, 0x0c, 0x0c}
	t0 := time.Now()
	r.recordPolled(deviceIdentity{ChassisMAC: mac}, "serial:9:X", 15*time.Minute, t0)
	r.recordPolled(deviceIdentity{ChassisMAC: mac}, "serial:9:X", time.Minute, t0.Add(time.Minute))
	if _, ok := r.canonicalFor(deviceIdentity{ChassisMAC: mac}, t0.Add(10*time.Minute)); !ok {
		t.Fatal("a 1 min recorder shortened the entry a 15 min sweep needs")
	}
}
