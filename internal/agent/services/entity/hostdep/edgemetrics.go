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
	// retiredTuples keeps sockets that were attributed to an edge whose
	// totals are still retained but are not attributed now, so their baselines
	// survive and a returning edge does not count their whole value again.
	retiredTuples map[tupleKey]edgeKey
	edges         map[edgeKey]struct{}
	totals        map[edgeKey]*edgeTotals
	socks         map[sockKey]sockState
	// retiredAt is when an edge with totals stopped being emitted. Its totals
	// are kept for totalsRetention so an edge that flaps continues the same
	// series instead of restarting at zero, which a consumer reads as a reset.
	retiredAt map[edgeKey]time.Time
	now       func() time.Time
	// limit caps each of the socket maps; evicted counts what the cap dropped.
	limit   int
	evicted uint64
}

// maxTrackedSockets bounds the per-socket maps whatever the socket churn.
const maxTrackedSockets = 65536

// totalsRetention bounds how long a retired edge's totals are kept: longer than
// the miss tolerance of the debounce at any sane cadence, short enough that
// the registry stays small on a host whose peers keep changing.
const totalsRetention = time.Hour

func newEdgeRegistry() *edgeRegistry {
	return &edgeRegistry{
		byTuple:       map[tupleKey]edgeKey{},
		retiredTuples: map[tupleKey]edgeKey{},
		edges:         map[edgeKey]struct{}{},
		totals:        map[edgeKey]*edgeTotals{},
		socks:         map[sockKey]sockState{},

		retiredAt: map[edgeKey]time.Time{},
		now:       time.Now,
		limit:     maxTrackedSockets,
	}
}

// sharedEdges is the registry the production source publishes to and
// CollectEdgeMetrics reads; one per process because there is one host.
var sharedEdges = newEdgeRegistry()

// EdgeEvictions is how many socket entries the size cap has dropped so far; an
// evicted socket may be counted again from its full value when next seen.
func EdgeEvictions() uint64 {
	sharedEdges.mu.Lock()
	defer sharedEdges.mu.Unlock()
	return sharedEdges.evicted
}

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
// An edge that is no longer emitted reports nothing; its totals are kept for
// totalsRetention, then purged, so a returning edge resumes its counters.
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
	retiredTuples := map[tupleKey]edgeKey{}
	for t, k := range r.byTuple {
		retiredTuples[t] = k
	}
	for t, k := range r.retiredTuples {
		retiredTuples[t] = k
	}
	for t := range byTuple {
		delete(retiredTuples, t)
	}
	r.byTuple = byTuple
	r.edges = edges
	now := r.now()
	for k := range r.totals {
		if _, ok := edges[k]; ok {
			delete(r.retiredAt, k)
			continue
		}
		since, retired := r.retiredAt[k]
		if !retired {
			r.retiredAt[k] = now
			continue
		}
		if now.Sub(since) > totalsRetention {
			delete(r.totals, k)
			delete(r.retiredAt, k)
		}
	}
	for t, k := range retiredTuples {
		if _, kept := r.totals[k]; !kept {
			delete(retiredTuples, t)
		}
	}
	r.evicted += capMap(retiredTuples, r.limit)
	r.retiredTuples = retiredTuples
}

func (r *edgeRegistry) collect(conns []netconn.Conn) []EdgeMetric {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.edges) == 0 && len(r.retiredTuples) == 0 {
		r.socks = map[sockKey]sockState{}
		return nil
	}

	type rttSum struct {
		sum time.Duration
		n   int
	}
	rtts := map[edgeKey]rttSum{}
	stillThere := map[tupleKey]edgeKey{}
	seen := make(map[sockKey]sockState, len(r.socks))

	for _, c := range conns {
		t := tupleKey{
			laddr: c.LocalAddr.Addr().Unmap().String(), lport: uint32(c.LocalAddr.Port()),
			raddr: c.RemoteAddr.Addr().Unmap().String(), rport: uint32(c.RemoteAddr.Port()),
		}
		ek, live := r.byTuple[t]
		if !live {
			var retired bool
			if ek, retired = r.retiredTuples[t]; !retired {
				continue
			}
			stillThere[t] = ek
		}
		sk := sockKey{inode: c.Inode}
		if c.Inode == 0 {
			sk.tuple = t
		}
		prev, known := r.socks[sk]
		if c.State != netconn.StateEstablished || !c.HasInfo {
			// Still in the table, only not readable this cycle: keep its
			// baseline, or its whole value would be added again when it is.
			if known {
				seen[sk] = prev
			}
			continue
		}
		cur := sockState{retrans: uint64(c.Retransmits), sent: c.BytesSent, received: c.BytesReceived}
		if !live {
			// Not attributed now (edge retired, or between two attributions):
			// keep the baseline untouched so the growth in between is added
			// once when the socket is attributed again.
			if known {
				seen[sk] = prev
			}
			continue
		}
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
	r.evicted += capMap(r.socks, r.limit)
	// A retired edge's socket that left the table can never come back.
	r.retiredTuples = stillThere

	out := make([]EdgeMetric, 0, len(r.totals))
	for ek, tot := range r.totals {
		if _, live := r.edges[ek]; !live {
			continue
		}
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

// capMap evicts entries until m holds at most limit, and returns how many it
// dropped. Which ones go is arbitrary: past the cap the host is churning more
// sockets than we are willing to remember, and the only cost of forgetting one
// is counting it again from its full value.
func capMap[K comparable, V any](m map[K]V, limit int) uint64 {
	var n uint64
	for k := range m {
		if len(m) <= limit {
			break
		}
		delete(m, k)
		n++
	}
	return n
}
