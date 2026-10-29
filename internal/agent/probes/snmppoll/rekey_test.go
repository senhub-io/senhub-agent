package snmppoll

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
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
	if got, _ := selfAttrs(di)[attrChassisSerials].([]string); strings.Join(got, "|") != "A1|B2|b2" {
		t.Errorf("%s = %#v, want []string{\"A1\", \"B2\", \"b2\"} (trimmed, case kept, byte order, deduplicated)", attrChassisSerials, selfAttrs(di)[attrChassisSerials])
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

// testClock drives both the sweeps and the source's own clock, so silent()
// sees the same time as maybeSweep.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newRekeyTestSourceWithClock(t *testing.T) (*snmpEntitySource, *recordedDeletes, *testClock) {
	t.Helper()
	s, rec := newRekeyTestSource(t)
	c := &testClock{t: time.Now()}
	s.now = c.now
	return s, rec, c
}

// sweepFor advances the clock by step and sweeps, n times, observing after
// each sweep as the entity pump would.
func sweepFor(s *snmpEntitySource, c *testClock, client snmpClient, n int, step time.Duration) {
	for i := 0; i < n; i++ {
		c.t = c.t.Add(step)
		s.maybeSweep(client, c.t)
		s.Observe()
	}
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

// failingWalks fails the walks of the listed base OIDs, the way a device
// that times out on one table does.
type failingWalks struct {
	*fakeClient
	fail map[string]bool
}

func (f failingWalks) WalkRaw(base string) ([]snmpRawBind, error) {
	if f.fail[base] {
		return nil, errors.New("request timeout")
	}
	return f.fakeClient.WalkRaw(base)
}

func singleChassisClient() *fakeClient {
	return &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidEntPhysicalClass:     {{OID: oidEntPhysicalClass + ".1", Value: entPhysicalClassChassis}},
		oidEntPhysicalSerialNum: {{OID: oidEntPhysicalSerialNum + ".1", Value: []byte("CN14HD90F4")}},
		oidSysObjectIDBase:      {{OID: oidSysObjectID, Value: "1.3.6.1.4.1.25506.1.1"}},
		oidSysNameBase:          {{OID: oidSysName, Value: []byte("access-sw")}},
		lldpLocBase: {
			{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: subtypeMacAddress},
			{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: []byte{0x70, 0x10, 0x6f, 0x0a, 0x0a, 0x0a}},
		},
		ifName: {{OID: ifName + ".1", Value: []byte("GE1/0/1")}},
	}}
}

func devicesIn(obs entity.Observation) []string {
	var out []string
	for _, e := range obs.Entities {
		if e.Type == entity.TypeNetworkDevice {
			out = append(out, fmt.Sprint(e.ID[idKeyNetworkDevice]))
		}
	}
	return out
}

// A timeout on ENTITY-MIB would drop a single-chassis switch from serial:
// to its chassis MAC. The established id is kept: the sweep is held like a
// failed one, nothing is published under the lower rung, nothing retired.
func TestRekey_IncompleteIdentityKeepsTheEstablishedID(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	full := singleChassisClient()
	degraded := failingWalks{full, map[string]bool{oidEntPhysicalClass: true, oidEntPhysicalSerialNum: true}}
	t0 := time.Now()

	s.maybeSweep(full, t0)
	s.Observe()
	const want = "serial:25506:CN14HD90F4"
	if s.DeviceID() != want {
		t.Fatalf("first id = %q", s.DeviceID())
	}
	s.maybeSweep(degraded, t0.Add(2*time.Minute))
	if s.DeviceID() != want {
		t.Fatalf("id after an incomplete identity read = %q, want %q kept", s.DeviceID(), want)
	}
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("the previous snapshot must still be observed")
	}
	if got := devicesIn(obs); len(got) != 1 || got[0] != want {
		t.Fatalf("devices published after the incomplete read = %v, want only %s", got, want)
	}
	s.maybeSweep(full, t0.Add(10*time.Minute))
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	if s.DeviceID() != want {
		t.Errorf("id after recovery = %q", s.DeviceID())
	}
	for _, ev := range rec.events {
		t.Errorf("incomplete read retired %v as rekeyed", ev.Entity.ID)
	}
}

// The rule protects an established id: a first sweep with an incomplete
// identity still publishes, or a device whose ENTITY-MIB an ACL refuses
// would never appear.
func TestRekey_FirstSweepWithIncompleteIdentityPublishes(t *testing.T) {
	s, _ := newRekeyTestSource(t)
	degraded := failingWalks{singleChassisClient(), map[string]bool{oidEntPhysicalClass: true, oidEntPhysicalSerialNum: true}}
	s.maybeSweep(degraded, time.Now())
	obs, ok := s.Observe()
	if !ok || len(devicesIn(obs)) != 1 || s.DeviceID() != "mac:70-10-6F-0A-0A-0A" {
		t.Fatalf("first incomplete sweep: ok=%v devices=%v id=%q, want the device published under its chassis MAC", ok, devicesIn(obs), s.DeviceID())
	}
}

