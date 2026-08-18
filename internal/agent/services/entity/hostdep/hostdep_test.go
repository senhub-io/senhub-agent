package hostdep

import (
	"errors"
	"net"
	"testing"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

// TestExcludeCIDR_DropsExcludedPeer pins the #213 privacy filter: a dependency
// flow whose peer falls in an excluded CIDR is dropped; others pass.
func TestExcludeCIDR_DropsExcludedPeer(t *testing.T) {
	_, ipnet, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	s := New(func() string { return "h-1" }, 1, []*net.IPNet{ipnet})
	s.procName = func(int32) string { return "curl" }
	s.connections = fakeConns([]gnet.ConnectionStat{
		conn(statusEstablished, "192.0.2.1", 40000, "10.1.2.3", 443, 5),    // peer in 10/8 → excluded
		conn(statusEstablished, "192.0.2.1", 40001, "203.0.113.9", 443, 5), // peer outside → kept
	})

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	if hasEndpoint(obs, "10.1.2.3", "443") {
		t.Errorf("excluded peer 10.1.2.3 must be dropped: %+v", obs.Entities)
	}
	if !hasEndpoint(obs, "203.0.113.9", "443") {
		t.Errorf("non-excluded peer 203.0.113.9 must be emitted: %+v", obs.Entities)
	}
}

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
	s := New(func() string { return "h-1" }, defaultThreshold, nil)
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

func TestNew_ConfigurableThreshold(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
	}
	// threshold = 1 → durable on the first scrape (no debounce wait).
	s := New(func() string { return "h-1" }, 1, nil)
	s.connections = fakeConns(rows)
	s.procName = func(int32) string { return "nginx" }
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("first scrape ok=false")
	}
	if relCount(obs, relDependsOn) != 1 {
		t.Errorf("threshold=1 must emit on the first scrape, got %+v", obs.Relations)
	}

	// non-positive → falls back to defaultThreshold.
	if got := New(func() string { return "h" }, 0, nil).threshold; got != defaultThreshold {
		t.Errorf("threshold 0 → %d, want fallback %d", got, defaultThreshold)
	}
	if got := New(func() string { return "h" }, -5, nil).threshold; got != defaultThreshold {
		t.Errorf("negative threshold → %d, want fallback %d", got, defaultThreshold)
	}
}

