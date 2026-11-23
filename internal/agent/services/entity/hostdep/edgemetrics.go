package hostdep

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/netconn"
)

// Per-edge connection metrics: round-trip time, retransmissions and bytes of
// the sockets behind each emitted depends_on edge.
//
// hostdep is the only place that knows which socket belongs to which edge (it
// owns the pid/uid to service attribution), so it keeps a snapshot of the edges
// it currently emits and of the four-tuples attributed to them. The network
// probe, which already reads the kernel socket table with tcp_info, hands that
// table to CollectEdgeMetrics and ships the result as ordinary metrics. The
// attribution therefore exists once, and the metrics carry exactly the keys of
// the edge — never the client port, which would mint a series per connection.

// EdgeMetric is one edge's connection telemetry at one scrape. Attributes holds
// exactly the identity keys of the depends_on edge: service.instance.id,
// server.address, server.port, network.transport, plus host.id when the target
// endpoint is host-local.
type EdgeMetric struct {
	Attributes map[string]string

	// RTT is the mean smoothed round-trip time over the edge's established
	// sockets that carry tcp_info at this scrape; HasRTT is false when none
	// does, and the gauge is then not reported.
	RTT    time.Duration
	HasRTT bool

	// Cumulative totals per edge, monotonic for the life of the edge.
	Retransmits   uint64
	BytesSent     uint64
	BytesReceived uint64
}

// edgeKey identifies an edge by the canonical keys the consumer sees.
type edgeKey struct {
	svcID, addr, port, hostID string
}

func (k edgeKey) attributes() map[string]string {
	a := map[string]string{
		idKeyServiceInstanceID: k.svcID,
		idKeyServerAddress:     k.addr,
		idKeyServerPort:        k.port,
		idKeyNetworkTransport:  transportTCP,
	}
	if k.hostID != "" {
		a[idKeyHost] = k.hostID
	}
	return a
}

// sockKey identifies a socket across scrapes: its inode, or the four-tuple when
// the kernel reports none.
type sockKey struct {
	inode uint32
	tuple tupleKey
}

// sockState is a socket's counters as last seen.
type sockState struct {
	retrans, sent, received uint64
}

type edgeTotals struct {
	retrans, sent, received uint64
}

// edgeRegistry joins the hostdep attribution to the socket table and keeps the
// running totals.
type edgeRegistry struct {
	mu      sync.Mutex
	byTuple map[tupleKey]edgeKey
	edges   map[edgeKey]struct{}
	totals  map[edgeKey]*edgeTotals
	socks   map[sockKey]sockState
}

func newEdgeRegistry() *edgeRegistry {
	return &edgeRegistry{
		byTuple: map[tupleKey]edgeKey{},
		edges:   map[edgeKey]struct{}{},
		totals:  map[edgeKey]*edgeTotals{},
		socks:   map[sockKey]sockState{},
	}
}

// sharedEdges is the registry the production source publishes to and
// CollectEdgeMetrics reads; one per process because there is one host.
var sharedEdges = newEdgeRegistry()

// CollectEdgeMetrics turns one socket-table read into per-edge metrics for the
// edges hostdep currently emits. It returns nothing until hostdep has emitted
// an edge, and on a platform whose table carries no tcp_info.
func CollectEdgeMetrics(conns []netconn.Conn) []EdgeMetric {
	return sharedEdges.collect(conns)
}

// emittedEdges maps each peer that has passed the debounce (the edges
// buildObservation emits, miss tolerance included) to its edge key.
func emittedEdges(streak map[peerKey]streakState, threshold int, hostID string) map[peerKey]edgeKey {
	out := make(map[peerKey]edgeKey, len(streak))
	for pk, st := range streak {
		if st.hits < threshold {
			continue
		}
		ep := endpointID(st.dep.addr, st.dep.port, hostID)
		k := edgeKey{
			svcID: st.dep.svcID,
			addr:  fmt.Sprint(ep[idKeyServerAddress]),
			port:  fmt.Sprint(ep[idKeyServerPort]),
		}
		if h, ok := ep[idKeyHost]; ok {
			k.hostID = fmt.Sprint(h)
		}
		out[pk] = k
	}
	return out
}

