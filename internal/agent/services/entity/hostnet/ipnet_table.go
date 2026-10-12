package hostnet

import (
	"encoding/binary"
	"net"
)

// Layout of MIB_IPNET_TABLE2 / MIB_IPNET_ROW2 as returned by GetIpNetTable2.
// x64 offsets from the Windows SDK; row size is 88 bytes.
const (
	ipnetTableHeaderSize          = 8
	ipnetRowSize                  = 88
	ipnetAddrFamilyOff            = 0
	ipnetV4AddrOff                = 4
	ipnetV6AddrOff                = 8
	ipnetInterfaceIndexOff        = 28
	ipnetInterfaceLuidOff         = 32
	ipnetPhysicalAddressOff       = 40
	ipnetPhysicalAddressLengthOff = 72
	ipnetStateOff                 = 76
	ipnetFlagsOff                 = 80
	ipnetReachabilityTimeOff      = 84

	afINET  = 2
	afINET6 = 23
)

func parseIpNetTable2(buf []byte, ifName func(int) string) []hostNeighbor {
	if len(buf) < ipnetTableHeaderSize {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	var out []hostNeighbor
	for i := 0; i < n; i++ {
		off := ipnetTableHeaderSize + i*ipnetRowSize
		if off+ipnetRowSize > len(buf) {
			break
		}
		row := buf[off : off+ipnetRowSize]

		family := binary.LittleEndian.Uint16(row[ipnetAddrFamilyOff:])
		var ip string
		switch family {
		case afINET:
			ip = net.IP(append([]byte(nil), row[ipnetV4AddrOff:ipnetV4AddrOff+4]...)).String()
		case afINET6:
			ip = net.IP(append([]byte(nil), row[ipnetV6AddrOff:ipnetV6AddrOff+16]...)).String()
		default:
			continue
		}

		macLen := int(binary.LittleEndian.Uint32(row[ipnetPhysicalAddressLengthOff:]))
		if macLen > 32 {
			macLen = 32
		}
		var mac string
		if macLen > 0 {
			mac = net.HardwareAddr(append([]byte(nil), row[ipnetPhysicalAddressOff:ipnetPhysicalAddressOff+macLen]...)).String()
		}

		ifIndex := int(binary.LittleEndian.Uint32(row[ipnetInterfaceIndexOff:]))
		var iface string
		if ifName != nil {
			iface = ifName(ifIndex)
		}
		n := hostNeighbor{
			IP:    ip,
			MAC:   mac,
			Iface: iface,
			State: ipnetRowState(binary.LittleEndian.Uint32(row[ipnetStateOff:])),
		}
		if keepNeighbor(n) {
			out = append(out, n)
		}
	}
	return out
}

func ipnetRowState(state uint32) string {
	switch state {
	case 0: // NL_NEIGHBOR_STATE_UNREACHABLE
		return "failed"
	case 1:
		return "incomplete"
	case 2:
		return "probe"
	case 3:
		return "delay"
	case 4:
		return "stale"
	case 5:
		return "reachable"
	case 6:
		return "permanent"
	default:
		return ""
	}
}
