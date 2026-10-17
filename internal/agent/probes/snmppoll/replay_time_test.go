package snmppoll

import (
	"errors"
	"testing"
	"time"
)

// TestSilentGraceReplaysWithTheLastSweepTime checks the kept snapshot of a
// device whose sweeps fail is stamped with the time it was read.
func TestSilentGraceReplaysWithTheLastSweepTime(t *testing.T) {
	good := &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidSysNameBase: {{OID: oidSysName, Value: []byte("sw1")}},
	}}
	dead := &fakeClient{getErr: errors.New("request timeout"), walkErr: errors.New("request timeout"),
		walkRawErr: errors.New("request timeout")}
	const iv = time.Minute
	s := newEntitySource(&config{Target: "192.0.2.10", TopologyInterval: iv}, testLogger(t))
	t0 := time.Unix(1780272000, 0)
	now := t0
	s.now = func() time.Time { return now }

	s.maybeSweep(good, now)
	if obs, _ := s.Observe(); len(obs.Entities) == 0 || !obs.Entities[0].ObservedAt.IsZero() {
		t.Fatalf("a fresh sweep is observed now, got %+v", obs.Entities)
	}

	now = now.Add(iv)
	s.maybeSweep(dead, now)
	obs, ok := s.Observe()
	if !ok || len(obs.Entities) == 0 {
		t.Fatalf("within the grace the last view is replayed, got ok=%v", ok)
	}
	for _, e := range obs.Entities {
		if !e.ObservedAt.Equal(t0) {
			t.Errorf("%s replayed with ObservedAt %v, want the last sweep %v", e.Type, e.ObservedAt, t0)
		}
	}
}
