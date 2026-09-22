package hostsvc

import (
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildObservation_Listeners(t *testing.T) {
	ls := []listener{
		{Pid: 1001, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},
		{Pid: 22, Proc: "sshd", Address: "0.0.0.0", Port: 22, Transport: "tcp"},
	}
	obs := buildObservation("h-1", ls, nil, 0) // wildcard binds → no interface to resolve

	// Two listeners, plus the service.instance each one says exists and
	// its runs_on.
	if len(listenersOf(obs)) != 2 || len(instancesOf(obs)) != 2 {
		t.Fatalf("obs = %+v", obs)
	}

	nginx, found := entityByProc(obs, "nginx")
	if !found {
		t.Fatalf("nginx listener not emitted: %+v", obs.Entities)
	}
	if nginx.Type != entityTypeServiceListener {
		t.Errorf("type = %q, want service.listener", nginx.Type)
	}
	if nginx.ID[idKeyServiceEndpoint] != "h-1:80/tcp" {
		t.Errorf("endpoint id = %v, want h-1:80/tcp", nginx.ID[idKeyServiceEndpoint])
	}
	if nginx.Attributes[attrProcessPID] != int64(1001) ||
		nginx.Attributes[attrTransport] != "tcp" ||
		nginx.Attributes[attrPort] != int64(80) ||
		nginx.Attributes[attrListenAddress] != "0.0.0.0" {
		t.Errorf("attrs = %+v", nginx.Attributes)
	}

	// Wildcard binds attach to the host (interim runs_on), and so does
	// every instance.
	for _, r := range obs.Relations {
		if r.Type != relRunsOn {
			t.Errorf("relation type = %q, want runs_on", r.Type)
		}
		if r.FromType != entityTypeServiceListener && r.FromType != entityTypeServiceInstance {
			t.Errorf("runs_on from %q", r.FromType)
		}
		if r.ToType != entityTypeHost || r.ToID[idKeyHost] != "h-1" {
			t.Errorf("runs_on wrong: %+v", r)
		}
	}
}

// listenersOf and instancesOf split an observation by entity type: the
// source emits both, and most assertions are about one of them.
func listenersOf(obs entity.Observation) []entity.Entity {
	return entitiesOfType(obs, entityTypeServiceListener)
}

func instancesOf(obs entity.Observation) []entity.Entity {
	return entitiesOfType(obs, entityTypeServiceInstance)
}

func entitiesOfType(obs entity.Observation, want string) []entity.Entity {
	var out []entity.Entity
	for _, e := range obs.Entities {
		if e.Type == want {
			out = append(out, e)
		}
	}
	return out
}

// TestBuildObservation_ListensOnInterface (#252): a non-wildcard bind resolves
// to the host interface that owns the IP and is tied with listens_on (bare edge,
// port on the entity), with no listen.address attribute; wildcard, loopback and
// unresolved binds fall back to runs_on --> host and keep listen.address.
func TestBuildObservation_ListensOnInterface(t *testing.T) {
	ls := []listener{
		{Pid: 10, Proc: "pg", Address: "10.0.0.5", Port: 5432, Transport: "tcp"},     // specific, on eth0 → listens_on
		{Pid: 20, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},     // wildcard → runs_on
		{Pid: 30, Proc: "redis", Address: "127.0.0.1", Port: 6379, Transport: "tcp"}, // loopback → runs_on
		{Pid: 40, Proc: "app", Address: "192.0.2.9", Port: 9000, Transport: "tcp"},   // specific, no local iface → runs_on
	}
	ipToIface := map[string]string{"10.0.0.5": "eth0"}
	obs := buildObservation("h-1", ls, ipToIface, 0)

	// pg: listens_on → network.interface{h-1, eth0}, no listen.address, port on entity.
	pg, _ := entityByProc(obs, "pg")
	if _, has := pg.Attributes[attrListenAddress]; has {
		t.Error("resolved bind must NOT carry listen.address (replaced by listens_on)")
	}
	if pg.Attributes[attrPort] != int64(5432) {
		t.Errorf("port must ride the listener entity: %+v", pg.Attributes)
	}
	listensOn := 0
	for _, r := range obs.Relations {
		if r.Type != relListensOn {
			continue
		}
		listensOn++
		if r.FromID[idKeyServiceEndpoint] != "h-1:5432/tcp" {
			t.Errorf("listens_on From wrong: %+v", r)
		}
		if r.ToType != entityTypeNetworkInterface || r.ToID[idKeyHost] != "h-1" || r.ToID[idKeyInterfaceName] != "eth0" {
			t.Errorf("listens_on To must be network.interface{h-1, eth0}: %+v", r)
		}
		if len(r.Attributes) != 0 {
			t.Errorf("listens_on edge must be bare (no attributes): %+v", r.Attributes)
		}
	}
	if listensOn != 1 {
		t.Errorf("want exactly 1 listens_on (only the resolved bind), got %d", listensOn)
	}

	// The other three fall back to runs_on --> host and keep listen.address.
	for _, proc := range []string{"nginx", "redis", "app"} {
		e, _ := entityByProc(obs, proc)
		if e.Attributes[attrListenAddress] == nil {
			t.Errorf("%s must keep listen.address (runs_on fallback): %+v", proc, e.Attributes)
		}
	}
	runsOn := 0
	for _, r := range obs.Relations {
		if r.Type == relRunsOn {
			runsOn++
		}
	}
	// Three listeners fall back to the host, and each of the four
	// processes gets the runs_on of the instance it is.
	if runsOn != 3+len(instancesOf(obs)) {
		t.Errorf("want %d runs_on (wildcard/loopback/unresolved, plus one per instance), got %d",
			3+len(instancesOf(obs)), runsOn)
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []listener{{Port: 80, Transport: "tcp"}}, nil, 0); len(o.Entities) != 0 {
		t.Error("no hostID → empty")
	}
	if o := buildObservation("h", nil, nil, 0); len(o.Entities) != 0 {
		t.Error("no listeners → empty")
	}
}

func TestBuildObservation_ProcAndPidOmittedWhenAbsent(t *testing.T) {
	obs := buildObservation("h-1", []listener{{Port: 443, Transport: "tcp"}}, nil, 0) // no Proc, Pid 0, no Address
	if n := len(instancesOf(obs)); n != 0 {
		t.Errorf("a listener whose owner cannot be named mints no instance; got %d", n)
	}
	a := listenersOf(obs)[0].Attributes
	if _, ok := a[attrProcessName]; ok {
		t.Error("process.executable.name should be omitted when unknown")
	}
	if _, ok := a[attrProcessPID]; ok {
		t.Error("process.pid should be omitted when 0")
	}
	if a[attrTransport] != "tcp" {
		t.Errorf("transport always present: %+v", a)
	}
}

func TestObserve_CachesBetweenRefreshes(t *testing.T) {
	calls := 0
	s := &Source{
		hostID: func() string { return "h-1" },
		enumerate: func() ([]listener, error) {
			calls++
			return []listener{{Pid: 1, Proc: "x", Port: 80, Transport: "tcp"}}, nil
		},
		interfaces: func() (gnet.InterfaceStatList, error) { return nil, nil },
		refresh:    time.Hour,
	}
	o1, ok1 := s.Observe()
	o2, ok2 := s.Observe() // within the refresh window → served from cache, no re-enumeration
	if !ok1 || !ok2 {
		t.Fatal("successful enumerations must report ok")
	}
	if calls != 1 {
		t.Errorf("enumerate called %d times, want 1 (cached within refresh)", calls)
	}
	if len(listenersOf(o1)) != 1 || len(listenersOf(o2)) != 1 {
		t.Errorf("obs1=%d obs2=%d listeners", len(listenersOf(o1)), len(listenersOf(o2)))
	}
}

// TestEnumerateListeners_Pid0NotFiltered is the regression test for #394.
// On Linux non-root, gopsutil cannot read /proc/<pid>/fd of foreign processes
// and returns Pid=0 for those sockets. Before the fix, the filter
// `c.Pid <= 0` silently discarded every listener, producing an empty entity
// observation. After the fix, Pid=0 sockets are accepted and emitted with
// process facts omitted (best-effort enrichment).
func TestEnumerateListeners_Pid0NotFiltered(t *testing.T) {
	fakeStat := []gnet.ConnectionStat{
		{Status: "LISTEN", Pid: 0, Laddr: gnet.Addr{IP: "0.0.0.0", Port: 8080}},
		// Non-LISTEN must still be excluded regardless of Pid.
		{Status: "ESTABLISHED", Pid: 0, Laddr: gnet.Addr{IP: "0.0.0.0", Port: 9090}},
		// Duplicate port (IPv4+IPv6 wildcard pair): only first should appear.
		{Status: "LISTEN", Pid: 0, Laddr: gnet.Addr{IP: "::", Port: 8080}},
	}

	s := &Source{
		hostID:  func() string { return "h-1" },
		refresh: time.Hour,
		connections: func(_ string) ([]gnet.ConnectionStat, error) {
			return fakeStat, nil
		},
		interfaces: func() (gnet.InterfaceStatList, error) { return nil, nil },
	}
	s.enumerate = s.enumerateListeners

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() returned ok=false; expected successful enumeration")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1 (port 8080); obs=%+v", len(obs.Entities), obs)
	}
	e := obs.Entities[0]
	if e.ID[idKeyServiceEndpoint] != "h-1:8080/tcp" {
		t.Errorf("entity ID = %v, want h-1:8080/tcp", e.ID[idKeyServiceEndpoint])
	}
	if _, ok := e.Attributes[attrProcessPID]; ok {
		t.Error("process.pid must be absent when Pid=0")
	}
	if _, ok := e.Attributes[attrProcessName]; ok {
		t.Error("process.executable.name must be absent when Pid=0")
	}
	if e.Attributes[attrTransport] != "tcp" {
		t.Errorf("network.transport = %v, want tcp", e.Attributes[attrTransport])
	}
}