func TestAgentSelfDependencyUsesFoundationIdentity(t *testing.T) {
	const agentKey = "a5f503ab-key"
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 40000, "10.0.0.9", 443, 4242), // agent's own pid
		conn(statusEstablished, "10.0.0.5", 41000, "10.0.0.8", 6379, 100), // nginx
	}
	s := newTestSource(rows)
	s.selfPID = func() int32 { return 4242 }
	s.agentID = func() string { return agentKey }
	var obs entity.Observation
	for i := 0; i < 3; i++ {
		obs, _ = s.Observe()
	}

	// Agent's own dependency attaches to the foundation service.instance (the
	// agent key): a depends_on with that From, but NO duplicate node and NO
	// runs_on (the foundation owns those) — #494.
	var agentDepends bool
	for _, r := range obs.Relations {
		if r.Type == relDependsOn && r.FromID[idKeyServiceInstanceID] == agentKey {
			agentDepends = true
		}
		if r.Type == relRunsOn && r.FromID[idKeyServiceInstanceID] == agentKey {
			t.Errorf("must not emit runs_on for the foundation-owned agent identity: %+v", r)
		}
	}
	if !agentDepends {
		t.Errorf("agent's own depends_on must use the foundation key %q: %+v", agentKey, obs.Relations)
	}
	for _, e := range obs.Entities {
		if e.Type == entityTypeServiceInstance && e.ID[idKeyServiceInstanceID] == agentKey {
			t.Errorf("must not emit a duplicate service.instance entity for the foundation key: %+v", e)
		}
	}

	// A non-agent process still mints <exe>@host with its own node + runs_on.
	var nginxNode, nginxRunsOn bool
	for _, e := range obs.Entities {
		if e.Type == entityTypeServiceInstance && e.ID[idKeyServiceInstanceID] == "nginx@h-1" {
			nginxNode = true
		}
	}
	for _, r := range obs.Relations {
		if r.Type == relRunsOn && r.FromID[idKeyServiceInstanceID] == "nginx@h-1" {
			nginxRunsOn = true
		}
	}
	if !nginxNode || !nginxRunsOn {
		t.Errorf("non-agent process must still mint <exe>@host with entity+runs_on (node=%v runs_on=%v)", nginxNode, nginxRunsOn)
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

// TestVanishedConnectionSurvivesTheMissTolerance pins the symmetric debounce
// (#808). This test previously asserted the opposite — that ONE missed scrape
// drops the edge — which is the defect: an edge that took three scrapes to
// assert was retracted by a single missed observation, and since depends_on is
// retired by absence at the consumer, that reached the graph as a removed edge
// and back again. A long-lived connection the socket table happens to miss once
// is not a dependency that ended.
func TestVanishedConnectionSurvivesTheMissTolerance(t *testing.T) {
	live := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100)}
	s := newTestSource(live)
	for i := 0; i < 3; i++ {
		s.Observe()
	}
	// Missed scrapes inside the tolerance: the edge stays on the wire.
	s.connections = fakeConns(nil)
	for i := 1; i <= 3; i++ {
		obs, ok := s.Observe()
		if !ok {
			t.Fatalf("miss %d: observation must stay trustworthy", i)
		}
		if relCount(obs, relDependsOn) != 1 {
			t.Fatalf("miss %d of 3: edge must survive the tolerance, got %+v", i, obs.Relations)
		}
	}
	// Gone for as long as it took to appear: given up.
	obs, _ := s.Observe()
	if len(obs.Relations) != 0 {
		t.Fatalf("edge must drop once the miss tolerance is exhausted: %+v", obs)
	}
	// Reappears: must debounce again from zero, not resurrect instantly.
	s.connections = fakeConns(live)
	obs, _ = s.Observe()
	if len(obs.Relations) != 0 {
		t.Errorf("reappearing connection must re-debounce, not resurrect: %+v", obs)
	}
}

func TestInboundAndUnnamedSkipped(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusListen, "0.0.0.0", 443, "", 0, 100),                   // our listener on 443
		conn(statusEstablished, "10.0.0.5", 443, "10.0.0.2", 60000, 100), // inbound (local port 443 is ours)
		conn(statusEstablished, "10.0.0.5", 52000, "10.0.0.9", 5432, 0),  // outbound but owner unknown
	}
	s := newTestSource(rows)
	for i := 0; i < 3; i++ {
		obs, _ := s.Observe()
		if len(obs.Relations) != 0 {
			t.Fatalf("scrape %d: inbound + unnamed rows must be skipped: %+v", i, obs)
		}
	}
}

func findEndpoint(obs entity.Observation, addr, port string) *entity.Entity {
	for i := range obs.Entities {
		e := &obs.Entities[i]
		if e.Type == entityTypeNetworkEndpoint &&
			e.ID[idKeyServerAddress] == addr && e.ID[idKeyServerPort] == port {
			return e
		}
	}
	return nil
}

