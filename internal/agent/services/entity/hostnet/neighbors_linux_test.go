//go:build linux

package hostnet

import (
	"encoding/binary"
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseNeighMessages(t *testing.T) {
	msgs := []syscall.NetlinkMessage{
		newNeighMsg(t, "2001:db8::1", "00:11:22:33:44:55", 1, unix.NUD_REACHABLE),
		newNeighMsg(t, "2001:db8::2", "00:00:00:00:00:00", 2, unix.NUD_FAILED),
	}
	got := parseNeighMessages(msgs, func(idx int) string {
		if idx == 1 {
			return "eth0"
		}
		return "eth1"
	})
	if len(got) != 1 {
		t.Fatalf("got %d neighbors, want 1: %+v", len(got), got)
	}
	if got[0].IP != "2001:db8::1" || got[0].MAC != "00:11:22:33:44:55" || got[0].Iface != "eth0" || got[0].State != "reachable" {
		t.Fatalf("unexpected neighbor: %+v", got[0])
	}
}

func newNeighMsg(t *testing.T, ipStr, macStr string, ifIndex int32, state uint16) syscall.NetlinkMessage {
	t.Helper()
	ip := net.ParseIP(ipStr)
	if ip == nil {
		t.Fatalf("bad test IP %q", ipStr)
	}
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		t.Fatalf("bad test MAC %q: %v", macStr, err)
	}

	data := make([]byte, 12)
	data[0] = unix.AF_INET6
	binary.LittleEndian.PutUint32(data[4:8], uint32(ifIndex))
	binary.LittleEndian.PutUint16(data[8:10], state)

	data = appendNetlinkAttr(data, unix.NDA_DST, ip.To16())
	data = appendNetlinkAttr(data, unix.NDA_LLADDR, mac)

	return syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: unix.RTM_NEWNEIGH},
		Data:   data,
	}
}

func appendNetlinkAttr(d []byte, typ uint16, val []byte) []byte {
	var hdr [4]byte
	binary.LittleEndian.PutUint16(hdr[0:2], uint16(4+len(val)))
	binary.LittleEndian.PutUint16(hdr[2:4], typ)
	d = append(d, hdr[:]...)
	d = append(d, val...)
	for len(d)%4 != 0 {
		d = append(d, 0)
	}
	return d
}