func entityByProc(obs entity.Observation, proc string) (entity.Entity, bool) {
	for _, e := range obs.Entities {
		if e.Attributes[attrProcessName] == proc {
			return e, true
		}
	}
	return entity.Entity{}, false
}

// A server that listens without end must exist without end. Before this,
// the service.instance was minted by the dependency source alone, as the
// source of an outbound edge it had sampled, so a reverse proxy whose
// upstream connections are brief lost its instance whenever two
// consecutive samples caught none — nine times in sixty-three hours on a
// host where it never stopped running.
func TestAListeningProcessExistsWhateverItConnectsTo(t *testing.T) {
	obs := buildObservation("h-1", []listener{
		{Pid: 1001, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},
		{Pid: 1002, Proc: "nginx", Address: "0.0.0.0", Port: 443, Transport: "tcp"},
	}, nil, 0)

	inst := instancesOf(obs)
	if len(inst) != 1 {
		t.Fatalf("one process, one instance, whatever it listens on; got %+v", inst)
	}
	if inst[0].ID[idKeyServiceInstanceID] != "nginx@h-1" {
		t.Errorf("id = %v; the dependency source mints the same one, or the two nodes never merge",
			inst[0].ID[idKeyServiceInstanceID])
	}
	if inst[0].Attributes[attrServiceName] != "nginx" {
		t.Errorf("attrs = %+v", inst[0].Attributes)
	}

	var anchored bool
	for _, r := range obs.Relations {
		if r.FromType == entityTypeServiceInstance && r.Type == relRunsOn && r.ToID[idKeyHost] == "h-1" {
			anchored = true
		}
	}
	if !anchored {
		t.Error("the instance floats: without runs_on it hangs off no host")
	}
}

// The agent's own service.instance is the foundation's, keyed on the agent
// instance id. Minting <exe>@<host> for the agent's own listening console
// would put a second node beside it for the same running thing (#494).
func TestTheAgentsOwnListenerMintsNoParallelInstance(t *testing.T) {
	obs := buildObservation("h-1", []listener{
		{Pid: 4242, Proc: "senhub-agent", Address: "127.0.0.1", Port: 18058, Transport: "tcp"},
		{Pid: 1001, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},
	}, nil, 4242)

	for _, e := range instancesOf(obs) {
		if e.ID[idKeyServiceInstanceID] == "senhub-agent@h-1" {
			t.Errorf("the agent got a second instance beside the foundation's: %+v", e.ID)
		}
	}
	if len(instancesOf(obs)) != 1 {
		t.Errorf("the other process still gets its instance; got %+v", instancesOf(obs))
	}
	// The listener itself is still published: what the agent exposes is a
	// fact about the host like any other.
	if len(listenersOf(obs)) != 2 {
		t.Errorf("listeners = %d, want both", len(listenersOf(obs)))
	}
}
