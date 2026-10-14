package hostnet

import (
	"encoding/binary"
	"net"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	forwardRowSize  = 14 * 4
	offDest         = 0
	offMask         = 4
	offNextHop      = 12
	offIfIndex      = 16
	offType         = 20
	offProto        = 24
	offMetric1      = 36
	forwardHeadSize = 4

	forwardTypeOther    = 1
	forwardTypeInvalid  = 2
	forwardTypeDirect   = 3
	forwardTypeIndirect = 4

	routeIndirect = forwardTypeIndirect
)

func parseForwardTable(buf []byte, ifName func(int) string) []hostRoute {
	if len(buf) < forwardHeadSize {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[0:4]))
	var out []hostRoute
	seen := map[string]bool{}

	for i := 0; i < n; i++ {
		off := forwardHeadSize + i*forwardRowSize
		if off+forwardRowSize > len(buf) {
			break
		}
		row := buf[off : off+forwardRowSize]
		fwdType := binary.LittleEndian.Uint32(row[offType:])
		routeType, emit := windowsRouteType(fwdType)
		if !emit {
			continue
		}

		dst := net.IP(append([]byte(nil), row[offDest:offDest+4]...)).String()
		ones, bits := net.IPMask(append([]byte(nil), row[offMask:offMask+4]...)).Size()
		if bits == 0 {
			continue
		}
		cidr, ok := entity.CanonicalCIDR(dst, ones)
		if !ok {
			continue
		}

		gwIP := net.IP(append([]byte(nil), row[offNextHop:offNextHop+4]...))
		nextHop := ""
		switch routeType {
		case routeTypeLocal:
			nextHop = "0.0.0.0"
		case routeTypeRemote, routeTypeOther:
			// An unusable gateway is kept with an empty next hop: the
			// observation drops it and counts it as omitted.
			if !gwIP.IsUnspecified() {
				nextHop = gwIP.String()
			}
		}
		if c, ok := entity.CanonicalIP(nextHop); ok {
			nextHop = c
		}

		metric := binary.LittleEndian.Uint32(row[offMetric1:])
		ifIndex := binary.LittleEndian.Uint32(row[offIfIndex:])
		r := hostRoute{
			Destination: cidr,
			NextHop:     nextHop,
			Metric:      int64(metric),
			RouteType:   routeType,
		}
		if ifName != nil {
			r.Iface = ifName(int(ifIndex))
		}

		proto := binary.LittleEndian.Uint32(row[offProto:])
		code := int(proto)
		r.ProtocolCode = &code
		r.ProtocolName = windowsProtocolName(proto)

		key := cidr + " " + r.NextHop
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func windowsRouteType(t uint32) (string, bool) {
	switch t {
	case forwardTypeDirect:
		return routeTypeLocal, true
	case forwardTypeIndirect:
		return routeTypeRemote, true
	case forwardTypeOther:
		return routeTypeOther, true
	case forwardTypeInvalid:
		// Seen but not a route: passed on so the omitted counter counts it.
		return routeTypeInvalid, true
	default:
		return "", false
	}
}

func windowsProtocolName(code uint32) string {
	switch code {
	case 1:
		return "other"
	case 2:
		return "local"
	case 3:
		return "netmgmt"
	case 4:
		return "icmp"
	case 5:
		return "egp"
	case 6:
		return "ggp"
	case 7:
		return "hello"
	case 8:
		return "rip"
	case 9:
		return "isis"
	case 10:
		return "esis"
	case 11:
		return "cisco_igrp"
	case 12:
		return "bbn_spf_igp"
	case 13:
		return "ospf"
	case 14:
		return "bgp"
	case 15:
		return "idpr"
	case 16:
		return "cisco_eigrp"
	case 17:
		return "dvmrp"
	case 18:
		return "rpl"
	case 19:
		return "dhcp"
	case 20:
		return "ttdp"
	default:
		return ""
	}
}
