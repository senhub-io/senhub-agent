package snmppoll

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestChassisSerials_FrozenForm(t *testing.T) {
	fc := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass: {
			{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis},
			{OID: oidEntPhysicalClass + ".2", Value: entPhysicalClassChassis},
			{OID: oidEntPhysicalClass + ".3", Value: entPhysicalClassChassis},
			{OID: oidEntPhysicalClass + ".4", Value: 9}, // module: not a member
		},
		oidEntPhysicalSerialNum: {
			{OID: oidEntPhysicalSerialNum + ".1", Value: []byte(" b2 ")},
			{OID: oidEntPhysicalSerialNum + ".2", Value: []byte("A1")},
			{OID: oidEntPhysicalSerialNum + ".3", Value: []byte("B2")},
			{OID: oidEntPhysicalSerialNum + ".4", Value: []byte("MOD")},
		},
	}}
	di := readSelfIdentity(fc, "192.0.2.1", lldpLocal{})
	if di.Serial != "" {
		t.Errorf("a stack must not take a member serial as its id, got %q", di.Serial)
	}
	if got := selfAttrs(di)[attrChassisSerials]; got != "A1,B2,b2" {
		t.Errorf("%s = %q, want \"A1,B2,b2\" (trimmed, case kept, byte order, no space)", attrChassisSerials, got)
	}
	if _, ok := selfAttrs(deviceIdentity{SysName: "x"})[attrChassisSerials]; ok {
		t.Errorf("%s must be omitted when no chassis serial is read", attrChassisSerials)
	}
	if got := sortedUnique([]string{"b", "a", "b"}); strings.Join(got, ",") != "a,b" {
		t.Errorf("sortedUnique = %v", got)
	}
}

// stackClient answers a two-member stack: same serials and sysName, the
// chassis MAC of whichever member is master, two ports.
func stackClient(chassisMAC []byte) *fakeClient {
	return &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass: {
			{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis},
			{OID: oidEntPhysicalClass + ".2", Value: entPhysicalClassChassis},
		},
		oidEntPhysicalSerialNum: {
			{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("M1")},
			{OID: oidEntPhysicalSerialNum + ".2", Value: []byte("M2")},
		},
		oidSysObjectIDBase: {{OID: oidSysObjectID, Value: "1.3.6.1.4.1.25506.1.1"}},
		// The 11-byte form of a Comware engine id; a real one has 15 bytes,
		// with the suffix 00000001.
		oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}}},
		oidSysNameBase:      {{OID: oidSysName, Value: []byte("core-stack")}},
		lldpLocBase: {
			{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: subtypeMacAddress},
			{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: chassisMAC},
		},
		ifName: {
			{OID: ifName + ".1", Value: []byte("GE1/0/1")},
			{OID: ifName + ".2", Value: []byte("GE2/0/1")},
		},
	}}
}

type recordedDeletes struct{ events []entity.Event }

func (r *recordedDeletes) publish(ev entity.Event) { r.events = append(r.events, ev) }

// retiredOwnedBy returns, from the recorded deletes, the identities owned by
// deviceID, and fails on any delete that does not carry the rekeyed reason.
func (r *recordedDeletes) retiredOwnedBy(t *testing.T, deviceID string) []string {
	t.Helper()
	var out []string
	for _, ev := range r.events {
		if ev.Kind != entity.EntityDelete || ev.Entity == nil {
			continue
		}
		if ev.DeleteReason != entity.ReasonRekeyed {
			t.Errorf("delete of %v carries reason %q, want %q", ev.Entity.ID, ev.DeleteReason, entity.ReasonRekeyed)
		}
		if ev.Entity.ID[idKeyNetworkDevice] == deviceID {
			out = append(out, ev.Entity.Type+":"+entityIdentity(*ev.Entity))
		}
	}
	sort.Strings(out)
	return out
}

func newRekeyTestSource(t *testing.T) (*snmpEntitySource, *recordedDeletes) {
	t.Helper()
	rec := &recordedDeletes{}
	s := newEntitySource(&config{Target: "192.0.2.1", TopologyInterval: time.Minute}, testLogger(t))
	s.registry = newPolledRegistry()
	s.registry.warmupOverride = time.Nanosecond
	s.publish = rec.publish
	return s, rec
}

