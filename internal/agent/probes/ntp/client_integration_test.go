//go:build integration

package ntp

import (
	"testing"
	"time"
)

// TestQueryAgainstARealServer exercises the socket path that the unit tests
// cannot: dial, deadline, write, read. The decoder above is proved against
// captured bytes, so what is left here is the plumbing around it.
//
// It needs outbound UDP 123 and is therefore behind the integration tag, like
// the database probes. Run it with:
//
//	go test -tags integration ./internal/agent/probes/ntp/ -v
func TestQueryAgainstARealServer(t *testing.T) {
	const server = "time.google.com"

	s, err := query(server, 5*time.Second)
	if err != nil {
		t.Fatalf("querying %s: %v (outbound UDP 123 open?)", server, err)
	}

	if s.stratum < 1 || s.stratum > maxStratum {
		t.Errorf("stratum = %d, outside the valid range", s.stratum)
	}
	if s.roundTrip <= 0 || s.roundTrip > 2*time.Second {
		t.Errorf("roundTrip = %v, implausible for a public server", s.roundTrip)
	}
	// No assertion on the offset's magnitude: the whole point of the probe is
	// that the machine running the test may legitimately have a wrong clock.
	t.Logf("%s: offset=%v roundTrip=%v stratum=%d rootDispersion=%v",
		server, s.offset, s.roundTrip, s.stratum, s.rootDispersion)
}
