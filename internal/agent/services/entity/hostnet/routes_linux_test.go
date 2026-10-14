//go:build linux

package hostnet

import (
	"encoding/binary"
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func routeMsg(family, table, proto, scope, typ, dstLen byte) []byte {
	b := make([]byte, 12)
	b[0] = family
	b[1] = dstLen
	b[4] = table
	b[5] = proto
	b[6] = scope
	b[7] = typ
	return b
}

func routeAttr(typ uint16, val []byte) []byte {
	l := 4 + len(val)
	b := make([]byte, (l+3)&^3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(l))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	copy(b[4:], val)
	return b
}

func u32RouteAttr(typ uint16, v uint32) []byte {
	val := make([]byte, 4)
	binary.LittleEndian.PutUint32(val, v)
	return routeAttr(typ, val)
}

func nextHopRecord(ifindex uint32, gateway []byte) []byte {
	inner := routeAttr(unix.RTA_GATEWAY, gateway)
	total := 8 + len(inner)
	b := make([]byte, total)
	binary.LittleEndian.PutUint16(b[0:2], uint16(total))
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	copy(b[8:], inner)
	return b
}

func newRoute(typ uint16, data []byte) syscall.NetlinkMessage {
	return syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: uint16(typ)},
		Data:   data,
	}
}

func TestParseRouteMessages_DefaultRouteWithGateway(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET), byte(unix.RT_TABLE_MAIN), byte(unix.RTPROT_BOOT), 0, byte(unix.RTN_UNICAST), 0)
	data = append(data, routeAttr(unix.RTA_GATEWAY, net.IPv4(192, 168, 1, 1).To4())...)
	data = append(data, u32RouteAttr(unix.RTA_OIF, 2)...)
	data = append(data, u32RouteAttr(unix.RTA_PRIORITY, 100)...)

	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, func(i int) string {
		if i == 2 {
			return "eth0"
		}
		return ""
	})
	if len(got) != 1 {
		t.Fatalf("routes = %+v, want 1", got)
	}
	r := got[0]
	if r.Destination != "0.0.0.0/0" || r.NextHop != "192.168.1.1" || r.Metric != 100 || r.Iface != "eth0" || r.RouteType != routeTypeRemote {
		t.Errorf("route = %+v", r)
	}
	if r.ProtocolName != "" || r.ProtocolCode == nil || *r.ProtocolCode != int(unix.RTPROT_BOOT) {
		t.Errorf("protocol = %v code %v", r.ProtocolName, r.ProtocolCode)
	}
}

func TestParseRouteMessages_ConnectedRoute(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET), byte(unix.RT_TABLE_MAIN), byte(unix.RTPROT_BOOT), byte(unix.RT_SCOPE_LINK), byte(unix.RTN_UNICAST), 24)
	data = append(data, routeAttr(unix.RTA_DST, net.IPv4(10, 0, 0, 0).To4())...)
	data = append(data, u32RouteAttr(unix.RTA_OIF, 2)...)

	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, func(i int) string {
		if i == 2 {
			return "eth0"
		}
		return ""
	})
	if len(got) != 1 {
		t.Fatalf("routes = %+v, want 1", got)
	}
	r := got[0]
	if r.Destination != "10.0.0.0/24" || r.NextHop != "0.0.0.0" || r.Iface != "eth0" || r.RouteType != routeTypeLocal {
		t.Errorf("route = %+v", r)
	}
}

func TestParseRouteMessages_IPv6Route(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET6), byte(unix.RT_TABLE_MAIN), byte(unix.RTPROT_RA), 0, byte(unix.RTN_UNICAST), 64)
	data = append(data, routeAttr(unix.RTA_DST, net.ParseIP("2001:db8:1::").To16())...)
	data = append(data, routeAttr(unix.RTA_GATEWAY, net.ParseIP("fe80::1").To16())...)
	data = append(data, u32RouteAttr(unix.RTA_OIF, 3)...)

	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, func(i int) string {
		if i == 3 {
			return "eth1"
		}
		return ""
	})
	if len(got) != 1 {
		t.Fatalf("routes = %+v, want 1", got)
	}
	r := got[0]
	if r.Destination != "2001:db8:1::/64" || r.NextHop != "fe80::1" || r.Iface != "eth1" || r.RouteType != routeTypeRemote {
		t.Errorf("route = %+v", r)
	}
	if r.ProtocolName != "icmp" || r.ProtocolCode == nil || *r.ProtocolCode != int(unix.RTPROT_RA) {
		t.Errorf("protocol = %v code %v", r.ProtocolName, r.ProtocolCode)
	}
}

