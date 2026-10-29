package snmppoll

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// failingWalkClient fails the walks of the listed base OIDs, as a switch
// does when a large table read is lost over UDP.
type failingWalkClient struct {
	*fakeClient
	fail map[string]bool
}

func (f *failingWalkClient) WalkRaw(base string) ([]snmpRawBind, error) {
	if f.fail[base] {
		return nil, errors.New("request timeout")
	}
	return f.fakeClient.WalkRaw(base)
}

const (
	fdbRowOwn     = "1.0.80.86.170.187.1" // VLAN 1, 00-50-56-AA-BB-01
	fdbRowForeign = "1.0.80.86.204.221.2" // VLAN 1, 00-50-56-CC-DD-02
)

func switchWalks(fdbRows ...string) map[string][]snmpRawBind {
	walks := map[string][]snmpRawBind{
		oidSysNameBase:       {{OID: oidSysName, Value: []byte("sw1")}},
		ifName:               {{OID: ifName + ".8", Value: []byte("Ethernet8")}},
		dot1dBasePortIfIndex: {{OID: dot1dBasePortIfIndex + ".1", Value: 8}},
	}
	for _, row := range fdbRows {
		walks[dot1qTpFdbEntry] = append(walks[dot1qTpFdbEntry],
			snmpRawBind{OID: dot1qTpFdbEntry + ".2." + row, Value: 1},
			snmpRawBind{OID: dot1qTpFdbEntry + ".3." + row, Value: fdbStatusLearned},
		)
	}
	return walks
}

func observedNeighborMACs(t *testing.T, s *snmpEntitySource) []string {
	t.Helper()
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("observation not ready")
	}
	var macs []string
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkNeighbor {
			macs = append(macs, e.ID[idKeyNeighborMAC].(string))
		}
	}
	sort.Strings(macs)
	return macs
}

// TestNeighbors_FailedFDBWalkKeepsPreviousRows checks a lost FDB read does
// not retire the device's neighbours; only a successful read that no longer
// lists a MAC removes it.
func TestNeighbors_FailedFDBWalkKeepsPreviousRows(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.30", TopologyInterval: 10 * time.Minute}, testLogger(t))
	s.registry = newPolledRegistry()
	t0 := time.Now()

	s.maybeSweep(&fakeClient{walkRawResult: switchWalks(fdbRowOwn, fdbRowForeign)}, t0)
	want := []string{ownMAC, foreignMAC}
	sort.Strings(want)
	if got := observedNeighborMACs(t, s); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sweep 1 neighbours = %v, want %v", got, want)
	}

	failing := &failingWalkClient{
		fakeClient: &fakeClient{walkRawResult: switchWalks()},
		fail:       map[string]bool{dot1qTpFdbEntry: true, dot1dTpFdbEntry: true},
	}
	s.maybeSweep(failing, t0.Add(11*time.Minute))
	if got := observedNeighborMACs(t, s); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after a failed FDB walk neighbours = %v, want %v kept", got, want)
	}

	s.maybeSweep(&fakeClient{walkRawResult: switchWalks(fdbRowOwn)}, t0.Add(22*time.Minute))
	if got := observedNeighborMACs(t, s); strings.Join(got, ",") != ownMAC {
		t.Fatalf("after a successful read without %s neighbours = %v, want [%s]", foreignMAC, got, ownMAC)
	}
}

// TestCollectFDB_PartialFailureIsAFailure checks a lost q-bridge or
// basePort walk is reported as a failure rather than as an empty table.
func TestCollectFDB_PartialFailureIsAFailure(t *testing.T) {
	for _, oid := range []string{dot1qTpFdbEntry, dot1dBasePortIfIndex} {
		c := &failingWalkClient{fakeClient: &fakeClient{walkRawResult: switchWalks(fdbRowOwn)}, fail: map[string]bool{oid: true}}
		if _, err := collectFDB(c); err == nil {
			t.Errorf("walk of %s failed but collectFDB reported no error", oid)
		}
	}
	c := &failingWalkClient{fakeClient: &fakeClient{walkRawResult: switchWalks(fdbRowOwn)}, fail: map[string]bool{dot1dTpFdbEntry: true}}
	if rows, err := collectFDB(c); err != nil || len(rows) != 1 {
		t.Errorf("q-bridge read with legacy failing: rows=%v err=%v, want 1 row", rows, err)
	}
}

// TestNeighbors_DiscoveredDeviceKeepsRowsOnFailedWalk checks the discovery
// path keeps a discovered device's rows across a failed walk too.
func TestNeighbors_DiscoveredDeviceKeepsRowsOnFailedWalk(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.30"}, testLogger(t))
	fdb, _ := s.discoveredL2Rows("name:sw2", &fakeClient{walkRawResult: switchWalks(fdbRowOwn, fdbRowForeign)})
	if len(fdb) != 2 {
		t.Fatalf("first read rows = %v, want 2", fdb)
	}
	failing := &failingWalkClient{
		fakeClient: &fakeClient{walkRawResult: switchWalks()},
		fail:       map[string]bool{dot1qTpFdbEntry: true, dot1dTpFdbEntry: true, ipNetToPhysicalEntry: true, ipNetToMediaEntry: true},
	}
	if fdb, _ = s.discoveredL2Rows("name:sw2", failing); len(fdb) != 2 {
		t.Fatalf("after a failed walk rows = %v, want the 2 previous", fdb)
	}
	if fdb, _ = s.discoveredL2Rows("name:sw2", &fakeClient{walkRawResult: switchWalks(fdbRowOwn)}); len(fdb) != 1 {
		t.Fatalf("after a successful read rows = %v, want 1", fdb)
	}
	s.neighborCapChanged("name:sw2", 3)
	s.forgetDiscoveredL2("name:sw2")
	if !s.neighborCapChanged("name:sw2", 3) {
		t.Error("a forgotten device's cut must be announced again")
	}
	if fdb, _ = s.discoveredL2Rows("name:sw2", failing); len(fdb) != 0 {
		t.Fatalf("a forgotten device kept rows %v", fdb)
	}
}
