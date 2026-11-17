package hostdep

import "net/netip"

type tupleKey struct {
	laddr string
	lport uint32
	raddr string
	rport uint32
}

// uidView is one scrape's owner view: the uid of each socket by its four-tuple, and the
// executable name of every uid whose processes all share one name.
type uidView struct {
	uidByTuple map[tupleKey]uint32
	nameByUID  map[uint32]string
}

// nameFor returns the unambiguous owner name for a socket four-tuple. It is
// nil-safe and reports false when the socket has no uid or its uid is ambiguous.
func (v *uidView) nameFor(laddr string, lport uint32, raddr string, rport uint32) (string, bool) {
	if v == nil {
		return "", false
	}
	uid, ok := v.uidByTuple[tupleKey{laddr: laddr, lport: lport, raddr: raddr, rport: rport}]
	if !ok {
		return "", false
	}
	name, ok := v.nameByUID[uid]
	return name, ok
}

type procOwner struct {
	UID  uint32
	Name string
}

// univoqueNames returns the executable name for each uid whose processes all share
// the same non-empty name. UIDs with empty or mixed names are omitted.
func univoqueNames(procs []procOwner) map[uint32]string {
	type state struct {
		name      string
		set       bool
		ambiguous bool
	}
	states := make(map[uint32]state)
	for _, p := range procs {
		st := states[p.UID]
		if !st.set {
			st = state{name: p.Name, set: true}
		} else if p.Name != st.name {
			st.ambiguous = true
			st.name = ""
		}
		states[p.UID] = st
	}
	out := make(map[uint32]string, len(states))
	for uid, st := range states {
		if st.set && !st.ambiguous && st.name != "" {
			out[uid] = st.name
		}
	}
	return out
}

// normalizeIP parses s and returns the unmapped string form. Invalid addresses are
// returned unchanged so callers feeding raw gopsutil or sock_diag strings still match.
func normalizeIP(s string) string {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	return addr.Unmap().String()
}
