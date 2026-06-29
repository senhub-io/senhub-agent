// Package hostdep is the host-side dependency entity source (#457): from the
// kernel socket table it derives durable OUTBOUND dependency edges and emits
// each as a service.instance --depends_on--> network.endpoint relation, so the
// graph shows "service A depends on B:443" — the relational context an LLM
// needs to reason about the system.
//
// Contract (Toise #184, the connection-topology design note):
//
//   - The dependent (source of the edge) is a service.instance, the spec-native
//     running-service entity. The agent mints its identity from the local
//     owning process the same way the probes do (service.instance.id =
//     "<exe>@<host.id>"); it never reads the peer's identity. A connection whose
//     local owner cannot be named is skipped, not attributed to a guessed
//     entity (data-model MUST-NOT: do not emit an identity you cannot populate).
//   - The target is an observable network.endpoint keyed on what the observer
//     can see: {server.address, server.port, network.transport}. It is emitted
//     as its own entity in the same cycle — Toise resolves a relation's
//     endpoints by identity and drops an edge whose target was never observed.
//     The consumer resolves the endpoint to the canonical remote listener/host
//     at read time (a read overlay, never written back).
//
// Direction: a TCP ESTABLISHED row whose local port is in our own LISTEN set is
// inbound (someone depends on us — answered consumer-side by incoming
// traversal) and ignored here; otherwise it is outbound and is a dependency.
//
// Durability: an edge is emitted only after its peer endpoint has persisted
// across `threshold` consecutive scrapes (debounce), aggregated per distinct
// peer endpoint — never per socket. This keeps the truth store free of
// ephemeral flow (live per-socket telemetry is out of scope). The edge carries
// no attributes (connection counts/bytes are telemetry, deferred in the spec).
package hostdep

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"

	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	entityTypeNetworkEndpoint = "network.endpoint"
	entityTypeHost            = "host"
	idKeyServiceInstanceID    = "service.instance.id"
	idKeyHost                 = "host.id"
	idKeyServerAddress        = "server.address"
	idKeyServerPort           = "server.port"
	idKeyNetworkTransport     = "network.transport"
	attrServiceName           = "service.name"
	relDependsOn              = "depends_on"
	relRunsOn                 = "runs_on"
	transportTCP              = "tcp"

	statusListen      = "LISTEN"
	statusEstablished = "ESTABLISHED"

	// A peer endpoint must be seen this many consecutive scrapes before its edge
	// is emitted — the line between a durable dependency and ephemeral flow.
	// The configured value comes from entities.depends_on_debounce; this is the
	// fallback when the caller passes a non-positive threshold.
	defaultThreshold = 3
)

// peerKey aggregates per distinct (dependent service, peer endpoint): many
// sockets from the same service to the same peer collapse to one edge.
type peerKey struct {
	svcID, addr, port string
}

// dependant is the minted dependent plus the peer it depends on. foundation is
// set when the dependent is the agent's own process: its identity is the
// foundation service.instance (the agent key), which the foundation detector
// already emits with its runs_on — so hostdep emits only the depends_on edge
// for it, not a duplicate node (#494).
type dependant struct {
	svcID, svcName string
	addr, port     string
	foundation     bool
}

// Source implements entity.Source for host outbound dependency edges.
type Source struct {
	hostID      func() string
	connections func(string) ([]gnet.ConnectionStat, error) // nil → gnet.Connections
	procName    func(int32) string                          // nil → gopsutil process name
	agentID     func() string                               // nil → agentstate.GetAgentInstanceID
	selfPID     func() int32                                // nil → os.Getpid
	threshold   int
	exclude     []*net.IPNet // peer endpoints in these ranges are dropped (privacy)

	mu     sync.Mutex
	streak map[peerKey]int // consecutive scrapes a peer endpoint has been seen
}

// New builds the host-dependency source. hostID returns the host's stable id,
// used to mint the dependent service.instance.id (so an outbound dependency
// hangs off a service tied to this host). threshold is the debounce length (the
// number of consecutive scrapes a peer must persist before its edge is emitted);
// a non-positive value falls back to defaultThreshold. exclude lists peer IP
// ranges whose flows are dropped (operator privacy filter); nil disables it.
func New(hostID func() string, threshold int, exclude []*net.IPNet) *Source {
	if threshold < 1 {
		threshold = defaultThreshold
	}
	return &Source{hostID: hostID, threshold: threshold, exclude: exclude, streak: map[peerKey]int{}}
}

