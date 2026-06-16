package hostdep

import (
	"errors"
	"testing"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

// fakeConns returns a connections function serving fixed rows, ignoring the
// kind argument.
func fakeConns(rows []gnet.ConnectionStat) func(string) ([]gnet.ConnectionStat, error) {
	return func(string) ([]gnet.ConnectionStat, error) { return rows, nil }
}

func conn(status, laddr string, lport uint32, raddr string, rport uint32, pid int32) gnet.ConnectionStat {
	return gnet.ConnectionStat{
		Status: status,
		Laddr:  gnet.Addr{IP: laddr, Port: lport},
		Raddr:  gnet.Addr{IP: raddr, Port: rport},
		Pid:    pid,
	}
}

func newTestSource(rows []gnet.ConnectionStat) *Source {
	s := New(func() string { return "h-1" })
	s.connections = fakeConns(rows)
	s.procName = func(pid int32) string {
		switch pid {
		case 100:
			return "nginx"
		case 0:
			return "" // owner unknown (unprivileged)
		default:
			return "app"
		}
	}
	return s
}

func relCount(obs entity.Observation, typ string) int {
	n := 0
	for _, r := range obs.Relations {
		if r.Type == typ {
			n++
		}
	}
	return n
}

func hasEndpoint(obs entity.Observation, addr, port string) bool {
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkEndpoint &&
			e.ID[idKeyServerAddress] == addr && e.ID[idKeyServerPort] == port {
			return true
		}
	}
	return false
}

func TestDebounce_EmitsOnlyAfterThreshold(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
	}
	s := newTestSource(rows) // threshold = 3 by default

	for i := 1; i < 3; i++ {
		obs, ok := s.Observe()
		if !ok {
			t.Fatalf("scrape %d: ok=false", i)
		}
		if len(obs.Relations) != 0 {
			t.Fatalf("scrape %d: edge emitted before threshold: %+v", i, obs)
		}
	}
	obs, ok := s.Observe() // 3rd scrape → durable
	if !ok {
		t.Fatal("3rd scrape ok=false")
	}
	if relCount(obs, relDependsOn) != 1 {
		t.Fatalf("want 1 depends_on at threshold, got %+v", obs)
	}
	if !hasEndpoint(obs, "10.0.0.9", "5432") {
		t.Errorf("network.endpoint entity must be emitted with the edge: %+v", obs.Entities)
	}
	// dependent service.instance present (foldRelationships needs the source).
	var foundSvc bool
	for _, e := range obs.Entities {
		if e.Type == entityTypeServiceInstance && e.ID[idKeyServiceInstanceID] == "nginx@h-1" {
			foundSvc = true
			if e.Attributes[attrServiceName] != "nginx" {
				t.Errorf("service.name = %v, want nginx", e.Attributes[attrServiceName])
			}
		}
	}
	if !foundSvc {
		t.Errorf("dependent service.instance nginx@h-1 not emitted: %+v", obs.Entities)
	}
}

func TestDependentAnchoredToHostWithRunsOn(t *testing.T) {
	// A minted dependent must hang off the host it runs on, not float with only
	// its depends_on edge: service.instance --runs_on--> host, host taken from
	// the foundation in the same cycle.
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
	}
	s := newTestSource(rows)
	var obs entity.Observation
	for i := 0; i < 3; i++ {
		obs, _ = s.Observe()
	}
	if got := relCount(obs, relRunsOn); got != 1 {
		t.Fatalf("want 1 runs_on anchoring the dependent, got %d: %+v", got, obs.Relations)
	}
	for _, r := range obs.Relations {
		if r.Type != relRunsOn {
			continue
		}
		if r.FromType != entityTypeServiceInstance || r.FromID[idKeyServiceInstanceID] != "nginx@h-1" {
			t.Errorf("runs_on must originate from the minted service.instance: %+v", r)
		}
		if r.ToType != entityTypeHost || r.ToID[idKeyHost] != "h-1" {
			t.Errorf("runs_on must target the host: %+v", r)
		}
	}
}

func TestVanishedConnectionResetsStreak(t *testing.T) {
	live := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100)}
	s := newTestSource(live)
	for i := 0; i < 3; i++ {
		s.Observe()
	}
	// Connection gone this scrape → edge leaves the snapshot, streak resets.
	s.connections = fakeConns(nil)
	obs, _ := s.Observe()
	if len(obs.Relations) != 0 {
		t.Fatalf("edge should drop once the connection vanishes: %+v", obs)
	}
	// Reappears: must debounce again from zero, not resurrect instantly.
	s.connections = fakeConns(live)
	obs, _ = s.Observe()
	if len(obs.Relations) != 0 {
		t.Errorf("reappearing connection must re-debounce, not resurrect: %+v", obs)
	}
}

func TestInboundAndUnnamedAndLoopbackSkipped(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusListen, "0.0.0.0", 443, "", 0, 100),                      // our listener on 443
		conn(statusEstablished, "10.0.0.5", 443, "10.0.0.2", 60000, 100),    // inbound (local port 443 is ours)
		conn(statusEstablished, "10.0.0.5", 52000, "10.0.0.9", 5432, 0),     // outbound but owner unknown
		conn(statusEstablished, "127.0.0.1", 53000, "127.0.0.1", 6379, 100), // loopback peer
	}
	s := newTestSource(rows)
	for i := 0; i < 3; i++ {
		obs, _ := s.Observe()
		if len(obs.Relations) != 0 {
			t.Fatalf("scrape %d: nothing should be emitted (all rows skipped): %+v", i, obs)
		}
	}
}

func TestAggregatePerPeerEndpoint(t *testing.T) {
	// Two sockets from the same service to the same peer → one edge.
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 5432, 100),
	}
	s := newTestSource(rows)
	var obs entity.Observation
	for i := 0; i < 3; i++ {
		obs, _ = s.Observe()
	}
	if got := relCount(obs, relDependsOn); got != 1 {
		t.Errorf("want 1 aggregated edge, got %d: %+v", got, obs.Relations)
	}
}

func TestTransientFailureKeepsStreak(t *testing.T) {
	live := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100)}
	s := newTestSource(live)
	s.Observe()
	s.Observe() // streak = 2
	// A failed read must not reset the streak nor delete edges.
	s.connections = func(string) ([]gnet.ConnectionStat, error) { return nil, errors.New("boom") }
	if _, ok := s.Observe(); ok {
		t.Fatal("read error must report ok=false")
	}
	// Recover: the next good scrape is the 3rd → durable (streak was preserved).
	s.connections = fakeConns(live)
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("recovered scrape ok=false")
	}
	if relCount(obs, relDependsOn) != 1 {
		t.Errorf("streak should have survived the transient failure: %+v", obs)
	}
}
