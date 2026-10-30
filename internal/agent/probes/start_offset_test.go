package probes

import (
	"testing"
	"time"
)

func TestStartOffsetIsDeterministicAndBelowTheBound(t *testing.T) {
	ids := []string{"a", "b", "cpu-main", "snmp_poll-edge-1", "0123456789abcdef"}
	for _, interval := range []time.Duration{time.Second, 10 * time.Second, time.Minute, time.Hour} {
		bound := min(interval, maxStartSpread)
		for _, id := range ids {
			first := StartOffset(id, interval)
			if again := StartOffset(id, interval); again != first {
				t.Errorf("StartOffset(%q, %s) changed between calls: %s then %s", id, interval, first, again)
			}
			if first < 0 || first >= bound {
				t.Errorf("StartOffset(%q, %s) = %s, want in [0, %s)", id, interval, first, bound)
			}
		}
	}
}

func TestStartOffsetSpreadsDistinctProbes(t *testing.T) {
	seen := map[time.Duration]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		seen[StartOffset(id, time.Minute)] = true
	}
	if len(seen) < 6 {
		t.Errorf("8 probe ids produced only %d distinct offsets", len(seen))
	}
}

func TestStartOffsetWithoutIntervalIsZero(t *testing.T) {
	if got := StartOffset("any", 0); got != 0 {
		t.Errorf("StartOffset with no interval = %s, want 0", got)
	}
	if got := StartOffset("any", -time.Second); got != 0 {
		t.Errorf("StartOffset with a negative interval = %s, want 0", got)
	}
}
