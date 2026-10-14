//go:build linux

package hostnet

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"senhub-agent.go/internal/agent/services/entity"
)

func platformRoutes() ([]hostRoute, error) {
	var out []hostRoute
	var firstErr error

	ifName := func(idx int) string {
		if ifc, err := net.InterfaceByIndex(idx); err == nil {
			return ifc.Name
		}
		return ""
	}

	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		rib, err := syscall.NetlinkRIB(unix.RTM_GETROUTE, family)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		msgs, err := syscall.ParseNetlinkMessage(rib)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, parseRouteMessages(msgs, ifName)...)
	}

	if len(out) == 0 && firstErr != nil {
		return nil, fmt.Errorf("netlink route dump: %w", firstErr)
	}
	return out, nil
}

type rtNexthop struct {
	Gateway string
	IfIndex int
}

func parseRouteMessages(msgs []syscall.NetlinkMessage, ifName func(int) string) []hostRoute {
	var out []hostRoute
	seen := map[string]bool{}

	for _, msg := range msgs {
		if msg.Header.Type != unix.RTM_NEWROUTE {
			continue
		}
		data := msg.Data
		if len(data) < 12 {
			continue
		}

		family := uint8(data[0])
		if family != unix.AF_INET && family != unix.AF_INET6 {
			continue
		}
		dstLen := int(data[1])
		table := uint8(data[4])
		proto := uint8(data[5])
		scope := uint8(data[6])
		typ := uint8(data[7])

		var dst, gateway, multipath []byte
		var ifindex int
		var metric int64
		routeTable := table

		attrs := data[12:]
		for len(attrs) >= 4 {
			l := int(binary.LittleEndian.Uint16(attrs[0:2]))
			atype := binary.LittleEndian.Uint16(attrs[2:4])
			if l < 4 || l > len(attrs) {
				break
			}
			val := attrs[4:l]
			switch atype {
			case unix.RTA_DST:
				dst = append([]byte(nil), val...)
			case unix.RTA_GATEWAY:
				gateway = append([]byte(nil), val...)
			case unix.RTA_OIF:
				if len(val) == 4 {
					ifindex = int(binary.LittleEndian.Uint32(val))
				}
			case unix.RTA_PRIORITY:
				if len(val) == 4 {
					metric = int64(binary.LittleEndian.Uint32(val))
				}
			case unix.RTA_TABLE:
				if len(val) >= 1 {
					routeTable = uint8(val[0])
				}
			case unix.RTA_MULTIPATH:
				multipath = append([]byte(nil), val...)
			}
			attrs = attrs[(l+3)&^3:]
		}

		if routeTable != uint8(unix.RT_TABLE_MAIN) {
			continue
		}
		if scope == unix.RT_SCOPE_HOST {
			continue
		}

		destCidr, ok := routeDestination(family, dst, dstLen)
		if !ok {
			continue
		}
		if skipRouteDestination(dst) {
			continue
		}

		nexthops := parseRtnexthops(multipath, int(family))
		gatewayStr := ""
		if len(gateway) > 0 {
			gatewayStr = net.IP(gateway).String()
			if c, ok := entity.CanonicalIP(gatewayStr); ok {
				gatewayStr = c
			}
		}

		hasGateway := len(gateway) > 0
		for _, nh := range nexthops {
			if nh.Gateway != "" {
				hasGateway = true
				break
			}
		}

		routeType, emit := linuxRouteType(typ, hasGateway)
		if !emit {
			continue
		}

		if routeType == routeTypeRemote && len(nexthops) > 0 {
			for _, nh := range nexthops {
				if nh.Gateway == "" {
					continue
				}
				nhIndex := nh.IfIndex
				if nhIndex == 0 {
					nhIndex = ifindex
				}
				r := hostRoute{
					Destination: destCidr,
					NextHop:     nh.Gateway,
					Metric:      metric,
					Iface:       routeIface(ifName, nhIndex),
					RouteType:   routeTypeRemote,
				}
				code := int(proto)
				r.ProtocolCode = &code
				r.ProtocolName = linuxProtocolName(proto)
				key := destCidr + " " + r.NextHop
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, r)
			}
			continue
		}

		nextHop := gatewayStr
		if routeType == routeTypeLocal || routeType == routeTypeReject || routeType == routeTypeBlackhole {
			nextHop = zeroNextHop(destCidr)
		}
		if routeType == routeTypeRemote && !usableNextHop(nextHop) {
			continue
		}
		if c, ok := entity.CanonicalIP(nextHop); ok {
			nextHop = c
		}
		r := hostRoute{
			Destination: destCidr,
			NextHop:     nextHop,
			Metric:      metric,
			Iface:       routeIface(ifName, ifindex),
			RouteType:   routeType,
		}
		code := int(proto)
		r.ProtocolCode = &code
		r.ProtocolName = linuxProtocolName(proto)
		key := destCidr + " " + r.NextHop
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func linuxRouteType(typ uint8, hasGateway bool) (string, bool) {
	switch typ {
	case unix.RTN_UNICAST:
		if hasGateway {
			return routeTypeRemote, true
		}
		return routeTypeLocal, true
	case unix.RTN_BLACKHOLE:
		return routeTypeBlackhole, true
	case unix.RTN_UNREACHABLE, unix.RTN_PROHIBIT:
		return routeTypeReject, true
	default:
		return "", false
	}
}

func linuxProtocolName(proto uint8) string {
	switch proto {
	case unix.RTPROT_KERNEL:
		// What the kernel adds from an interface address: every connected
		// route, and IANA's local(2) is exactly "local interface".
		return "local"
	case unix.RTPROT_STATIC:
		return "netmgmt"
	case unix.RTPROT_DHCP:
		return "dhcp"
	case unix.RTPROT_RA:
		return "icmp"
	case unix.RTPROT_BGP:
		return "bgp"
	case unix.RTPROT_OSPF:
		return "ospf"
	case unix.RTPROT_ISIS:
		return "isis"
	case unix.RTPROT_RIP:
		return "rip"
	case unix.RTPROT_EIGRP:
		return "cisco_eigrp"
	default:
		return ""
	}
}

func routeDestination(family uint8, dst []byte, dstLen int) (string, bool) {
	if dstLen == 0 && len(dst) == 0 {
		if family == unix.AF_INET6 {
			return "::/0", true
		}
		return "0.0.0.0/0", true
	}
	if len(dst) == 0 {
		return "", false
	}
	ip := net.IP(dst).String()
	if c, ok := entity.CanonicalIP(ip); ok {
		ip = c
	}
	return entity.CanonicalCIDR(ip, dstLen)
}

func skipRouteDestination(dst []byte) bool {
	if len(dst) == 0 {
		return false // default route
	}
	ip := net.IP(dst)
	return ip.IsLinkLocalUnicast() || ip.IsMulticast()
}

func parseRtnexthops(val []byte, family int) []rtNexthop {
	var out []rtNexthop
	for len(val) >= 8 {
		l := int(binary.LittleEndian.Uint16(val[0:2]))
		if l < 8 || l > len(val) {
			break
		}
		ifidx := int(binary.LittleEndian.Uint32(val[4:8]))
		var gateway string
		attrs := val[8:l]
		for len(attrs) >= 4 {
			al := int(binary.LittleEndian.Uint16(attrs[0:2]))
			atype := binary.LittleEndian.Uint16(attrs[2:4])
			if al < 4 || al > len(attrs) {
				break
			}
			if atype == unix.RTA_GATEWAY {
				if ip := net.IP(attrs[4:al]); ip != nil {
					if c, ok := entity.CanonicalIP(ip.String()); ok {
						gateway = c
					} else {
						gateway = ip.String()
					}
				}
			}
			attrs = attrs[(al+3)&^3:]
		}
		out = append(out, rtNexthop{Gateway: gateway, IfIndex: ifidx})
		val = val[(l+3)&^3:]
	}
	return out
}

func routeIface(ifName func(int) string, idx int) string {
	if ifName == nil || idx == 0 {
		return ""
	}
	return ifName(idx)
}
