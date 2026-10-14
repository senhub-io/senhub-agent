//go:build linux

package hostnet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformNeighbors() ([]hostNeighbor, error) {
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return nil, fmt.Errorf("read /proc/net/arp: %w", err)
	}
	neighbors := parseProcARP(bytes.NewReader(b))

	rib, err := syscall.NetlinkRIB(unix.RTM_GETNEIGH, unix.AF_INET6)
	if err == nil {
		msgs, err := syscall.ParseNetlinkMessage(rib)
		if err == nil {
			neighbors = append(neighbors, parseNeighMessages(msgs, func(idx int) string {
				if ifc, err := net.InterfaceByIndex(idx); err == nil {
					return ifc.Name
				}
				return ""
			})...)
		}
	}
	return dedupeAndSortNeighbors(neighbors), nil
}

func parseNeighMessages(msgs []syscall.NetlinkMessage, ifName func(int) string) []hostNeighbor {
	var out []hostNeighbor
	for _, msg := range msgs {
		if msg.Header.Type != unix.RTM_NEWNEIGH {
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
		ifindex := int(int32(binary.LittleEndian.Uint32(data[4:8])))
		state := binary.LittleEndian.Uint16(data[8:10])

		var ip, mac string
		attrs := data[12:]
		for len(attrs) >= 4 {
			l := int(binary.LittleEndian.Uint16(attrs[0:2]))
			typ := binary.LittleEndian.Uint16(attrs[2:4])
			if l < 4 || l > len(attrs) {
				break
			}
			val := attrs[4:l]
			switch typ {
			case unix.NDA_DST:
				if (family == unix.AF_INET && len(val) == 4) || (family == unix.AF_INET6 && len(val) == 16) {
					ip = net.IP(val).String()
				}
			case unix.NDA_LLADDR:
				if len(val) > 0 {
					mac = net.HardwareAddr(val).String()
				}
			}
			attrs = attrs[(l+3)&^3:]
		}
		if ip == "" || mac == "" {
			continue
		}
		var iface string
		if ifName != nil {
			iface = ifName(ifindex)
		}
		n := hostNeighbor{IP: ip, MAC: mac, Iface: iface, State: neighState(state)}
		if keepNeighbor(n) {
			out = append(out, n)
		}
	}
	return out
}

func neighState(state uint16) string {
	switch state {
	case unix.NUD_INCOMPLETE:
		return "incomplete"
	case unix.NUD_REACHABLE:
		return "reachable"
	case unix.NUD_STALE:
		return "stale"
	case unix.NUD_DELAY:
		return "delay"
	case unix.NUD_PROBE:
		return "probe"
	case unix.NUD_FAILED:
		return "failed"
	case unix.NUD_PERMANENT:
		return "permanent"
	default:
		return ""
	}
}
