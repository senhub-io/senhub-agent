//go:build linux

package hostiface

import (
	"encoding/binary"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func rtAttr(typ uint16, val []byte) []byte {
	l := 4 + len(val)
	aligned := (l + 3) &^ 3
	out := make([]byte, aligned)
	binary.LittleEndian.PutUint16(out[0:2], uint16(l))
	binary.LittleEndian.PutUint16(out[2:4], typ)
	copy(out[4:], val)
	return out
}

func rtAttrString(typ uint16, s string) []byte {
	return rtAttr(typ, append([]byte(s), 0))
}

func rtAttrU32(typ uint16, v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return rtAttr(typ, b)
}

func linkMsg(name string, index, master int, up bool, attrs ...[]byte) syscall.NetlinkMessage {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[4:8], uint32(index))
	if up {
		binary.LittleEndian.PutUint32(data[8:12], unix.IFF_UP)
	}
	for _, a := range attrs {
		data = append(data, a...)
	}
	return syscall.NetlinkMessage{Header: syscall.NlMsghdr{Type: unix.RTM_NEWLINK}, Data: data}
}

func TestParseAggregationBond(t *testing.T) {
	bondInfoData := rtAttr(unix.IFLA_BOND_MODE, []byte{4})
	bondLinkInfo := rtAttr(unix.IFLA_LINKINFO, append(rtAttrString(unix.IFLA_INFO_KIND, "bond"), rtAttr(unix.IFLA_INFO_DATA, bondInfoData)...))
	bond0 := linkMsg("bond0", 10, 0, true,
		rtAttrString(unix.IFLA_IFNAME, "bond0"),
		bondLinkInfo,
	)

	eth0SlaveData := rtAttr(unix.IFLA_BOND_SLAVE_STATE, []byte{0})
	eth0LinkInfo := rtAttr(unix.IFLA_LINKINFO, rtAttr(unix.IFLA_INFO_SLAVE_DATA, eth0SlaveData))
	eth0 := linkMsg("eth0", 2, 10, true,
		rtAttrString(unix.IFLA_IFNAME, "eth0"),
		rtAttrU32(unix.IFLA_MASTER, 10),
		eth0LinkInfo,
	)

	eth1SlaveData := rtAttr(unix.IFLA_BOND_SLAVE_STATE, []byte{1})
	eth1LinkInfo := rtAttr(unix.IFLA_LINKINFO, rtAttr(unix.IFLA_INFO_SLAVE_DATA, eth1SlaveData))
	eth1 := linkMsg("eth1", 3, 10, true,
		rtAttrString(unix.IFLA_IFNAME, "eth1"),
		rtAttrU32(unix.IFLA_MASTER, 10),
		eth1LinkInfo,
	)

	links := parseLinkMessages([]syscall.NetlinkMessage{bond0, eth0, eth1})
	agg := aggregationByInterface(links)

	if got := agg["bond0"].Mode; got != "lacp" {
		t.Errorf("bond0 mode = %q, want lacp", got)
	}
	if got := agg["eth0"].MasterName; got != "bond0" {
		t.Errorf("eth0 master = %q, want bond0", got)
	}
	if got := agg["eth0"].State; got != "active" {
		t.Errorf("eth0 state = %q, want active", got)
	}
	if got := agg["eth1"].State; got != "standby" {
		t.Errorf("eth1 state = %q, want standby", got)
	}
}

func TestParseAggregationBridgeMasterIsNotAggregation(t *testing.T) {
	br0 := linkMsg("br0", 10, 0, true,
		rtAttrString(unix.IFLA_IFNAME, "br0"),
		rtAttr(unix.IFLA_LINKINFO, rtAttrString(unix.IFLA_INFO_KIND, "bridge")),
	)
	veth0 := linkMsg("veth0", 2, 10, true,
		rtAttrString(unix.IFLA_IFNAME, "veth0"),
		rtAttrU32(unix.IFLA_MASTER, 10),
	)

	links := parseLinkMessages([]syscall.NetlinkMessage{br0, veth0})
	agg := aggregationByInterface(links)

	if len(agg) != 0 {
		t.Errorf("bridge master must not produce aggregation metadata: %+v", agg)
	}
}