// A device without ENTITY-MIB (the walks answer, with no row) has a complete
// identity: its id is not held.
func TestRekey_NoEntityMIBIsACompleteIdentity(t *testing.T) {
	c := singleChassisClient()
	delete(c.walkRawResult, oidEntPhysicalClass)
	delete(c.walkRawResult, oidEntPhysicalSerialNum)
	if di := readSelfIdentity(c, "192.0.2.1", lldpLocal{}); !di.identityComplete {
		t.Error("an empty ENTITY-MIB walk marked the identity incomplete")
	}
}

// Only entities keyed on the previous id are retired: a neighbour learned
// over LLDP is emitted under the new id as well.
func TestRekey_RetiresOnlyWhatThePreviousIDOwned(t *testing.T) {
	in := []entity.Entity{
		{Type: entity.TypeNetworkDevice, ID: map[string]any{idKeyNetworkDevice: "mac:A"}},
		{Type: "network.interface", ID: map[string]any{idKeyNetworkDevice: "mac:A", idKeyInterfaceName: "GE1"}},
		{Type: entity.TypeNetworkDevice, ID: map[string]any{idKeyNetworkDevice: "mac:NEIGHBOUR"}},
		{Type: "network.address", ID: map[string]any{"network.address": "192.0.2.7"}},
	}
	got := ownedBy(in, "mac:A")
	if len(got) != 2 {
		t.Fatalf("ownedBy kept %v, want the device and its interface only", got)
	}
}

// Two re-identifications inside one announcement window each keep their
// own three cycles.
func TestRekey_EachBatchAnnouncedThreeTimes(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	a := entity.Entity{Type: entity.TypeNetworkDevice, ID: map[string]any{idKeyNetworkDevice: "mac:A"}}
	b := entity.Entity{Type: entity.TypeNetworkDevice, ID: map[string]any{idKeyNetworkDevice: "mac:B"}}
	s.rekeyBatches = []rekeyBatch{{entities: []entity.Entity{a}, left: entity.RekeyAnnounceCycles}}
	s.announceRekey()
	s.rekeyBatches = append(s.rekeyBatches, rekeyBatch{entities: []entity.Entity{b}, left: entity.RekeyAnnounceCycles})
	for i := 0; i < 2*entity.RekeyAnnounceCycles; i++ {
		s.announceRekey()
	}
	count := map[string]int{}
	for _, ev := range rec.events {
		count[fmt.Sprint(ev.Entity.ID[idKeyNetworkDevice])]++
	}
	if count["mac:A"] != entity.RekeyAnnounceCycles || count["mac:B"] != entity.RekeyAnnounceCycles {
		t.Errorf("announcements per id = %v, want %d each", count, entity.RekeyAnnounceCycles)
	}
}

// A device first read with ENTITY-MIB failing is published under its
// chassis MAC; once ENTITY-MIB answers it moves to serial: and the mac: id
// it was published under is retired as rekeyed.
func TestRekey_FirstIncompleteIDRetiredWhenTheIdentityReadsInFull(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	full := singleChassisClient()
	degraded := failingWalks{full, map[string]bool{oidEntPhysicalClass: true, oidEntPhysicalSerialNum: true}}
	t0 := time.Now()
	s.maybeSweep(degraded, t0)
	s.Observe()
	first := s.DeviceID()
	rec.events = nil
	s.maybeSweep(full, t0.Add(2*time.Minute))
	s.Observe()
	if s.DeviceID() != "serial:25506:CN14HD90F4" {
		t.Fatalf("id once ENTITY-MIB answers = %q", s.DeviceID())
	}
	if got := rec.retiredOwnedBy(t, first); len(got) == 0 {
		t.Errorf("the %s id published by the first sweep was not retired as rekeyed", first)
	}
}

// An error-status on ENTITY-MIB (genErr) reaches the probe as an empty
// table, not a failed walk. A device that had chassis serials and suddenly
// has none keeps its established id, and nothing is retired as rekeyed.
func TestRekey_VanishedChassisSerialsKeepTheEstablishedID(t *testing.T) {
	s, rec := newRekeyTestSource(t)
	full := singleChassisClient()
	genErr := singleChassisClient()
	delete(genErr.walkRawResult, oidEntPhysicalClass)
	delete(genErr.walkRawResult, oidEntPhysicalSerialNum)
	t0 := time.Now()

	s.maybeSweep(full, t0)
	s.Observe()
	const want = "serial:25506:CN14HD90F4"
	if s.DeviceID() != want {
		t.Fatalf("first id = %q", s.DeviceID())
	}
	s.maybeSweep(genErr, t0.Add(2*time.Minute))
	obs, _ := s.Observe()
	if s.DeviceID() != want {
		t.Fatalf("id while ENTITY-MIB reads empty = %q, want %q kept", s.DeviceID(), want)
	}
	if got := devicesIn(obs); len(got) != 1 || got[0] != want {
		t.Fatalf("devices published = %v, want only %s", got, want)
	}
	s.maybeSweep(full, t0.Add(10*time.Minute))
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	for _, ev := range rec.events {
		t.Errorf("retired %v as rekeyed across an ENTITY-MIB error", ev.Entity.ID)
	}
}

