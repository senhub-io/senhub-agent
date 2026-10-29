package snmppoll

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestResolveDeviceID_MACBeforeEngine(t *testing.T) {
	usable := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	allZero := []byte{0, 0, 0, 0, 0, 0}
	allFF := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	multicast := []byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0x01}
	shortMAC := []byte{0x00, 0x11, 0x22, 0x33}

	tests := []struct {
		name string
		id   deviceIdentity
		want string
	}{
		{
			name: "engine plus usable MAC chooses MAC",
			id: deviceIdentity{
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: usable,
				SysName:    "x",
			},
			want: "mac:00-11-22-33-44-55",
		},
		{
			name: "engine plus all-zero MAC falls to engine",
			id: deviceIdentity{
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: allZero,
				SysName:    "x",
			},
			want: "engine:800102",
		},
		{
			name: "engine plus all-ff MAC falls to engine",
			id: deviceIdentity{
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: allFF,
				SysName:    "x",
			},
			want: "engine:800102",
		},
		{
			name: "engine plus multicast MAC falls to engine",
			id: deviceIdentity{
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: multicast,
				SysName:    "x",
			},
			want: "engine:800102",
		},
		{
			name: "engine plus short MAC falls to engine",
			id: deviceIdentity{
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: shortMAC,
				SysName:    "x",
			},
			want: "engine:800102",
		},
		{
			name: "serial with PEN beats MAC",
			id: deviceIdentity{
				Serial:     "abc",
				VendorPEN:  "9",
				EngineID:   []byte{0x80, 0x01, 0x02},
				ChassisMAC: usable,
			},
			want: "serial:9:abc",
		},
		{
			name: "MAC only",
			id: deviceIdentity{
				ChassisMAC: usable,
			},
			want: "mac:00-11-22-33-44-55",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDeviceID(tt.id)
			if got != tt.want {
				t.Fatalf("resolveDeviceID() = %q, want %q", got, tt.want)
			}
		})
	}

	// same engine, different usable MACs → two ids
	id1 := deviceIdentity{EngineID: []byte{0x80, 0x01, 0x02}, ChassisMAC: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}}
	id2 := deviceIdentity{EngineID: []byte{0x80, 0x01, 0x02}, ChassisMAC: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x66}}
	r1, r2 := resolveDeviceID(id1), resolveDeviceID(id2)
	if r1 == r2 {
		t.Fatalf("same engine with different usable chassis MACs must resolve differently, got %q for both", r1)
	}
}

