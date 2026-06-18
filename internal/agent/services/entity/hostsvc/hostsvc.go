// Package hostsvc is the host-side service-inventory entity source: it
// enumerates the host's listening TCP sockets and emits each as a
// service.listener entity — "what runs on this host and exposes a port" —
// characterising the host beyond its bare identity (OTel resource/process
// model). Each listener carries its process facts (executable, pid, transport)
// and is attached to the host.
//
// Attachment shape: the listener is tied to the host with the frozen runs_on
// relation (a listener runs on the host). A non-wildcard bind is additionally
// tied to the network.address it binds with a bound_to relation (enterprise#37),
// so the IP is a shared hub (network.address --bound_to--> network.interface is
// emitted by hostiface) instead of an opaque string repeated per listener;
// wildcard/loopback binds (0.0.0.0/::, 127.0.0.0/8) have no address entity to
// point at and stay attribute-only (listen.address).
package hostsvc

import (
	"fmt"
	"net"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceListener = "service.listener"
	entityTypeHost            = "host"
	entityTypeNetworkAddress  = "network.address"
	idKeyServiceEndpoint      = "service.endpoint"
	idKeyHost                 = "host.id"
	idKeyNetworkAddress       = "network.address"
	attrProcessName           = "process.executable.name"
	attrProcessPID            = "process.pid"
	attrTransport             = "network.transport"
	attrListenAddress         = "listen.address"
	relRunsOn                 = "runs_on"
	relBoundTo                = "bound_to"

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
	hostID      func() string
	enumerate   func() ([]listener, error)
	connections func(string) ([]gnet.ConnectionStat, error) // nil → gnet.Connections
	refresh     time.Duration

	mu    sync.Mutex
	cache entity.Observation
	last  time.Time
}

// New builds the host-service source. hostID returns the host's stable id
// (gopsutil HostID) — the same id the host entity uses, so the listeners hang
// off the same node.
func New(hostID func() string) *Source {
	s := &Source{hostID: hostID, refresh: defaultRefresh}
	s.enumerate = s.enumerateListeners
	return s
}

// Observe returns the host's listeners. Non-blocking between refreshes: it
// re-enumerates at most every refresh interval and serves the cached snapshot
// otherwise. A failed enumeration keeps the previous cache and reports
// ok=false instead of replacing the listeners with an empty set — a
// transient sockets-read error must not delete every service.listener in
// the consumer (audit D3).
func (s *Source) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	stale := s.last.IsZero() || time.Since(s.last) >= s.refresh
	if !stale {
		obs := s.cache
		s.mu.Unlock()
		return obs, true
	}
	s.mu.Unlock()

	ls, err := s.enumerate()
	if err != nil {
		return entity.Observation{}, false
	}
	obs := buildObservation(s.hostID(), ls)

	s.mu.Lock()
	s.cache = obs
	s.last = time.Now()
	s.mu.Unlock()
	return obs, true
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
		// A non-wildcard bind to a real unicast IP is modeled as a relation to
		// the network.address entity (emitted by hostiface), so the IP becomes a
		// shared hub instead of an opaque string repeated on every listener
		// (enterprise#37). Wildcard/loopback/unspecified binds have no address
		// entity to point at and stay attribute-only.
		boundIP := bindableIP(l.Address)
		if boundIP == "" && l.Address != "" {
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
		if boundIP != "" {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relBoundTo,
				FromType: entityTypeServiceListener, FromID: listenerID,
				ToType: entityTypeNetworkAddress, ToID: map[string]any{idKeyNetworkAddress: boundIP},
			})
		}
	}
	return obs
}

// bindableIP returns the bare IP a listener binds to when it is a real unicast
// address that hostiface also emits as a network.address (so the bound_to edge
// resolves). Wildcard (0.0.0.0/::), loopback, link-local, unspecified and
// multicast binds return "" — they have no shared address entity to point at.
func bindableIP(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return ""
	}
	return ip.String()
}

// enumerateListeners returns the host's listening TCP sockets, one per port
// (collapsing the IPv4/IPv6 wildcard pair), with the owning process name.
// PID resolution is best-effort: on Linux non-root the kernel withholds
// /proc/<pid>/fd for foreign processes, so gopsutil returns Pid=0 for those
// sockets. Filtering on Pid>0 would silently drop every listener when the
// agent runs as an unprivileged user (#394). Process facts are enriched when
// available and omitted otherwise — the socket itself is always emitted.
func (s *Source) enumerateListeners() ([]listener, error) {
	connFn := s.connections
	if connFn == nil {
		connFn = gnet.Connections
	}
	conns, err := connFn("tcp")
	if err != nil {
		return nil, err
	}
	out := make([]listener, 0, len(conns))
	seen := map[uint32]bool{}
	for _, c := range conns {
		if c.Status != "LISTEN" || seen[c.Laddr.Port] {
			continue
		}
		seen[c.Laddr.Port] = true
		proc := ""
		if c.Pid > 0 {
			if p, err := process.NewProcess(c.Pid); err == nil {
				if name, err := p.Name(); err == nil {
					proc = name
				}
			}
		}
		out = append(out, listener{
			Pid: c.Pid, Proc: proc,
			Address: c.Laddr.IP, Port: c.Laddr.Port, Transport: "tcp",
		})
	}
	return out, nil
}
