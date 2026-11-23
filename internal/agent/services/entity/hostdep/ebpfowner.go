package hostdep

import (
	"net/netip"

	"senhub-agent.go/internal/ebpfproto"
)

// EBPFSource is the optional view of the senhub-ebpf helper. It is consulted
// only for what the unprivileged socket read cannot say: the process behind a
// socket owned by another user, and the retransmissions the kernel counted.
// A nil source, or one that fails, leaves every result exactly as without it.
type EBPFSource interface {
	Connections() ([]ebpfproto.Conn, error)
}

// pidReuseSlackMS is how much later than the helper saw a connection begin a
// process may have started and still be its owner: clocks of the helper and of
// the process start time are read at different moments.
const pidReuseSlackMS = 2000

type ebpfOwner struct {
	pid       int32
	startedMS int64
	retrans   uint64
	closed    bool
}

// ebpfView is one scrape's snapshot of the helper, by four-tuple.
type ebpfView struct {
	byTuple map[tupleKey]ebpfOwner
}

func newEBPFView(conns []ebpfproto.Conn) *ebpfView {
	v := &ebpfView{byTuple: make(map[tupleKey]ebpfOwner, len(conns))}
	for _, c := range conns {
		l, errL := netip.ParseAddr(c.LocalAddr)
		r, errR := netip.ParseAddr(c.RemoteAddr)
		if errL != nil || errR != nil {
			continue
		}
		t := tupleKey{
			laddr: l.Unmap().String(), lport: uint32(c.LocalPort),
			raddr: r.Unmap().String(), rport: uint32(c.RemotePort),
		}
		// A closed connection's tuple may be reused by a live one; the live
		// one is the one the socket table shows.
		if _, seen := v.byTuple[t]; seen && c.Closed {
			continue
		}
		v.byTuple[t] = ebpfOwner{pid: int32(c.PID), startedMS: c.StartedMS, retrans: c.Retransmits, closed: c.Closed}
	}
	return v
}

// owner returns the pid the helper saw connect this socket. It is nil-safe and
// reports false when the helper did not see the connection start.
func (v *ebpfView) owner(laddr string, lport uint32, raddr string, rport uint32) (ebpfOwner, bool) {
	if v == nil {
		return ebpfOwner{}, false
	}
	o, ok := v.byTuple[tupleKey{laddr: laddr, lport: lport, raddr: raddr, rport: rport}]
	if !ok || o.pid == 0 {
		return ebpfOwner{}, false
	}
	return o, true
}

// retransmits returns the helper's per-socket retransmission counts, or nil.
func (v *ebpfView) retransmits() map[tupleKey]uint64 {
	if v == nil {
		return nil
	}
	out := make(map[tupleKey]uint64, len(v.byTuple))
	for t, o := range v.byTuple {
		// A closed connection's count must not land on a new one reusing the tuple.
		if o.retrans > 0 && !o.closed {
			out[t] = o.retrans
		}
	}
	return out
}

// SetEBPF registers the helper view. onError, when non-nil, receives every
// failure other than the helper being absent. Call before Observe.
func (s *Source) SetEBPF(src EBPFSource, onError func(error)) {
	s.ebpf = src
	s.onEBPFError = onError
}

// fetchEBPF reads the helper once per scrape. Any failure yields nil.
func (s *Source) fetchEBPF() *ebpfView {
	if s.ebpf == nil {
		return nil
	}
	conns, err := s.ebpf.Connections()
	if err != nil {
		if s.onEBPFError != nil {
			s.onEBPFError(err)
		}
		return nil
	}
	return newEBPFView(conns)
}

// ebpfName names the owner the helper saw, with the same function every other
// path uses. It reports false when the process has exited, was started after
// the connection (a recycled pid), or cannot be named.
func (s *Source) ebpfName(o ebpfOwner, nameFn func(int32) string, createdFn func(int32) (int64, bool), priorLRU, nextLRU map[int32]nameEntry) (string, bool) {
	created, ok := createdFn(o.pid)
	if !ok {
		return "", false
	}
	if o.startedMS != 0 && created > o.startedMS+pidReuseSlackMS {
		return "", false
	}
	name := cachedName(o.pid, nameFn, createdFn, priorLRU, nextLRU)
	return name, name != ""
}
