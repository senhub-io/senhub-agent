package ebpfhelper

import (
	"container/list"
	"sync"
	"time"

	"senhub-agent.go/internal/ebpfproto"
)

// Default bounds of a Tracker.
const (
	DefaultMaxConns  = 16384
	DefaultClosedTTL = 10 * time.Minute
)

type record struct {
	skaddr      uint64
	conn        ebpfproto.Conn
	closedAt    time.Time
	closed      bool
	hasTuple    bool
	lruPosition *list.Element
}

// Tracker keeps the connections the kernel programs report, within hard caps.
//
// Live connections are bounded by an LRU of max entries: a host that opens more
// connections than that loses the least recently active ones, counted in
// Evicted, never memory. A closed connection is kept for closedTTL so that a
// short-lived connection is still there when the agent next asks, then goes.
type Tracker struct {
	mu        sync.Mutex
	max       int
	closedTTL time.Duration
	now       func() time.Time

	byAddr map[uint64]*record
	lru    *list.List // front = most recently touched

	events  uint64
	evicted uint64
}

// NewTracker returns a Tracker holding at most max connections. Non-positive
// arguments select the defaults.
func NewTracker(max int, closedTTL time.Duration) *Tracker {
	if max <= 0 {
		max = DefaultMaxConns
	}
	if closedTTL <= 0 {
		closedTTL = DefaultClosedTTL
	}
	return &Tracker{
		max:       max,
		closedTTL: closedTTL,
		now:       time.Now,
		byAddr:    make(map[uint64]*record),
		lru:       list.New(),
	}
}

// Apply folds one kernel event into the table.
func (t *Tracker) Apply(ev Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events++

	if ev.Kind == KindState && ev.NewState == StateListen {
		return
	}
	r := t.byAddr[ev.SkAddr]
	// A socket structure is reused by the kernel once freed, so an event for a
	// closed record, or a fresh connect, starts a new connection.
	if r != nil && (r.closed || (ev.Kind == KindState && ev.NewState == StateSynSent)) {
		t.remove(r)
		r = nil
	}
	if r == nil {
		r = &record{skaddr: ev.SkAddr}
		r.lruPosition = t.lru.PushFront(r)
		t.byAddr[ev.SkAddr] = r
		for t.lru.Len() > t.max {
			t.remove(t.lru.Back().Value.(*record))
			t.evicted++
		}
	} else {
		t.lru.MoveToFront(r.lruPosition)
	}

	if ev.Sport != 0 && ev.Dport != 0 {
		r.hasTuple = true
		r.conn.LocalPort, r.conn.RemotePort = ev.Sport, ev.Dport
		r.conn.LocalAddr, r.conn.RemoteAddr = ev.Saddr.String(), ev.Daddr.String()
		if ev.Family == 10 {
			r.conn.Family = 6
		} else {
			r.conn.Family = 4
		}
	}
	switch ev.Kind {
	case KindRetrans:
		r.conn.Retransmits++
	case KindState:
		r.conn.State = StateName(ev.NewState)
		if ev.NewState == StateSynSent && ev.TGID != 0 {
			r.conn.PID = ev.TGID
			r.conn.Comm = ev.Comm
			r.conn.StartedMS = t.now().UnixMilli()
		}
		if ev.NewState == StateClose {
			r.closed = true
			r.closedAt = t.now()
		}
	}
}

func (t *Tracker) remove(r *record) {
	t.lru.Remove(r.lruPosition)
	delete(t.byAddr, r.skaddr)
}

// Snapshot returns the connections whose four-tuple is known, dropping closed
// ones older than the retention. It also returns how many connections the cap
// has evicted and how many are tracked.
func (t *Tracker) Snapshot() (conns []ebpfproto.Conn, evicted uint64, tracked int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	conns = make([]ebpfproto.Conn, 0, len(t.byAddr))
	for _, r := range t.byAddr {
		if r.closed && now.Sub(r.closedAt) > t.closedTTL {
			t.remove(r)
			continue
		}
		if !r.hasTuple {
			continue
		}
		c := r.conn
		c.Closed = r.closed
		conns = append(conns, c)
	}
	return conns, t.evicted, len(t.byAddr)
}

// Len returns the number of tracked connections.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.byAddr)
}

// Events returns how many kernel events have been applied.
func (t *Tracker) Events() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events
}
