package hostdep

import (
	"errors"
	"os"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/netconn"
	"senhub-agent.go/internal/ebpfproto"
)

type fakeEBPF struct {
	conns []ebpfproto.Conn
	err   error
	calls int
}

func (f *fakeEBPF) Connections() ([]ebpfproto.Conn, error) {
	f.calls++
	return f.conns, f.err
}

// otherUsersSource is a source whose socket owners the unprivileged read cannot
// see (pid 0) and whose uid view is ambiguous, so only the helper can name them.
func otherUsersSource(rows []gnet.ConnectionStat) *Source {
	s := newTestSource(rows)
	s.threshold = 1
	s.edges = newEdgeRegistry()
	s.uidView = func() *uidView { return &uidView{} }
	s.agentID = func() string { return "" }
	s.selfPID = func() int32 { return -1 }
	s.ephemeral = func() (uint32, uint32) { return 60000, 61000 }
	s.procName = func(pid int32) string {
		switch pid {
		case 4242:
			return "psql"
		case 4243:
			return "curl"
		}
		return ""
	}
	s.procCreated = func(pid int32) (int64, bool) {
		if pid == 4242 || pid == 4243 {
			return time.Now().Add(-time.Hour).UnixMilli(), true
		}
		return 0, false
	}
	return s
}

func helperConn(pid uint32, lport, rport uint16, retrans uint64) ebpfproto.Conn {
	return ebpfproto.Conn{
		PID: pid, Comm: "x", Family: 4, LocalAddr: "10.0.0.5", RemoteAddr: "10.0.0.9",
		LocalPort: lport, RemotePort: rport, State: ebpfproto.StateEstablished, Retransmits: retrans,
		StartedMS: time.Now().Add(-30 * time.Minute).UnixMilli(),
	}
}

func twoUserRows() []gnet.ConnectionStat {
	return []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 0),
		conn(statusEstablished, "10.0.0.5", 51001, "10.0.0.10", 443, 0),
	}
}

func TestEBPFNamesSocketsTheUIDCannot(t *testing.T) {
	s := otherUsersSource(twoUserRows())
	s.SetEBPF(&fakeEBPF{conns: []ebpfproto.Conn{
		helperConn(4242, 51000, 5432, 0),
		{PID: 4243, Family: 4, LocalAddr: "10.0.0.5", RemoteAddr: "10.0.0.10", LocalPort: 51001, RemotePort: 443,
			StartedMS: time.Now().Add(-time.Minute).UnixMilli()},
	}}, nil)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false: the sockets should have been attributed")
	}
	// The same key shape as the other attribution paths: <name>@<host.id>.
	if !hasServiceInstance(obs, "psql@h-1") || !hasServiceInstance(obs, "curl@h-1") {
		t.Fatalf("entities = %+v", obs.Entities)
	}
	if relCount(obs, relDependsOn) != 2 {
		t.Fatalf("depends_on edges = %d", relCount(obs, relDependsOn))
	}
}

func TestEBPFPrefersTheUIDViewWhenItNamesTheSocket(t *testing.T) {
	s := otherUsersSource(twoUserRows()[:1])
	s.uidView = func() *uidView {
		return &uidView{
			uidByTuple: map[tupleKey]uint32{{"10.0.0.5", 51000, "10.0.0.9", 5432}: 1000},
			nameByUID:  map[uint32]string{1000: "postgres"},
		}
	}
	s.SetEBPF(&fakeEBPF{conns: []ebpfproto.Conn{helperConn(4242, 51000, 5432, 0)}}, nil)
	obs, _ := s.Observe()
	if !hasServiceInstance(obs, "postgres@h-1") || hasServiceInstance(obs, "psql@h-1") {
		t.Fatalf("the uid attribution must win: %+v", obs.Entities)
	}
}

func hasServiceInstance(obs entity.Observation, id string) bool {
	for _, e := range obs.Entities {
		if e.Type == entityTypeServiceInstance && e.ID[idKeyServiceInstanceID] == id {
			return true
		}
	}
	return false
}

func TestEBPFExitedProcessEmitsNothing(t *testing.T) {
	s := otherUsersSource(twoUserRows()[:1])
	s.SetEBPF(&fakeEBPF{conns: []ebpfproto.Conn{helperConn(9999, 51000, 5432, 0)}}, nil)
	obs, ok := s.Observe()
	if ok {
		t.Fatalf("a socket nobody can name leaves the view blind, got %+v", obs)
	}
}

func TestEBPFRecycledPidIsRejected(t *testing.T) {
	s := otherUsersSource(twoUserRows()[:1])
	// The process now holding pid 4242 started after the connection began.
	s.procCreated = func(int32) (int64, bool) { return time.Now().UnixMilli(), true }
	c := helperConn(4242, 51000, 5432, 0)
	c.StartedMS = time.Now().Add(-10 * time.Minute).UnixMilli()
	s.SetEBPF(&fakeEBPF{conns: []ebpfproto.Conn{c}}, nil)
	if _, ok := s.Observe(); ok {
		t.Fatal("a recycled pid must not name the connection")
	}
}