func TestClaimID(t *testing.T) {
	now := time.Now()
	fresh := now
	stale := now.Add(-polledRegistryTTL - time.Second)

	usableMAC := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	otherMAC := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x66}

	t.Run("different sysName collides and warns once", func(t *testing.T) {
		r := newPolledRegistry()
		id := "engine:abc"
		self1 := deviceIdentity{SysName: "switch-a", ChassisMAC: usableMAC}
		self2 := deviceIdentity{SysName: "switch-b", ChassisMAC: usableMAC}

		_, coll, warn := r.claimID(id, "10.0.0.1", self1, fresh)
		if coll || warn {
			t.Fatalf("first claim should not collide, coll=%v warn=%v", coll, warn)
		}
		other, coll, warn := r.claimID(id, "10.0.0.2", self2, fresh)
		if !coll || !warn {
			t.Fatalf("second claim should collide and first warn: coll=%v warn=%v", coll, warn)
		}
		if other.target != "10.0.0.1" {
			t.Fatalf("other.target = %q, want 10.0.0.1", other.target)
		}
		// repeat should collide but not warn again
		_, coll, warn = r.claimID(id, "10.0.0.2", self2, fresh)
		if !coll || warn {
			t.Fatalf("repeat collision coll=%v warn=%v, want coll=true warn=false", coll, warn)
		}
	})

	t.Run("same sysName and MAC => same device, no collision", func(t *testing.T) {
		r := newPolledRegistry()
		id := "mac:00-11-22-33-44-55"
		self1 := deviceIdentity{SysName: "switch", ChassisMAC: usableMAC}
		self2 := deviceIdentity{SysName: "switch", ChassisMAC: usableMAC}

		_, coll, warn := r.claimID(id, "10.0.0.1", self1, fresh)
		if coll || warn {
			t.Fatalf("first claim coll=%v warn=%v", coll, warn)
		}
		_, coll, warn = r.claimID(id, "10.0.0.2", self2, fresh)
		if coll || warn {
			t.Fatalf("same device via two addresses should not collide, coll=%v warn=%v", coll, warn)
		}
	})

	t.Run("same target repeated is not collision", func(t *testing.T) {
		r := newPolledRegistry()
		self := deviceIdentity{SysName: "switch", ChassisMAC: usableMAC}
		_, coll, _ := r.claimID("id", "10.0.0.1", self, fresh)
		if coll {
			t.Fatal("first claim should not collide")
		}
		_, coll, _ = r.claimID("id", "10.0.0.1", self, fresh)
		if coll {
			t.Fatal("same target repeated should not collide")
		}
	})

	t.Run("different MAC but one sysName empty collides", func(t *testing.T) {
		r := newPolledRegistry()
		self1 := deviceIdentity{SysName: "", ChassisMAC: usableMAC}
		self2 := deviceIdentity{SysName: "", ChassisMAC: otherMAC}
		_, coll, _ := r.claimID("id", "a", self1, fresh)
		if coll {
			t.Fatal("first claim should not collide")
		}
		_, coll, _ = r.claimID("id", "b", self2, fresh)
		if !coll {
			t.Fatal("different chassis MACs should collide even with empty sysName")
		}
	})

	t.Run("stale first claim replaced, no collision", func(t *testing.T) {
		r := newPolledRegistry()
		self1 := deviceIdentity{SysName: "old", ChassisMAC: usableMAC}
		self2 := deviceIdentity{SysName: "new", ChassisMAC: otherMAC}
		_, coll, _ := r.claimID("id", "a", self1, stale)
		if coll {
			t.Fatal("stale first claim should be accepted")
		}
		_, coll, _ = r.claimID("id", "b", self2, fresh)
		if coll {
			t.Fatal("stale claim should be replaced, not collide")
		}
		// should now be claimed by b
		if c, ok := r.byID["id"]; !ok || c.target != "b" {
			t.Fatalf("byID[%q] = %+v, want target b", "id", c)
		}
	})

	t.Run("empty id is no-op", func(t *testing.T) {
		r := newPolledRegistry()
		_, coll, warn := r.claimID("", "a", deviceIdentity{}, fresh)
		if coll || warn {
			t.Fatal("empty id should not collide or warn")
		}
	})
}

// TestSweep_SharedEngineIDTwoDevices polls two switches that carry the same
// snmpEngineID through one shared registry: with their LLDP chassis MACs
// they get two ids; without LLDP they still share the engine id, and the
// second target counts the collision.
func TestSweep_SharedEngineIDTwoDevices(t *testing.T) {
	engine := []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}
	mk := func(sysName string, chassisMAC []byte) *fakeClient {
		walks := map[string][]snmpRawBind{
			oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: engine}},
			oidSysNameBase:      {{OID: oidSysName, Value: []byte(sysName)}},
		}
		if chassisMAC != nil {
			walks[lldpLocBase] = []snmpRawBind{
				{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: subtypeMacAddress},
				{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: chassisMAC},
			}
		}
		return &fakeClient{walkRawResult: walks}
	}
	sweepBoth := func(macA, macB []byte) (a, b *snmpEntitySource) {
		reg := newPolledRegistry()
		a = newEntitySource(&config{Target: "192.0.2.1"}, testLogger(t))
		b = newEntitySource(&config{Target: "192.0.2.2"}, testLogger(t))
		a.registry, b.registry = reg, reg
		now := time.Now()
		a.maybeSweep(mk("access-sw", macA), now)
		b.maybeSweep(mk("core-sw", macB), now)
		return a, b
	}

	a, b := sweepBoth([]byte{0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}, []byte{0x70, 0x10, 0x6f, 0x01, 0x02, 0x03})
	if a.DeviceID() == b.DeviceID() {
		t.Fatalf("two devices with distinct chassis MACs share id %q", a.DeviceID())
	}
	if a.DeviceID() != "mac:DC-68-0C-9E-37-2C" || b.DeviceID() != "mac:70-10-6F-01-02-03" {
		t.Errorf("ids = %q, %q, want the chassis MACs", a.DeviceID(), b.DeviceID())
	}
	if n := b.IdentityCollisions(); n != 0 {
		t.Errorf("collisions = %d with distinct ids, want 0", n)
	}

	a, b = sweepBoth(nil, nil)
	if a.DeviceID() != b.DeviceID() {
		t.Fatalf("without LLDP both should fall to the shared engine id, got %q and %q", a.DeviceID(), b.DeviceID())
	}
	if n := a.IdentityCollisions(); n != 0 {
		t.Errorf("first claimer counted %d collisions, want 0", n)
	}
	if n := b.IdentityCollisions(); n != 1 {
		t.Errorf("second target counted %d collisions, want 1", n)
	}
}

