package entity

import (
	"testing"
	"time"
)

func stateTimes(evs []Event) []time.Time {
	var out []time.Time
	for _, ev := range evs {
		if ev.Kind == EntityState && ev.Entity != nil && ev.Entity.Type == "db" {
			out = append(out, ev.Time)
		}
	}
	return out
}

// TestReplayKeepsTheTimeOfTheObservationItRepeats checks a replayed state is
// stamped with the last good observation, and a fresh one with the tick.
func TestReplayKeepsTheTimeOfTheObservationItRepeats(t *testing.T) {
	resetSourcesForTest()
	defer resetSourcesForTest()

	src := &flakySource{obs: dbObservation("pg1"), ok: true}
	unregister := RegisterSource(src)
	defer unregister()

	var got []Event
	d := testDetector()
	tr := NewTracker(func(ev Event) { got = append(got, ev) }, 0)

	t0 := time.Unix(1000, 0).UTC()
	d.reconcile(tr, t0)

	src.ok = false
	got = nil
	d.reconcile(tr, t0.Add(time.Minute))
	if ts := stateTimes(got); len(ts) != 1 || !ts[0].Equal(t0) {
		t.Fatalf("replayed state time = %v, want the last good observation %v", ts, t0)
	}

	src.ok = true
	got = nil
	t2 := t0.Add(2 * time.Minute)
	d.reconcile(tr, t2)
	if ts := stateTimes(got); len(ts) != 1 || !ts[0].Equal(t2) {
		t.Fatalf("fresh state time = %v, want the tick %v", ts, t2)
	}
}

func TestMergeKeepsTheNewestObservationTime(t *testing.T) {
	old := time.Unix(1000, 0)
	newer := time.Unix(2000, 0)
	cases := []struct {
		name   string
		copies []Entity
		want   time.Time
	}{
		{"two replays keep the newer", []Entity{{ObservedAt: old}, {ObservedAt: newer}}, newer},
		{"a fresh read wins over a replay", []Entity{{ObservedAt: newer}, {}}, time.Time{}},
	}
	for _, tc := range cases {
		if got := newestObservedAt(tc.copies); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
