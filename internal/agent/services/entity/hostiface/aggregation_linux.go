//go:build linux

package hostiface

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// linuxLink is the subset of a RTM_NEWLINK message needed for aggregation.
type linuxLink struct {
	ifindex        int
	name           string
	master         int
	kind           string
	bondMode       uint8
	bondSlaveState uint8
	hasSlaveState  bool
	up             bool
	permMAC        []byte
}

func platformAggregation() (map[string]*aggregationInfo, error) {
	links, err := readNetlinkLinks()
	if err != nil {
		return nil, err
	}
	return aggregationByInterface(links), nil
}

func readNetlinkLinks() ([]linuxLink, error) {
	rib, err := syscall.NetlinkRIB(unix.RTM_GETLINK, unix.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("netlink RTM_GETLINK: %w", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(rib)
	if err != nil {
		return nil, fmt.Errorf("parse netlink messages: %w", err)
	}
	return parseLinkMessages(msgs), nil
}

func parseLinkMessages(msgs []syscall.NetlinkMessage) []linuxLink {
	var out []linuxLink
	for _, msg := range msgs {
		if msg.Header.Type != unix.RTM_NEWLINK {
			continue
		}
		data := msg.Data
		if len(data) < 16 {
			continue
		}
		link := linuxLink{
			ifindex: int(int32(binary.LittleEndian.Uint32(data[4:8]))),
			up:      binary.LittleEndian.Uint32(data[8:12])&unix.IFF_UP != 0,
		}
		attrs := data[16:]
		for len(attrs) >= 4 {
			l := int(binary.LittleEndian.Uint16(attrs[0:2]))
			typ := binary.LittleEndian.Uint16(attrs[2:4])
			if l < 4 || l > len(attrs) {
				break
			}
			val := attrs[4:l]
			switch typ {
			case unix.IFLA_IFNAME:
				link.name = strings.TrimRight(string(val), "\x00")
			case unix.IFLA_MASTER:
				if len(val) >= 4 {
					link.master = int(int32(binary.LittleEndian.Uint32(val[:4])))
				}
			case unix.IFLA_OPERSTATE:
				// Prefer the netlink oper state over the administrative IFF_UP
				// flag when the kernel supplies it. Unknown (0) and not-present
				// (1) defer to the flag-based value.
				if len(val) >= 1 && val[0] >= 2 {
					link.up = val[0] == 6 // IF_OPER_UP
				}
			case unix.IFLA_LINKINFO:
				parseLinkInfo(val, &link)
			case unix.IFLA_PERM_ADDRESS:
				link.permMAC = append([]byte(nil), val...)
			}
			attrs = attrs[(l+3)&^3:]
		}
		if link.name != "" {
			out = append(out, link)
		}
	}
	return out
}

func parseLinkInfo(val []byte, link *linuxLink) {
	for len(val) >= 4 {
		l := int(binary.LittleEndian.Uint16(val[0:2]))
		typ := binary.LittleEndian.Uint16(val[2:4])
		if l < 4 || l > len(val) {
			break
		}
		switch typ {
		case unix.IFLA_INFO_KIND:
			link.kind = strings.TrimRight(string(val[4:l]), "\x00")
		case unix.IFLA_INFO_DATA:
			if link.kind == "bond" {
				parseBondInfo(val[4:l], link)
			}
		case unix.IFLA_INFO_SLAVE_DATA:
			// Slave data can appear on a link without IFLA_INFO_KIND itself;
			// bond slave state is nested behind this attribute.
			parseBondSlaveData(val[4:l], link)
		}
		val = val[(l+3)&^3:]
	}
}

func parseBondInfo(val []byte, link *linuxLink) {
	for len(val) >= 4 {
		l := int(binary.LittleEndian.Uint16(val[0:2]))
		typ := binary.LittleEndian.Uint16(val[2:4])
		if l < 4 || l > len(val) {
			break
		}
		if typ == unix.IFLA_BOND_MODE && l >= 5 {
			link.bondMode = val[4]
		}
		val = val[(l+3)&^3:]
	}
}

func parseBondSlaveData(val []byte, link *linuxLink) {
	for len(val) >= 4 {
		l := int(binary.LittleEndian.Uint16(val[0:2]))
		typ := binary.LittleEndian.Uint16(val[2:4])
		if l < 4 || l > len(val) {
			break
		}
		if typ == unix.IFLA_BOND_SLAVE_STATE && l >= 5 {
			link.bondSlaveState = val[4]
			link.hasSlaveState = true
		}
		val = val[(l+3)&^3:]
	}
}

func aggregationByInterface(links []linuxLink) map[string]*aggregationInfo {
	byIndex := make(map[int]linuxLink, len(links))
	for _, l := range links {
		byIndex[l.ifindex] = l
	}

	out := make(map[string]*aggregationInfo)
	for _, l := range links {
		if l.name == "" {
			continue
		}
		if l.master != 0 {
			master, ok := byIndex[l.master]
			if !ok || (master.kind != "bond" && master.kind != "team") {
				continue
			}
			info := &aggregationInfo{MasterName: master.name}
			// A member's state exists only where the aggregate has one to
			// give: LACP and active-backup. A balance-* bond or a team has
			// no backup notion, so the state stays absent; the aggregate's
			// mode then says every member carries traffic.
			if master.kind == "bond" && (master.bondMode == 4 || master.bondMode == 1) && l.hasSlaveState {
				switch l.bondSlaveState {
				case 0:
					info.State = "active"
				case 1:
					info.State = "standby"
				}
			}
			out[l.name] = info
			continue
		}
		if l.kind == "bond" || l.kind == "team" {
			info := &aggregationInfo{}
			if l.kind == "bond" {
				switch l.bondMode {
				case 4:
					info.Mode = "lacp"
				case 1:
					info.Mode = "active-backup"
				default:
					info.Mode = "static"
				}
			} else {
				info.Mode = "static"
			}
			out[l.name] = info
		}
	}
	return out
}