func TestAbsentOrFailingHelperChangesNothing(t *testing.T) {
	baseline := otherUsersSource(twoUserRows())
	_, okBase := baseline.Observe()

	var reported []error
	failing := otherUsersSource(twoUserRows())
	failing.SetEBPF(&fakeEBPF{err: errors.New("boom")}, func(err error) { reported = append(reported, err) })
	_, okFail := failing.Observe()

	if okBase != okFail || len(reported) != 1 {
		t.Fatalf("baseline=%v failing=%v reported=%v", okBase, okFail, reported)
	}

	absent := otherUsersSource(twoUserRows())
	absent.SetEBPF(&fakeEBPF{err: os.ErrNotExist}, nil)
	if _, ok := absent.Observe(); ok != okBase {
		t.Fatal("an absent helper must behave like none")
	}
}

func TestEBPFHelperUntouchedWhenNothingToAttribute(t *testing.T) {
	// The helper is read once per scrape even when the sockets are all readable:
	// its retransmit counts are useful on their own.
	f := &fakeEBPF{}
	s := newTestSource([]gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100)})
	s.SetEBPF(f, nil)
	s.Observe()
	if f.calls != 1 {
		t.Fatalf("calls=%d", f.calls)
	}
}

func TestEBPFRetransmitsFeedTheEdgeMetric(t *testing.T) {
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 51000, "10.0.0.9", 5432, 100)}
	s, reg := edgeSource(rows)
	f := &fakeEBPF{conns: []ebpfproto.Conn{helperConn(100, 51000, 5432, 7)}}
	s.SetEBPF(f, nil)
	observeOK(t, s)

	// tcp_info saw 2 retransmissions, the helper 7: the larger count stands.
	ms := reg.collect([]netconn.Conn{nc("10.0.0.5", 51000, "10.0.0.9", 5432, 1, time.Millisecond, 2, 10, 20)})
	m := metricFor(ms, "10.0.0.9")
	if m == nil || m.Retransmits != 7 {
		t.Fatalf("%+v", ms)
	}

	// The helper's count grows to 9, tcp_info stays: the total follows, never double counts.
	f.conns = []ebpfproto.Conn{helperConn(100, 51000, 5432, 9)}
	observeOK(t, s)
	ms = reg.collect([]netconn.Conn{nc("10.0.0.5", 51000, "10.0.0.9", 5432, 1, time.Millisecond, 2, 10, 20)})
	if m := metricFor(ms, "10.0.0.9"); m == nil || m.Retransmits != 9 {
		t.Fatalf("%+v", ms)
	}

	// Without the helper the tcp_info count is what it was: the baseline is kept.
	f.err = os.ErrNotExist
	observeOK(t, s)
	ms = reg.collect([]netconn.Conn{nc("10.0.0.5", 51000, "10.0.0.9", 5432, 1, time.Millisecond, 3, 10, 20)})
	if m := metricFor(ms, "10.0.0.9"); m == nil || m.Retransmits != 9 {
		t.Fatalf("%+v", ms)
	}
}

func TestEBPFViewTuplesAreNormalised(t *testing.T) {
	v := newEBPFView([]ebpfproto.Conn{
		{PID: 5, Family: 6, LocalAddr: "::ffff:10.0.0.5", RemoteAddr: "::ffff:10.0.0.9", LocalPort: 1, RemotePort: 2, StartedMS: 1},
		{PID: 0, Family: 4, LocalAddr: "garbage", RemoteAddr: "10.0.0.9", LocalPort: 1, RemotePort: 2},
	})
	if _, ok := v.owner("10.0.0.5", 1, "10.0.0.9", 2); !ok {
		t.Fatal("a v4-mapped address must match its v4 form")
	}
	var nilView *ebpfView
	if _, ok := nilView.owner("a", 1, "b", 2); ok || nilView.retransmits() != nil {
		t.Fatal("nil view")
	}
}

func TestEBPFClosedConnectionDoesNotShadowALiveOne(t *testing.T) {
	live := helperConn(111, 51000, 5432, 0)
	closed := helperConn(222, 51000, 5432, 0)
	closed.Closed = true
	for _, order := range [][]ebpfproto.Conn{{live, closed}, {closed, live}} {
		o, ok := newEBPFView(order).owner("10.0.0.5", 51000, "10.0.0.9", 5432)
		if !ok || o.pid != 111 {
			t.Fatalf("owner=%+v ok=%v", o, ok)
		}
	}
}

func TestEBPFClosedConnectionRetransmitsAreNotReused(t *testing.T) {
	c := helperConn(111, 51000, 5432, 40)
	c.Closed = true
	if got := newEBPFView([]ebpfproto.Conn{c}).retransmits(); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}