func TestParseRouteMessages_Multipath(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET), byte(unix.RT_TABLE_MAIN), byte(unix.RTPROT_BGP), 0, byte(unix.RTN_UNICAST), 0)
	mp := append(nextHopRecord(2, net.IPv4(10, 0, 0, 1).To4()), nextHopRecord(3, net.IPv4(10, 0, 0, 2).To4())...)
	data = append(data, routeAttr(unix.RTA_MULTIPATH, mp)...)

	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, func(i int) string {
		if i == 2 {
			return "eth0"
		}
		if i == 3 {
			return "eth1"
		}
		return ""
	})
	if len(got) != 2 {
		t.Fatalf("routes = %+v, want 2 multipath next hops", got)
	}
	if got[0].Destination != "0.0.0.0/0" || got[0].NextHop != "10.0.0.1" || got[0].Iface != "eth0" || got[0].RouteType != routeTypeRemote {
		t.Errorf("first route = %+v", got[0])
	}
	if got[1].Destination != "0.0.0.0/0" || got[1].NextHop != "10.0.0.2" || got[1].Iface != "eth1" || got[1].RouteType != routeTypeRemote {
		t.Errorf("second route = %+v", got[1])
	}
	if got[0].ProtocolName != "bgp" || got[0].ProtocolCode == nil || *got[0].ProtocolCode != int(unix.RTPROT_BGP) {
		t.Errorf("protocol = %v code %v", got[0].ProtocolName, got[0].ProtocolCode)
	}
}

func TestParseRouteMessages_NonMainTableIgnored(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET), byte(unix.RT_TABLE_LOCAL), byte(unix.RTPROT_BOOT), 0, byte(unix.RTN_UNICAST), 0)
	data = append(data, routeAttr(unix.RTA_GATEWAY, net.IPv4(10, 0, 0, 1).To4())...)
	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, nil)
	if len(got) != 0 {
		t.Fatalf("non-main table should be ignored, got %+v", got)
	}
}

func TestParseRouteMessages_SkipsLinkLocalIPv6Destination(t *testing.T) {
	data := routeMsg(byte(unix.AF_INET6), byte(unix.RT_TABLE_MAIN), byte(unix.RTPROT_BOOT), 0, byte(unix.RTN_UNICAST), 128)
	data = append(data, routeAttr(unix.RTA_DST, net.ParseIP("fe80::1").To16())...)
	data = append(data, routeAttr(unix.RTA_GATEWAY, net.ParseIP("2001:db8::1").To16())...)
	got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, nil)
	if len(got) != 0 {
		t.Fatalf("link-local destination should be skipped, got %+v", got)
	}
}

func TestParseRouteMessages_ProtocolMapping(t *testing.T) {
	cases := []struct {
		proto byte
		want  string
	}{
		{byte(unix.RTPROT_STATIC), "netmgmt"},
		{byte(unix.RTPROT_DHCP), "dhcp"},
		{byte(unix.RTPROT_RA), "icmp"},
		{byte(unix.RTPROT_BGP), "bgp"},
		{byte(unix.RTPROT_OSPF), "ospf"},
		{byte(unix.RTPROT_ISIS), "isis"},
		{byte(unix.RTPROT_RIP), "rip"},
		{byte(unix.RTPROT_EIGRP), "cisco_eigrp"},
		{byte(unix.RTPROT_KERNEL), "local"},
		{byte(unix.RTPROT_BOOT), ""},
	}
	for _, tc := range cases {
		data := routeMsg(byte(unix.AF_INET), byte(unix.RT_TABLE_MAIN), tc.proto, 0, byte(unix.RTN_UNICAST), 0)
		data = append(data, routeAttr(unix.RTA_GATEWAY, net.IPv4(10, 0, 0, 1).To4())...)
		got := parseRouteMessages([]syscall.NetlinkMessage{newRoute(unix.RTM_NEWROUTE, data)}, nil)
		if len(got) != 1 {
			t.Fatalf("proto %v: routes = %+v, want 1", tc.proto, got)
		}
		if got[0].ProtocolName != tc.want || got[0].ProtocolCode == nil || *got[0].ProtocolCode != int(tc.proto) {
			t.Errorf("proto %v: name = %q code %v, want name %q code %d", tc.proto, got[0].ProtocolName, got[0].ProtocolCode, tc.want, int(tc.proto))
		}
	}
}