func TestRekey_MasterChangeRetiresTheWholeSubtree(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	macA := []byte{0x70, 0x10, 0x6f, 0x0a, 0x0a, 0x0a}
	macB := []byte{0x70, 0x10, 0x6f, 0x0b, 0x0b, 0x0b}
	t0 := time.Now()

	s.maybeSweep(stackClient(macA), t0)
	before, ok := s.Observe()
	if !ok {
		t.Fatal("first sweep produced no observation")
	}
	oldID := s.DeviceID()
	if oldID != "mac:70-10-6F-0A-0A-0A" {
		t.Fatalf("first id = %q", oldID)
	}
	owned := map[string]bool{}
	for _, e := range before.Entities {
		if e.ID[idKeyNetworkDevice] == oldID {
			owned[e.Type+":"+entityIdentity(e)] = true
		}
	}
	if len(owned) < 3 {
		t.Fatalf("fixture too small: %d entities owned by the device", len(owned))
	}
	rec.events = nil // the legacy engine: id was retired on the first sweep

	s.maybeSweep(stackClient(macB), t0.Add(2*time.Minute))
	if s.DeviceID() == oldID {
		t.Fatal("the id did not follow the new chassis MAC")
	}
	for i := 0; i < entity.RekeyAnnounceCycles+2; i++ {
		s.Observe()
	}
	got := rec.retiredOwnedBy(t, oldID)
	if len(got) != len(owned)*entity.RekeyAnnounceCycles {
		t.Fatalf("retired %d deletes for the old id, want %d (%d entities x %d cycles): %v",
			len(got), len(owned)*entity.RekeyAnnounceCycles, len(owned), entity.RekeyAnnounceCycles, got)
	}
	for _, k := range got {
		if !owned[k] {
			t.Errorf("retired %s, which the old id did not own", k)
		}
	}
	for _, ev := range rec.events {
		if ev.Entity.ID[idKeyNetworkDevice] == s.DeviceID() {
			t.Errorf("retired %v, which belongs to the new id", ev.Entity.ID)
		}
	}
}

func TestRekey_UpgradeRetiresTheEngineID(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	s.maybeSweep(stackClient([]byte{0x70, 0x10, 0x6f, 0x0a, 0x0a, 0x0a}), time.Now())
	s.Observe()
	legacy := "engine:800063a280dc680c9e372c"
	if got := rec.retiredOwnedBy(t, legacy); len(got) < 3 {
		t.Fatalf("the id the previous precedence gave was not retired with its subtree: %v", got)
	}
}

func TestRekey_DifferentDeviceBehindTheTargetIsNotARekey(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	t0 := time.Now()
	first := &fakeClient{walkRawResult: map[string][]snmpRawBind{oidSysNameBase: {{OID: oidSysName, Value: []byte("old-switch")}}}}
	second := &fakeClient{walkRawResult: map[string][]snmpRawBind{oidSysNameBase: {{OID: oidSysName, Value: []byte("new-switch")}}}}
	s.maybeSweep(first, t0)
	s.Observe()
	s.maybeSweep(second, t0.Add(2*time.Minute))
	s.Observe()
	for _, ev := range rec.events {
		t.Errorf("unexpected rekeyed delete of %v", ev.Entity.ID)
	}
}

// A copied configuration: the target now answers for another switch that
// carries the same engine id, under another name and chassis MAC. That is a
// different device, not a re-identification.
func TestRekey_SameEngineIDOtherDeviceIsNotARekey(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	engine := []byte{0x80, 0x00, 0x63, 0xa2, 0x80, 0xdc, 0x68, 0x0c, 0x9e, 0x37, 0x2c}
	mk := func(sysName string, mac []byte) *fakeClient {
		return &fakeClient{walkRawResult: map[string][]snmpRawBind{
			oidSnmpEngineIDBase: {{OID: oidSnmpEngineID, Value: engine}},
			oidSysNameBase:      {{OID: oidSysName, Value: []byte(sysName)}},
			lldpLocBase: {
				{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: subtypeMacAddress},
				{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: mac},
			},
			ifName: {{OID: ifName + ".1", Value: []byte("GE1/0/1")}},
		}}
	}
	t0 := time.Now()
	s.maybeSweep(mk("access-sw", []byte{0x70, 0x10, 0x6f, 0x0a, 0x0a, 0x0a}), t0)
	s.Observe()
	first := s.DeviceID()
	rec.events = nil // the legacy engine: id of the first device is retired on its first sweep
	s.maybeSweep(mk("core-sw", []byte{0x70, 0x10, 0x6f, 0x0b, 0x0b, 0x0b}), t0.Add(2*time.Minute))
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	for _, ev := range rec.events {
		if ev.Entity.ID[idKeyNetworkDevice] == first {
			t.Errorf("retired %v as rekeyed although another device answers the target", ev.Entity.ID)
		}
	}
}
