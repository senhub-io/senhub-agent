// Package hostsvc is the host-side service-inventory entity source: it
// enumerates the host's listening TCP sockets and emits each as a
// service.listener entity — "what runs on this host and exposes a port" —
// characterising the host beyond its bare identity (OTel resource/process
// model). Each listener carries its process facts (executable, pid, transport)
// and is attached to the host.
//
// Attachment shape: the listener is tied to the host with the frozen runs_on
// relation (a listener runs on the host). The richer listens_on → the specific
// network.interface the socket binds (per the Toise reference producer) is
// deferred: it needs the host's interfaces as entities and a decision for
// wildcard binds (0.0.0.0/::), and the exact listener attachment is not pinned
// in the conformance fixture — flagged for Toise alignment (#252).
package hostsvc

import (
	"fmt"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceListener = "service.listener"
	entityTypeHost            = "host"
	idKeyServiceEndpoint      = "service.endpoint"
	idKeyHost                 = "host.id"
	attrProcessName           = "process.executable.name"
	attrProcessPID            = "process.pid"
	attrTransport             = "network.transport"
	attrListenAddress         = "listen.address"
	relRunsOn                 = "runs_on"

	// Listeners change rarely; re-enumerate on a slow cadence (walking sockets
	// + per-pid process lookups is not free on a busy host) and serve the cache
	// in between.
	defaultRefresh = 60 * time.Second
)

// listener is one decoded listening socket.
type listener struct {
	Pid       int32
	Proc      string
	Address   string
	Port      uint32
	Transport string
}

// Source implements entity.Source for host listening services.
type Source struct {
	hostID    func() string
	enumerate func() []listener
	refresh   time.Duration

	mu    sync.Mutex
	cache entity.Observation
	last  time.Time
}

// New builds the host-service source. hostID returns the host's stable id
// (gopsutil HostID) — the same id the host entity uses, so the listeners hang
// off the same node.
func New(hostID func() string) *Source {
	return &Source{hostID: hostID, enumerate: enumerateListeners, refresh: defaultRefresh}
}

// Observe returns the host's listeners. Non-blocking between refreshes: it
// re-enumerates at most every refresh interval and serves the cached snapshot
// otherwise.
func (s *Source) Observe() entity.Observation {
	s.mu.Lock()
	stale := s.last.IsZero() || time.Since(s.last) >= s.refresh
	if !stale {
		obs := s.cache
		s.mu.Unlock()
		return obs
	}
	s.mu.Unlock()

	obs := buildObservation(s.hostID(), s.enumerate())

	s.mu.Lock()
	s.cache = obs
	s.last = time.Now()
	s.mu.Unlock()
	return obs
}

// buildObservation maps listening sockets → service.listener entities the host
// runs (runs_on). One entity per listener; the host endpoint is referenced.
func buildObservation(hostID string, ls []listener) entity.Observation {
	if hostID == "" || len(ls) == 0 {
		return entity.Observation{}
	}
	hostKey := map[string]any{idKeyHost: hostID}

	obs := entity.Observation{}
	for _, l := range ls {
		endpoint := fmt.Sprintf("%s:%d/%s", hostID, l.Port, l.Transport)
		listenerID := map[string]any{idKeyServiceEndpoint: endpoint}

		attrs := map[string]any{attrTransport: l.Transport}
		if l.Proc != "" {
			attrs[attrProcessName] = l.Proc
		}
		if l.Pid > 0 {
			attrs[attrProcessPID] = int64(l.Pid)
		}
		if l.Address != "" {
			attrs[attrListenAddress] = l.Address
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeServiceListener, ID: listenerID, Attributes: attrs,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relRunsOn,
			FromType: entityTypeServiceListener, FromID: listenerID,
			ToType: entityTypeHost, ToID: hostKey,
		})
	}
	return obs
}

// enumerateListeners returns the host's listening TCP sockets, one per port
// (collapsing the IPv4/IPv6 wildcard pair), with the owning process name. The
// real implementation; injected in tests.
func enumerateListeners() []listener {
	conns, err := gnet.Connections("tcp")
	if err != nil {
		return nil
	}
	out := make([]listener, 0, len(conns))
	seen := map[uint32]bool{}
	for _, c := range conns {
		if c.Status != "LISTEN" || c.Pid <= 0 || seen[c.Laddr.Port] {
			continue
		}
		seen[c.Laddr.Port] = true
		proc := ""
		if p, err := process.NewProcess(c.Pid); err == nil {
			if name, err := p.Name(); err == nil {
				proc = name
			}
		}
		out = append(out, listener{
			Pid: c.Pid, Proc: proc,
			Address: c.Laddr.IP, Port: c.Laddr.Port, Transport: "tcp",
		})
	}
	return out
}
