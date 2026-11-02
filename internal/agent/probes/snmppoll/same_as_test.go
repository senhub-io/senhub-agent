package snmppoll

import "testing"

func TestASerialKeyedDeviceAliasesItsChassisMAC(t *testing.T) {
	mac := []byte{0x70, 0x10, 0x6f, 0x0d, 0x0e, 0x0f}
	self := deviceIdentity{Serial: "FOC123", VendorPEN: "9", ChassisMAC: mac}
	selfID := resolveDeviceID(self)

	// The neighbour side builds its key from lldpRemChassisId through
	// resolveDeviceID; the alias must be that exact string.
	neighbourKey := resolveDeviceID(deviceIdentity{ChassisMAC: mac})
	if alias := chassisMACAlias(selfID, self); alias != neighbourKey {
		t.Fatalf("alias %q, neighbour key %q: the two ends must render the MAC alike", alias, neighbourKey)
	}

	obs := buildObservationAs(selfID, self, lldpTopology{}, nil, nil, nil, nil)
	var found int
	for _, r := range obs.Relations {
		if r.Type != "same_as" {
			continue
		}
		found++
		if r.FromType != entityTypeNetworkDevice || r.ToType != entityTypeNetworkDevice {
			t.Fatalf("same_as %s -> %s, want network.device at both ends", r.FromType, r.ToType)
		}
		if r.ToID[idKeyNetworkDevice] != neighbourKey || r.FromID[idKeyNetworkDevice] != selfID {
			t.Fatalf("same_as %v -> %v, want %s -> %s", r.FromID, r.ToID, selfID, neighbourKey)
		}
		if r.Attributes["confidence"] != 1.0 || r.Attributes["basis"] != "lldp_chassis" {
			t.Fatalf("attributes %v, want confidence 1.0 and basis lldp_chassis", r.Attributes)
		}
	}
	if found != 1 {
		t.Fatalf("same_as relations = %d, want 1", found)
	}
}

func TestNoAliasWhenAlreadyKeyedByMACOrWithoutOne(t *testing.T) {
	mac := []byte{0x70, 0x10, 0x6f, 0x0d, 0x0e, 0x0f}
	if a := chassisMACAlias(resolveDeviceID(deviceIdentity{ChassisMAC: mac}), deviceIdentity{ChassisMAC: mac}); a != "" {
		t.Fatalf("a mac-keyed device aliases itself: %q", a)
	}
	if a := chassisMACAlias("serial:9:X", deviceIdentity{ChassisMAC: []byte{0, 0, 0, 0, 0, 0}}); a != "" {
		t.Fatalf("an unusable MAC gave an alias: %q", a)
	}
}