func TestEngineIDMAC(t *testing.T) {
	cases := []struct {
		name   string
		engine []byte
		want   string
	}{
		{"comware enterprise format", []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}, "DC-68-0C-9E-37-2C"},
		{"rfc 3411 mac format", []byte{0x80, 0x00, 0x00, 0x09, 0x03, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, "00-11-22-33-44-55"},
		{"enterprise format of another vendor", []byte{0x80, 0x00, 0x00, 0x09, 0x80, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, ""},
		{"text format", []byte{0x80, 0x00, 0x1f, 0x88, 0x04, 'r', 'o', 'u', 't', 'e', 'r'}, ""},
		{"pre-rfc 3411 engine id", []byte{0x00, 0x00, 0x00, 0x09, 0x03, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, ""},
		{"too short", []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := engineIDMAC(c.engine)
			if c.want == "" {
				if ok {
					t.Fatalf("engineIDMAC = %x, want none", got)
				}
				return
			}
			if !ok || macHex(got) != c.want {
				t.Fatalf("engineIDMAC = %x,%v, want %s", got, ok, c.want)
			}
		})
	}
}

func TestCheckEngineIDMAC(t *testing.T) {
	engine := []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}
	s := newEntitySource(&config{Target: "192.0.2.1"}, testLogger(t))

	s.checkEngineIDMAC(deviceIdentity{EngineID: engine, ChassisMAC: []byte{0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}})
	if s.EngineIDMACMismatch() {
		t.Error("engine id embedding the chassis MAC flagged as a mismatch")
	}
	s.checkEngineIDMAC(deviceIdentity{EngineID: engine, ChassisMAC: []byte{0x70, 0x10, 0x6f, 0x01, 0x02, 0x03}})
	if !s.EngineIDMACMismatch() {
		t.Error("engine id embedding another MAC not flagged")
	}
	if got, _ := s.engineMACWarned.Load().(string); got != "DC-68-0C-9E-37-2C/70-10-6F-01-02-03" {
		t.Errorf("warned pair = %q", got)
	}
	s.checkEngineIDMAC(deviceIdentity{EngineID: engine})
	if s.EngineIDMACMismatch() {
		t.Error("no chassis MAC to compare with, yet flagged")
	}
}

func TestSelfAttrs_RawIdentityParts(t *testing.T) {
	a := selfAttrs(deviceIdentity{
		Serial:     " CN14HD90F4 ",
		EngineID:   []byte{0x80, 0x00, 0x63, 0xa2},
		ChassisMAC: []byte{0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c},
		SysName:    "sw1",
	})
	if a[attrHwSerial] != "CN14HD90F4" || a[attrEngineID] != "800063a2" || a[attrChassisMAC] != "DC-68-0C-9E-37-2C" {
		t.Errorf("raw identity parts = serial:%v engine:%v mac:%v", a[attrHwSerial], a[attrEngineID], a[attrChassisMAC])
	}
	if _, ok := selfAttrs(deviceIdentity{ChassisMAC: []byte{0, 0, 0, 0, 0, 0}})[attrChassisMAC]; ok {
		t.Error("an unusable chassis MAC must not be published")
	}
}