// TestHostScopedEndpoints pins the loopback/link-local host-scoping: peers on
// loopback and link-local unicast (previously DROPPED) are now emitted with
// host.id in the endpoint identity so host A's 127.0.0.1 / the shared
// 169.254.169.254 metadata endpoint never collapse onto another host's; routable
// peers stay 3-key; unspecified/multicast stay dropped (Toise ADR 0032).
func TestHostScopedEndpoints(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 53000, "127.0.0.1", 6379, 100),     // loopback
		conn(statusEstablished, "10.0.0.5", 53001, "169.254.169.254", 80, 100), // cloud metadata (link-local)
		conn(statusEstablished, "10.0.0.5", 53002, "8.8.8.8", 443, 100),        // routable
		conn(statusEstablished, "10.0.0.5", 53003, "224.0.0.1", 5353, 100),     // multicast
	}
	s := New(func() string { return "host-A" }, 1, nil)
	s.connections = fakeConns(rows)
	s.procName = func(int32) string { return "nginx" }
	obs, _ := s.Observe()

	if ep := findEndpoint(obs, "127.0.0.1", "6379"); ep == nil {
		t.Error("loopback peer must now be emitted")
	} else if ep.ID[idKeyHost] != "host-A" {
		t.Errorf("loopback endpoint must carry host.id, got ID=%v", ep.ID)
	}
	if ep := findEndpoint(obs, "169.254.169.254", "80"); ep == nil {
		t.Error("link-local metadata peer must be emitted")
	} else if ep.ID[idKeyHost] != "host-A" {
		t.Errorf("169.254.169.254 must be host-scoped, got ID=%v", ep.ID)
	}
	if ep := findEndpoint(obs, "8.8.8.8", "443"); ep == nil {
		t.Error("routable peer must be emitted")
	} else if _, has := ep.ID[idKeyHost]; has {
		t.Errorf("routable endpoint must NOT be host-scoped: %v", ep.ID)
	}
	if findEndpoint(obs, "224.0.0.1", "5353") != nil {
		t.Error("multicast peer must be dropped")
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

// TestNameLRU_CachesStableProcessAcrossScrapes pins #492: a stable owning
// process (same pid, same start time) is named once, then served from the
// cross-scrape cache on later scrapes — the optimization that keeps a
// high-socket host from re-resolving the same pids every cycle.
func TestNameLRU_CachesStableProcessAcrossScrapes(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 5433, 100), // same pid, 2nd peer
	}
	var nameCalls int
	s := New(func() string { return "h-1" }, defaultThreshold, nil)
	s.connections = fakeConns(rows)
	s.procName = func(int32) string { nameCalls++; return "nginx" }
	s.procCreated = func(int32) (int64, bool) { return 1717000000000, true } // stable start time

	for i := 0; i < 3; i++ {
		s.Observe()
	}
	// pid 100 is resolved exactly once total: once on the first scrape, then
	// served from the LRU on scrapes 2 and 3 (within a scrape the inner
	// nameCache already dedups the two sockets).
	if nameCalls != 1 {
		t.Errorf("stable pid should be named once across 3 scrapes, got %d resolutions", nameCalls)
	}
}

// TestNameLRU_RecycledPidIsReResolved pins the correctness guard: a pid reused
// by a new process (different start time) must not serve the old name — the
// (pid, createTime) key forces a fresh resolution.
func TestNameLRU_RecycledPidIsReResolved(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100),
	}
	names := []string{"old-proc", "new-proc"}
	var idx int
	var created int64 = 1000
	s := New(func() string { return "h-1" }, 1, nil) // threshold 1 → emits each scrape
	s.connections = fakeConns(rows)
	s.procName = func(int32) string {
		n := names[idx]
		if idx < len(names)-1 {
			idx++
		}
		return n
	}
	s.procCreated = func(int32) (int64, bool) { return created, true }

	obs, _ := s.Observe()
	if !hasService(obs, "old-proc@h-1") {
		t.Fatalf("first scrape should name the original process: %+v", obs.Entities)
	}
	// pid 100 recycled: same number, new start time → cache must miss.
	created = 2000
	obs, _ = s.Observe()
	if !hasService(obs, "new-proc@h-1") {
		t.Errorf("recycled pid must be re-resolved, not served the stale name: %+v", obs.Entities)
	}
	// The old dependent is not re-derived from the cache — it is inside its miss
	// tolerance, the same grace every dependency now gets (#808). What must not
	// happen is the stale name being SERVED for the live socket, and it is not:
	// the socket resolves to new-proc. One more missed scrape retires the old
	// one, since the tolerance here is the threshold, 1.
	obs, _ = s.Observe()
	if hasService(obs, "old-proc@h-1") {
		t.Errorf("the recycled pid's old dependent must not outlive its miss tolerance: %+v", obs.Entities)
	}
	if !hasService(obs, "new-proc@h-1") {
		t.Errorf("the live dependent must remain: %+v", obs.Entities)
	}
}