// excluded reports whether a peer IP falls in any operator-excluded range.
func (s *Source) excluded(ip string) bool {
	if len(s.exclude) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.exclude {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// Observe reads the socket table once (one scrape), advances the debounce
// streaks, and returns the durable dependency edges as a full snapshot. A
// failed read reports ok=false and leaves the streaks untouched — a transient
// sockets error must not delete every dependency in the consumer, nor reset the
// debounce progress.
func (s *Source) Observe() (entity.Observation, bool) {
	connFn := s.connections
	if connFn == nil {
		connFn = gnet.Connections
	}
	conns, err := connFn("tcp")
	if err != nil {
		return entity.Observation{}, false
	}
	hostID := s.hostID()
	if hostID == "" {
		return entity.Observation{}, false
	}

	deps := s.scrape(conns, hostID)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Advance streaks: increment what we saw this scrape, drop what we did not
	// (a vanished connection resets to zero so its edge leaves the snapshot).
	seen := make(map[peerKey]dependant, len(deps))
	for _, d := range deps {
		seen[peerKey{d.svcID, d.addr, d.port}] = d
	}
	next := make(map[peerKey]int, len(seen))
	for k := range seen {
		n := s.streak[k] + 1
		if n > s.threshold {
			n = s.threshold
		}
		next[k] = n
	}
	s.streak = next

	return buildObservation(seen, next, s.threshold, hostID).WithScope(entity.ScopeHostDep), true
}

// scrape classifies one socket-table read into candidate outbound dependants.
func (s *Source) scrape(conns []gnet.ConnectionStat, hostID string) []dependant {
	listenPorts := map[uint32]bool{}
	for _, c := range conns {
		if c.Status == statusListen {
			listenPorts[c.Laddr.Port] = true
		}
	}
	nameFn := s.procName
	if nameFn == nil {
		nameFn = processName
	}
	agentIDFn := s.agentID
	if agentIDFn == nil {
		agentIDFn = agentstate.GetAgentInstanceID
	}
	selfPIDFn := s.selfPID
	if selfPIDFn == nil {
		selfPIDFn = func() int32 { return int32(os.Getpid()) }
	}
	self := selfPIDFn()
	agentID := agentIDFn()
	nameCache := map[int32]string{}

	var out []dependant
	for _, c := range conns {
		if c.Status != statusEstablished || listenPorts[c.Laddr.Port] {
			continue // not established, or inbound (local port is one of ours)
		}
		if !resolvablePeer(c.Raddr.IP) || c.Raddr.Port == 0 || s.excluded(c.Raddr.IP) {
			continue
		}
		name, ok := nameCache[c.Pid]
		if !ok {
			name = nameFn(c.Pid)
			nameCache[c.Pid] = name
		}
		d := dependant{
			addr: c.Raddr.IP,
			port: strconv.FormatUint(uint64(c.Raddr.Port), 10),
		}
		switch {
		case c.Pid == self && agentID != "":
			// The agent's own outbound dependency: attach it to the foundation
			// service.instance (the agent key) instead of minting a parallel
			// <exe>@host node (#494). No name needed — the foundation owns the
			// node and its runs_on.
			d.svcID = agentID
			d.svcName = name
			d.foundation = true
		case name != "":
			d.svcID = name + "@" + hostID
			d.svcName = name
		default:
			continue // cannot name the dependent → do not fabricate a service.instance
		}
		out = append(out, d)
	}
	return out
}

// buildObservation emits, for every peer endpoint that has reached the
// durability threshold, the dependent service.instance, the observable
// network.endpoint, and the depends_on edge between them. Each service.instance
// is also anchored to the host it runs on with a runs_on edge (the host comes
// from the foundation entity merged into the same cycle, so it is referenced,
// not re-emitted — mirroring hostsvc's listener runs_on); without it the minted
// dependents would float, unattached to the host they run on. Each
// service.instance and each endpoint is emitted once.
func buildObservation(seen map[peerKey]dependant, streak map[peerKey]int, threshold int, hostID string) entity.Observation {
	obs := entity.Observation{}
	hostKey := map[string]any{idKeyHost: hostID}
	svcDone := map[string]bool{}
	epDone := map[string]bool{}
	for k, d := range seen {
		if streak[k] < threshold {
			continue
		}
		svcKey := map[string]any{idKeyServiceInstanceID: d.svcID}
		if !svcDone[d.svcID] {
			svcDone[d.svcID] = true
			// The agent's own dependent is the foundation service.instance; the
			// foundation detector already emits that node and its runs_on, so
			// emitting them here would duplicate the node (#494). The depends_on
			// edge below still folds onto the foundation node (same identity,
			// merged in the same cycle).
			if !d.foundation {
				obs.Entities = append(obs.Entities, entity.Entity{
					Type: entityTypeServiceInstance, ID: svcKey,
					Attributes: map[string]any{attrServiceName: d.svcName},
				})
				obs.Relations = append(obs.Relations, entity.Relation{
					Type:     relRunsOn,
					FromType: entityTypeServiceInstance, FromID: svcKey,
					ToType: entityTypeHost, ToID: hostKey,
				})
			}
		}
		epID := map[string]any{
			idKeyServerAddress:    d.addr,
			idKeyServerPort:       d.port,
			idKeyNetworkTransport: transportTCP,
		}
		epDedup := fmt.Sprintf("%s:%s/%s", d.addr, d.port, transportTCP)
		if !epDone[epDedup] {
			epDone[epDedup] = true
			obs.Entities = append(obs.Entities, entity.Entity{
				Type: entityTypeNetworkEndpoint, ID: epID,
			})
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relDependsOn,
			FromType: entityTypeServiceInstance, FromID: svcKey,
			ToType: entityTypeNetworkEndpoint, ToID: epID,
		})
	}
	return obs
}

// processName resolves a pid to its executable name, best-effort ("" when the
// kernel withholds it — e.g. a foreign process for an unprivileged agent).
func processName(pid int32) string {
	if pid <= 0 {
		return ""
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return ""
	}
	name, err := p.Name()
	if err != nil {
		return ""
	}
	return name
}

// resolvablePeer reports whether a peer IP is worth a dependency edge: a real
// remote unicast address, not loopback/link-local/unspecified/multicast (a
// service talking to itself locally is not topology).
func resolvablePeer(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast())
}
