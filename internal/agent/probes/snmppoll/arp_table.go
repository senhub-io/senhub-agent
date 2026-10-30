package snmppoll

import (
	"fmt"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/snmpcore"
)

const (
	ipNetToPhysicalEntry = "1.3.6.1.2.1.4.35.1"
	colIPNetPhysAddress  = "4"
	colIPNetType         = "6"
	colIPNetState        = "7"

	ipNetToMediaEntry     = "1.3.6.1.2.1.4.22.1"
	colIPNetMediaPhysAddr = "2"
	colIPNetMediaType     = "4"
)

const (
	arpTypeInvalid = 2
	arpTypeDynamic = 3
	arpTypeStatic  = 4
	arpTypeLocal   = 5
)

type arpEntry struct {
	IfIndex int
	IP      string
	MAC     string
	Static  bool
}

type arpBuild struct {
	ifIndex int
	ip      string
	mac     string
	typ     int
}

func collectARP(client snmpClient) ([]arpEntry, error) {
	primary, pErr := client.WalkRaw(ipNetToPhysicalEntry)
	legacy, lErr := client.WalkRaw(ipNetToMediaEntry)
	rows := parseARPTables(primary, legacy)
	if pErr != nil && lErr != nil {
		return nil, fmt.Errorf("ARP walks: primary %w; legacy %w", pErr, lErr)
	}
	if pErr != nil && len(rows) == 0 {
		return nil, fmt.Errorf("ARP walks: primary %w; legacy table empty", pErr)
	}
	return rows, nil
}

func parseARPTables(primary, legacy []snmpRawBind) []arpEntry {
	if rows := parseIPNetToPhysicalTable(primary); len(rows) > 0 {
		return rows
	}
	return parseIPNetToMediaTable(legacy)
}

func parseIPNetToPhysicalTable(binds []snmpRawBind) []arpEntry {
	rows := map[string]*arpBuild{}
	var order []string
	prefix := ipNetToPhysicalEntry + "."

	for _, b := range binds {
		rest, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		col, rowKey, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		r := rows[rowKey]
		if r == nil {
			ifIndex, ip, ok := parseIPNetToPhysicalIndex(rowKey)
			if !ok {
				continue
			}
			r = &arpBuild{ifIndex: ifIndex, ip: ip, typ: -1}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colIPNetPhysAddress:
			r.mac = validUnicastMAC(snmpcore.AsBytes(b.Value))
		case colIPNetType:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.typ = v
			}
		}
	}

	out := make([]arpEntry, 0, len(order))
	for _, k := range order {
		r := rows[k]
		if r.typ == arpTypeInvalid || r.typ < 0 || r.mac == "" {
			continue
		}
		if r.typ == arpTypeLocal {
			continue
		}
		out = append(out, arpEntry{
			IfIndex: r.ifIndex,
			IP:      r.ip,
			MAC:     r.mac,
			Static:  r.typ == arpTypeStatic,
		})
	}
	return out
}

func parseIPNetToMediaTable(binds []snmpRawBind) []arpEntry {
	rows := map[string]*arpBuild{}
	var order []string
	prefix := ipNetToMediaEntry + "."

	for _, b := range binds {
		rest, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		col, rowKey, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		r := rows[rowKey]
		if r == nil {
			p := strings.Split(rowKey, ".")
			if len(p) != 5 {
				continue
			}
			ifIndex, err := strconv.Atoi(p[0])
			if err != nil {
				continue
			}
			ip := inetAddrFromIndex(inetAddrTypeIPv4, p[1:5])
			if ip == "" {
				continue
			}
			r = &arpBuild{ifIndex: ifIndex, ip: ip, typ: -1}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colIPNetMediaPhysAddr:
			r.mac = validUnicastMAC(snmpcore.AsBytes(b.Value))
		case colIPNetMediaType:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.typ = v
			}
		}
	}

	out := make([]arpEntry, 0, len(order))
	for _, k := range order {
		r := rows[k]
		if r.typ == arpTypeInvalid || r.typ < 0 || r.mac == "" {
			continue
		}
		out = append(out, arpEntry{
			IfIndex: r.ifIndex,
			IP:      r.ip,
			MAC:     r.mac,
			Static:  r.typ == arpTypeStatic,
		})
	}
	return out
}

func parseIPNetToPhysicalIndex(rowKey string) (int, string, bool) {
	p := strings.Split(rowKey, ".")
	if len(p) < 3 {
		return 0, "", false
	}
	ifIndex, err := strconv.Atoi(p[0])
	if err != nil {
		return 0, "", false
	}
	addrType, err := strconv.Atoi(p[1])
	if err != nil {
		return 0, "", false
	}
	addrLen, err := strconv.Atoi(p[2])
	if err != nil || addrLen <= 0 {
		return 0, "", false
	}
	if len(p) < 3+addrLen {
		return 0, "", false
	}
	ip := inetAddrFromIndex(addrType, p[3:3+addrLen])
	if ip == "" {
		return 0, "", false
	}
	return ifIndex, ip, true
}

func validUnicastMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ""
	}
	// I/G bit set covers broadcast and all multicast Ethernet forms.
	if b[0]&1 == 1 {
		return ""
	}
	return macHex(b)
}