func hasService(obs entity.Observation, id string) bool {
	for _, e := range obs.Entities {
		if e.Type == entityTypeServiceInstance && e.ID[idKeyServiceInstanceID] == id {
			return true
		}
	}
	return false
}

// Blindness (#808). Mapping a socket to its owning process reads
// /proc/<pid>/fd, which is owner-only, so a non-root daemon — the default since
// 0.5.4 — sees every other service's connections with no pid at all. Measured
// on a real host: 45 of 45 established sockets attributed as root, 0 of 45 as
// the service account.
//
// The danger is not the missing data, it is reporting the absence as a fact.
// An empty observation returned with ok=true tells the tracker that nothing
// depends on anything any more, and it retires every dependency the consumer
// holds. The source must say its view failed instead.

func TestBlind_UnattributableSocketsAreNotReportedAsNoDependencies(t *testing.T) {
	// Outbound, established, real peers — and not one owner the agent can name.
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 0),
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 8427, 0),
		conn(statusEstablished, "10.0.0.5", 51002, "10.0.0.10", 8481, 0),
	}
	s := newTestSource(rows)

	obs, ok := s.Observe()
	if ok {
		t.Fatalf("a scrape that could attribute nothing must not report success: %+v", obs)
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("a failed observation must be empty, not partial: %+v", obs)
	}
}

func TestBlind_ReportedOnceWithTheCounts(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 0),
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 8427, 0),
	}
	s := newTestSource(rows)
	var calls, observed, unattributable int
	s.OnBlind(func(o, u int) { calls++; observed, unattributable = o, u })

	for i := 0; i < 5; i++ {
		s.Observe()
	}
	if calls != 1 {
		t.Errorf("the operator is told once, not every cycle: %d calls", calls)
	}
	if observed != 2 || unattributable != 2 {
		t.Errorf("counts = observed %d / unattributable %d, want 2 / 2", observed, unattributable)
	}
}

// TestBlind_NotTriggeredByAnIdleHost: no outbound sockets at all is a
// legitimate empty observation, not a failure. Reporting failure there would
// keep a stale view alive on a host that genuinely depends on nothing.
func TestBlind_NotTriggeredByAnIdleHost(t *testing.T) {
	s := newTestSource(nil)
	obs, ok := s.Observe()
	if !ok {
		t.Errorf("a host with no outbound sockets is observable, not blind: %+v", obs)
	}
}

// TestBlind_NotTriggeredWhenOnlySomeSocketsAreUnattributable: a kernel row
// racing with process exit is normal. Blindness is the structural case — every
// socket but our own — so one nameless row among named ones must not blank the
// rail.
func TestBlind_NotTriggeredWhenOnlySomeSocketsAreUnattributable(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100), // nginx
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 8427, 0),   // unknown owner
	}
	s := newTestSource(rows)
	var blind bool
	s.OnBlind(func(int, int) { blind = true })

	var obs entity.Observation
	var ok bool
	for i := 0; i < defaultThreshold; i++ {
		obs, ok = s.Observe()
	}
	if !ok {
		t.Fatalf("a partially attributable scrape is still an observation: %+v", obs)
	}
	if blind {
		t.Error("blindness reported while a dependent was successfully named")
	}
	if relCount(obs, relDependsOn) != 1 {
		t.Errorf("the one attributable dependency must be emitted: %+v", obs.Relations)
	}
}

// TestBlind_AgentsOwnSocketsDoNotCountAsSight: the agent can always name its
// own sockets, since it owns them. If that were enough to call attribution
// working, the check would never fire on the very install shape it exists for.
func TestBlind_AgentsOwnSocketsDoNotCountAsSight(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 4317, 42), // the agent itself
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.9", 5432, 0),  // postgres, invisible
	}
	s := newTestSource(rows)
	s.selfPID = func() int32 { return 42 }
	s.agentID = func() string { return "agent-1" }
	var blind bool
	s.OnBlind(func(int, int) { blind = true })

	if _, ok := s.Observe(); ok {
		t.Error("only the agent's own sockets attributed: that is blindness, not sight")
	}
	if !blind {
		t.Error("the operator was not told")
	}
}
