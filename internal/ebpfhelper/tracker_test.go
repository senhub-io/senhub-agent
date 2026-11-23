package ebpfhelper

import (
	"net/netip"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestTracker(max int, ttl time.Duration) (*Tracker, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	tr := NewTracker(max, ttl)
	tr.now = c.now
	return tr, c
}

func state(sk uint64, old, new uint8, sport, dport uint16, tgid uint32, comm string) Event {
	return Event{
		SkAddr: sk, Kind: KindState, OldState: old, NewState: new,
		Sport: sport, Dport: dport, Family: 2, TGID: tgid, Comm: comm,
		Saddr: netip.MustParseAddr("10.0.0.5"), Daddr: netip.MustParseAddr("10.0.0.9"),
	}
}

func retrans(sk uint64, sport, dport uint16) Event {
	return Event{
		SkAddr: sk, Kind: KindRetrans, Sport: sport, Dport: dport, Family: 2,
		Saddr: netip.MustParseAddr("10.0.0.5"), Daddr: netip.MustParseAddr("10.0.0.9"),
	}
}

func TestConnectEstablishRetransmitClose(t *testing.T) {
	tr, clk := newTestTracker(0, time.Minute)
	// The connecting process is known at SYN_SENT, before the source port is.
	tr.Apply(state(1, StateClose, StateSynSent, 0, 5432, 4242, "psql"))
	if conns, _, _ := tr.Snapshot(); len(conns) != 0 {
		t.Fatalf("a connection without a four-tuple must not be reported: %+v", conns)
	}
	tr.Apply(state(1, StateSynSent, StateEstablished, 41234, 5432, 0, ""))
	tr.Apply(retrans(1, 41234, 5432))
	tr.Apply(retrans(1, 41234, 5432))

	conns, _, tracked := tr.Snapshot()
	if len(conns) != 1 || tracked != 1 {
		t.Fatalf("want one connection, got %+v (%d tracked)", conns, tracked)
	}
	c := conns[0]
	if c.PID != 4242 || c.Comm != "psql" || c.LocalPort != 41234 || c.RemotePort != 5432 ||
		c.State != "ESTABLISHED" || c.Retransmits != 2 || c.Closed || c.Family != 4 ||
		c.LocalAddr != "10.0.0.5" || c.RemoteAddr != "10.0.0.9" || c.StartedMS != clk.t.UnixMilli() {
		t.Fatalf("unexpected connection %+v", c)
	}

	tr.Apply(state(1, StateEstablished, StateClose, 41234, 5432, 0, ""))
	conns, _, _ = tr.Snapshot()
	if len(conns) != 1 || !conns[0].Closed || conns[0].PID != 4242 {
		t.Fatalf("a just-closed connection stays visible with its owner: %+v", conns)
	}

	clk.t = clk.t.Add(2 * time.Minute)
	if conns, _, tracked := tr.Snapshot(); len(conns) != 0 || tracked != 0 {
		t.Fatalf("a closed connection must expire: %+v (%d tracked)", conns, tracked)
	}
}

func TestAcceptedConnectionHasNoOwner(t *testing.T) {
	tr, _ := newTestTracker(0, time.Minute)
	// Accept-side transitions run in softirq: the kernel programs send no owner.
	tr.Apply(state(2, StateSynRecv, StateEstablished, 8080, 51000, 0, ""))
	conns, _, _ := tr.Snapshot()
	if len(conns) != 1 || conns[0].PID != 0 || conns[0].Comm != "" {
		t.Fatalf("unexpected %+v", conns)
	}
}

func TestSocketReuseStartsANewConnection(t *testing.T) {
	tr, _ := newTestTracker(0, time.Minute)
	tr.Apply(state(3, StateClose, StateSynSent, 0, 80, 100, "old"))
	tr.Apply(state(3, StateSynSent, StateEstablished, 40000, 80, 0, ""))
	tr.Apply(retrans(3, 40000, 80))
	tr.Apply(state(3, StateEstablished, StateClose, 40000, 80, 0, ""))
	// The kernel hands the same address to a new socket.
	tr.Apply(state(3, StateClose, StateSynSent, 0, 443, 200, "new"))
	tr.Apply(state(3, StateSynSent, StateEstablished, 40001, 443, 0, ""))

	conns, _, tracked := tr.Snapshot()
	if tracked != 1 || len(conns) != 1 {
		t.Fatalf("want one record, got %+v", conns)
	}
	if c := conns[0]; c.PID != 200 || c.Comm != "new" || c.Retransmits != 0 || c.Closed || c.RemotePort != 443 {
		t.Fatalf("the new connection inherited the old one: %+v", c)
	}
}

func TestRetransmitOfUnseenConnection(t *testing.T) {
	tr, _ := newTestTracker(0, time.Minute)
	tr.Apply(retrans(4, 22, 50000))
	conns, _, _ := tr.Snapshot()
	if len(conns) != 1 || conns[0].Retransmits != 1 || conns[0].PID != 0 {
		t.Fatalf("unexpected %+v", conns)
	}
}

func TestListeningSocketsAreIgnored(t *testing.T) {
	tr, _ := newTestTracker(0, time.Minute)
	tr.Apply(state(5, StateClose, StateListen, 22, 0, 0, ""))
	if tr.Len() != 0 {
		t.Fatal("a listener is not a connection")
	}
}

func TestCapEvictsLeastRecentlyActive(t *testing.T) {
	tr, _ := newTestTracker(3, time.Minute)
	for i := uint64(1); i <= 3; i++ {
		tr.Apply(state(i, StateSynSent, StateEstablished, uint16(40000+i), 80, 0, ""))
	}
	// Touch 1 so that 2 is now the oldest.
	tr.Apply(retrans(1, 40001, 80))
	tr.Apply(state(4, StateSynSent, StateEstablished, 40004, 80, 0, ""))

	conns, evicted, tracked := tr.Snapshot()
	if tracked != 3 || evicted != 1 || len(conns) != 3 {
		t.Fatalf("tracked=%d evicted=%d conns=%d", tracked, evicted, len(conns))
	}
	for _, c := range conns {
		if c.LocalPort == 40002 {
			t.Fatal("the least recently active connection should have been evicted")
		}
	}
}

func TestCapHoldsUnderChurn(t *testing.T) {
	tr, _ := newTestTracker(100, time.Minute)
	for i := uint64(0); i < 10_000; i++ {
		tr.Apply(state(i, StateSynSent, StateEstablished, uint16(1+i%60000), 80, 0, ""))
	}
	if tr.Len() != 100 {
		t.Fatalf("len=%d, the cap must hold", tr.Len())
	}
	if _, evicted, _ := tr.Snapshot(); evicted != 9_900 {
		t.Fatalf("evicted=%d", evicted)
	}
	if tr.Events() != 10_000 {
		t.Fatalf("events=%d", tr.Events())
	}
}