// The hold on vanished chassis serials ends: past the silence threshold the
// device is resolved again from what it answers, instead of staying held,
// and absent, until the agent restarts.
func TestRekey_VanishedChassisSerialsGuardEnds(t *testing.T) {
	s, _, c := newRekeyTestSourceWithClock(t)
	full := singleChassisClient()
	genErr := singleChassisClient()
	delete(genErr.walkRawResult, oidEntPhysicalClass)
	delete(genErr.walkRawResult, oidEntPhysicalSerialNum)

	sweepFor(s, c, full, 1, time.Minute)
	if s.DeviceID() != "serial:25506:CN14HD90F4" {
		t.Fatalf("first id = %q", s.DeviceID())
	}
	sweepFor(s, c, genErr, 20, 10*time.Minute)
	if s.DeviceID() != "mac:70-10-6F-0A-0A-0A" {
		t.Fatalf("id after a lasting loss of the serials = %q, want the device resolved again", s.DeviceID())
	}
	if obs, ok := s.Observe(); !ok || len(devicesIn(obs)) != 1 {
		t.Fatalf("after the hold ended the device must be published again: ok=%v %v", ok, devicesIn(obs))
	}
}

// The hold on an identity walk that fails (ent#816) ends the same way, and a
// device that reads in full again returns under its full id.
func TestRekey_FailingIdentityWalkHoldEndsAndRecovers(t *testing.T) {
	s, rec, c := newRekeyTestSourceWithClock(t)
	full := singleChassisClient()
	failing := failingWalks{singleChassisClient(), map[string]bool{oidEntPhysicalClass: true, oidEntPhysicalSerialNum: true}}

	sweepFor(s, c, full, 1, time.Minute)
	sweepFor(s, c, failing, 2, time.Minute)
	if s.DeviceID() != "serial:25506:CN14HD90F4" {
		t.Fatalf("id during a short failure = %q, want it held", s.DeviceID())
	}
	sweepFor(s, c, failing, 20, 10*time.Minute)
	if s.DeviceID() != "mac:70-10-6F-0A-0A-0A" {
		t.Fatalf("id after a lasting failure = %q, want the device resolved again", s.DeviceID())
	}
	rec.events = nil
	sweepFor(s, c, full, 3, 10*time.Minute)
	if s.DeviceID() != "serial:25506:CN14HD90F4" {
		t.Fatalf("id once the walks answer again = %q", s.DeviceID())
	}
	retired := false
	for _, ev := range rec.events {
		if ev.DeleteReason == entity.ReasonRekeyed && ev.Entity.ID[idKeyNetworkDevice] == "mac:70-10-6F-0A-0A-0A" {
			retired = true
		}
	}
	if !retired {
		t.Error("the id the device was resolved to during the failure was not retired as rekeyed on its return")
	}
	if obs, ok := s.Observe(); !ok || len(devicesIn(obs)) != 1 || devicesIn(obs)[0] != "serial:25506:CN14HD90F4" {
		t.Fatalf("after recovery: ok=%v %v", ok, devicesIn(obs))
	}
}

func TestRekey_VanishedSerialsReturnRetiresTheInterimIDAsRekeyed(t *testing.T) {
	s, rec, c := newRekeyTestSourceWithClock(t)
	full := singleChassisClient()
	genErr := singleChassisClient()
	delete(genErr.walkRawResult, oidEntPhysicalClass)
	delete(genErr.walkRawResult, oidEntPhysicalSerialNum)

	sweepFor(s, c, full, 1, time.Minute)
	sweepFor(s, c, genErr, 20, 10*time.Minute)
	interim := s.DeviceID()
	if interim == "serial:25506:CN14HD90F4" {
		t.Fatalf("hold did not end")
	}
	rec.events = nil
	sweepFor(s, c, full, 3, 10*time.Minute)
	got := false
	for _, ev := range rec.events {
		if ev.DeleteReason == entity.ReasonRekeyed && ev.Entity.ID[idKeyNetworkDevice] == interim {
			got = true
		}
	}
	if !got {
		t.Errorf("interim id %s not retired as rekeyed on return; events: %d", interim, len(rec.events))
	}
}

// A hold that ends while the reads keep failing logs its end once per
// outage, not at every retry past the threshold.
func TestRekey_HoldReleaseLoggedOncePerOutage(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	s, _, c := newRekeyTestSourceWithClock(t)
	s.moduleLogger = logger.NewModuleLogger(&zl, "test")
	full := singleChassisClient()
	failing := failingWalks{singleChassisClient(), map[string]bool{oidEntPhysicalClass: true, oidEntPhysicalSerialNum: true, oidSysNameBase: true, lldpLocBase: true}}
	sweepFor(s, c, full, 1, time.Minute)
	sweepFor(s, c, failing, 30, 10*time.Minute)
	if n := strings.Count(buf.String(), "still incomplete after the silence threshold"); n != 1 {
		t.Errorf("end of hold logged %d times during one outage, want once", n)
	}
}