func TestParseAggregationTeam(t *testing.T) {
	team0 := linkMsg("team0", 10, 0, true,
		rtAttrString(unix.IFLA_IFNAME, "team0"),
		rtAttr(unix.IFLA_LINKINFO, rtAttrString(unix.IFLA_INFO_KIND, "team")),
	)
	eth0 := linkMsg("eth0", 2, 10, true,
		rtAttrString(unix.IFLA_IFNAME, "eth0"),
		rtAttrU32(unix.IFLA_MASTER, 10),
	)
	eth1 := linkMsg("eth1", 3, 10, false,
		rtAttrString(unix.IFLA_IFNAME, "eth1"),
		rtAttrU32(unix.IFLA_MASTER, 10),
	)

	links := parseLinkMessages([]syscall.NetlinkMessage{team0, eth0, eth1})
	agg := aggregationByInterface(links)

	if got := agg["team0"].Mode; got != "static" {
		t.Errorf("team0 mode = %q, want static", got)
	}
	if got := agg["eth0"].State; got != "" {
		t.Errorf("team member eth0 state = %q, want absent (no backup notion)", got)
	}
	if got := agg["eth1"].State; got != "" {
		t.Errorf("team member eth1 state = %q, want absent (no backup notion)", got)
	}
}

// TestParseAggregationBalanceBondHasNoMemberState checks a balance-xor bond
// (no backup notion) leaves its members' state absent even though the kernel
// reports a slave state, and that a bond member without slave data is not
// read as active.
func TestParseAggregationBalanceBondHasNoMemberState(t *testing.T) {
	bond0 := linkMsg("bond0", 10, 0, true,
		rtAttrString(unix.IFLA_IFNAME, "bond0"),
		rtAttr(unix.IFLA_LINKINFO, append(rtAttrString(unix.IFLA_INFO_KIND, "bond"),
			rtAttr(unix.IFLA_INFO_DATA, rtAttr(unix.IFLA_BOND_MODE, []byte{2}))...)),
	)
	eth0 := linkMsg("eth0", 2, 10, true,
		rtAttrString(unix.IFLA_IFNAME, "eth0"),
		rtAttrU32(unix.IFLA_MASTER, 10),
		rtAttr(unix.IFLA_LINKINFO, rtAttr(unix.IFLA_INFO_SLAVE_DATA, rtAttr(unix.IFLA_BOND_SLAVE_STATE, []byte{0}))),
	)
	agg := aggregationByInterface(parseLinkMessages([]syscall.NetlinkMessage{bond0, eth0}))
	if got := agg["bond0"].Mode; got != "static" {
		t.Errorf("balance-xor mode = %q, want static", got)
	}
	if got := agg["eth0"].State; got != "" {
		t.Errorf("balance-xor member state = %q, want absent", got)
	}

	lacp := linkMsg("bond1", 20, 0, true,
		rtAttrString(unix.IFLA_IFNAME, "bond1"),
		rtAttr(unix.IFLA_LINKINFO, append(rtAttrString(unix.IFLA_INFO_KIND, "bond"),
			rtAttr(unix.IFLA_INFO_DATA, rtAttr(unix.IFLA_BOND_MODE, []byte{4}))...)),
	)
	eth2 := linkMsg("eth2", 4, 20, true,
		rtAttrString(unix.IFLA_IFNAME, "eth2"),
		rtAttrU32(unix.IFLA_MASTER, 20),
	)
	agg = aggregationByInterface(parseLinkMessages([]syscall.NetlinkMessage{lacp, eth2}))
	if got := agg["eth2"].State; got != "" {
		t.Errorf("member without slave data state = %q, want absent", got)
	}
}
