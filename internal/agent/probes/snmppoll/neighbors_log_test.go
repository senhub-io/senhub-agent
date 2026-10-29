package snmppoll

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestNeighbors_BoundIsLoggedOncePerCut checks the max_neighbors cut is
// announced once although every report rebuilds the observation, and again
// when the cut moves.
func TestNeighbors_BoundIsLoggedOncePerCut(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	s := resolveTestSource(t, false)
	s.moduleLogger = logger.NewModuleLogger(&zl, "test")
	s.cfg.MaxNeighbors = 1

	res := switchSweep()
	s.appendResolvesNeighbors(entity.Observation{}, res)
	s.appendResolvesNeighbors(entity.Observation{}, res)
	if n := strings.Count(buf.String(), "neighbor table bound"); n != 1 {
		t.Fatalf("two observations of one sweep logged the bound %d times, want 1: %s", n, buf.String())
	}

	res.fdb = append(res.fdb, fdbEntry{MAC: "00-50-56-EE-EE-03", IfIndex: 8})
	s.appendResolvesNeighbors(entity.Observation{}, res)
	if n := strings.Count(buf.String(), "neighbor table bound"); n != 2 {
		t.Fatalf("a moved cut logged the bound %d times in total, want 2", n)
	}
}