// publish replaces the set of emitted edges and the sockets attributed to them.
// Totals of an edge that is no longer emitted are dropped, so its series ends
// with it; if the same edge returns later it starts from zero, as a new series.
func (r *edgeRegistry) publish(emitted map[peerKey]edgeKey, deps []dependant) {
	byTuple := make(map[tupleKey]edgeKey, len(deps))
	edges := make(map[edgeKey]struct{}, len(emitted))
	for _, k := range emitted {
		edges[k] = struct{}{}
	}
	for _, d := range deps {
		if k, ok := emitted[peerKey{d.svcID, d.addr, d.port}]; ok {
			byTuple[d.tuple] = k
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byTuple = byTuple
	r.edges = edges
	for k := range r.totals {
		if _, ok := edges[k]; !ok {
			delete(r.totals, k)
		}
	}
}

func (r *edgeRegistry) collect(conns []netconn.Conn) []EdgeMetric {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.edges) == 0 {
		r.socks = map[sockKey]sockState{}
		return nil
	}

	type rttSum struct {
		sum time.Duration
		n   int
	}
	rtts := map[edgeKey]rttSum{}
	seen := make(map[sockKey]sockState, len(r.socks))

	for _, c := range conns {
		if c.State != netconn.StateEstablished || !c.HasInfo {
			continue
		}
		t := tupleKey{
			laddr: c.LocalAddr.Addr().Unmap().String(), lport: uint32(c.LocalAddr.Port()),
			raddr: c.RemoteAddr.Addr().Unmap().String(), rport: uint32(c.RemoteAddr.Port()),
		}
		ek, ok := r.byTuple[t]
		if !ok {
			continue
		}
		sk := sockKey{inode: c.Inode}
		if c.Inode == 0 {
			sk.tuple = t
		}
		cur := sockState{retrans: uint64(c.Retransmits), sent: c.BytesSent, received: c.BytesReceived}
		prev, known := r.socks[sk]
		seen[sk] = cur

		tot := r.totals[ek]
		if tot == nil {
			tot = &edgeTotals{}
			r.totals[ek] = tot
		}
		// A socket seen for the first time contributes its whole current value:
		// those bytes were really exchanged, and the edge may have been
		// established long before it was first observed. After that only the
		// growth since the previous scrape is added. A value below the previous
		// one (kernel reuse of an inode, or a counter reset) adds nothing: the
		// total never goes down, and the socket restarts from its new value.
		if !known {
			prev = sockState{}
		}
		tot.retrans += nonNegDelta(cur.retrans, prev.retrans)
		tot.sent += nonNegDelta(cur.sent, prev.sent)
		tot.received += nonNegDelta(cur.received, prev.received)

		s := rtts[ek]
		s.sum += c.RTT
		s.n++
		rtts[ek] = s
	}
	// Sockets that did not appear are gone: forget them.
	r.socks = seen

	out := make([]EdgeMetric, 0, len(r.totals))
	for ek, tot := range r.totals {
		m := EdgeMetric{
			Attributes:    ek.attributes(),
			Retransmits:   tot.retrans,
			BytesSent:     tot.sent,
			BytesReceived: tot.received,
		}
		if s := rtts[ek]; s.n > 0 {
			m.RTT = s.sum / time.Duration(s.n)
			m.HasRTT = true
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Attributes, out[j].Attributes
		for _, key := range []string{idKeyServiceInstanceID, idKeyServerAddress, idKeyServerPort, idKeyHost} {
			if a[key] != b[key] {
				return a[key] < b[key]
			}
		}
		return false
	})
	return out
}

func nonNegDelta(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}
