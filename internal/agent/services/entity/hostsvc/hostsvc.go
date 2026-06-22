// Package hostsvc is the host-side service-inventory entity source: it
// enumerates the host's listening TCP sockets and emits each as a
// service.listener entity — "what runs on this host and exposes a port" —
// characterising the host beyond its bare identity (OTel resource/process
// model). Each listener carries its process facts (executable, pid, transport)
// and is attached to the host.
//
// Attachment shape (canonical, #252): a non-wildcard listener binds a specific
// host IP, which belongs to one of the host's interfaces — so it is tied to that
// interface with listens_on (service.listener --listens_on--> network.interface,
// the interface emitted by hostiface). The edge is bare: the port is a fact of
// the listener and rides its entity (the service.endpoint identity
// <host>:<port>/<proto> plus an explicit port attribute), never the edge (ADR
// 0022 / no edge attributes). A wildcard/loopback bind (0.0.0.0/::, 127.0.0.0/8)
// or one whose IP resolves to no local interface has no single interface to point
// at and falls back to runs_on --> host (interim), keeping listen.address as an
// attribute.
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
	entityTypeServiceListener  = "service.listener"
	entityTypeHost             = "host"
	entityTypeNetworkInterface = "network.interface"
	idKeyServiceEndpoint       = "service.endpoint"
	idKeyHost                  = "host.id"
	idKeyInterfaceName         = "interface.name"
	attrProcessName            = "process.executable.name"
	attrProcessPID             = "process.pid"
	attrTransport              = "network.transport"
	attrListenAddress          = "listen.address"
	attrPort                   = "port"
	relRunsOn                  = "runs_on"
	relListensOn               = "listens_on"

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
	interfaces  func() (gnet.InterfaceStatList, error)      // nil → gnet.Interfaces; resolves bind IP → interface
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
	obs := buildObservation(s.hostID(), ls, s.ipToIface())

	s.mu.Lock()
	s.cache = obs
	s.last = time.Now()
	s.mu.Unlock()
	return obs, true
}

// buildObservation maps listening sockets → service.listener entities, each
// attached either to the host interface it binds (listens_on, non-wildcard) or
// to the host (runs_on, wildcard/interim). ipToIface resolves a bind IP to its
// interface name.
func buildObservation(hostID string, ls []listener, ipToIface map[string]string) entity.Observation {
	if hostID == "" || len(ls) == 0 {
		return entity.Observation{}
	}
	hostKey := map[string]any{idKeyHost: hostID}

	obs := entity.Observation{}
	for _, l := range ls {
		endpoint := fmt.Sprintf("%s:%d/%s", hostID, l.Port, l.Transport)
		listenerID := map[string]any{idKeyServiceEndpoint: endpoint}

		attrs := map[string]any{attrTransport: l.Transport}
		if l.Port > 0 {
			attrs[attrPort] = int64(l.Port)
		}
		if l.Proc != "" {
			attrs[attrProcessName] = l.Proc
		}
		if l.Pid > 0 {
			attrs[attrProcessPID] = int64(l.Pid)
		}

		// Resolve a non-wildcard bind to the host interface that owns the IP.
		ifname := ""
		if boundIP := bindableIP(l.Address); boundIP != "" {
			ifname = ipToIface[boundIP]
		}
		if ifname == "" && l.Address != "" {
			// Wildcard/loopback bind, or an IP on no local interface: keep the
			// address as a descriptive attribute (the runs_on fallback applies).
			attrs[attrListenAddress] = l.Address
		}

		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeServiceListener, ID: listenerID, Attributes: attrs,
		})

		if ifname != "" {
			// Canonical: the listener listens on the interface that owns its bind
			// IP. The edge is bare — the port is a fact of the listener (its
			// identity + the port attribute), never the edge.
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relListensOn,
				FromType: entityTypeServiceListener, FromID: listenerID,
				ToType: entityTypeNetworkInterface,
				ToID:   map[string]any{idKeyHost: hostID, idKeyInterfaceName: ifname},
			})
		} else {
			// Interim: no single interface to point at → attach to the host.
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relRunsOn,
				FromType: entityTypeServiceListener, FromID: listenerID,
				ToType: entityTypeHost, ToID: hostKey,
			})
		}
	}
	return obs
}

// ipToIface maps each of the host's unicast IPs to the interface that owns it,
// so a non-wildcard listener bind can resolve to its network.interface.
// Best-effort: on a read error the map is empty and every listener falls back to
// runs_on --> host.
func (s *Source) ipToIface() map[string]string {
	ifFn := s.interfaces
	if ifFn == nil {
		ifFn = gnet.Interfaces
	}
	ifaces, err := ifFn()
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		for _, a := range ifc.Addrs {
			if ip := bareIP(a.Addr); ip != "" {
				m[ip] = ifc.Name
			}
		}
	}
	return m
}

// bareIP normalizes a gopsutil address ("ip/prefix" or a bare ip) to the bare IP
// string, or "" if unparseable.
func bareIP(addr string) string {
	if ip, _, err := net.ParseCIDR(addr); err == nil {
		return ip.String()
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}
	return ""
}

// bindableIP returns the bare IP a listener binds to when it is a real unicast
// address that can resolve to one of the host's interfaces (the listens_on
// target). Wildcard (0.0.0.0/::), loopback, link-local, unspecified and
// multicast binds return "" — they have no single interface to point at and
// fall back to runs_on --> host.
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
