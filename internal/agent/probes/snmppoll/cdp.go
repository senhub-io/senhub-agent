package snmppoll

import (
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/snmpcore"
)

const (
	cdpCacheEntry      = "1.3.6.1.4.1.9.9.23.1.2.1.1"
	colCDPAddressType  = "3"
	colCDPAddress      = "4"
	colCDPDeviceId     = "6"
	colCDPDevicePort   = "7"
	colCDPPlatform     = "8"
	colCDPCapabilities = "9"

	cdpAddressTypeIP = 1
)

type cdpNeighbor struct {
	LocalIfIndex int
	DeviceID     string
	Port         string
	Platform     string
	MgmtIP       string
	Capabilities []string
}

type cdpBuild struct {
	cdpNeighbor
	addrType int
	addr     []byte
	caps     int
}

func parseCDPNeighbors(binds []snmpRawBind) []cdpNeighbor {
	rows := map[string]*cdpBuild{}
	var order []string
	prefix := cdpCacheEntry + "."

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
			if len(p) < 2 {
				continue
			}
			localIfIndex, err := strconv.Atoi(p[0])
			if err != nil {
				continue
			}
			r = &cdpBuild{cdpNeighbor: cdpNeighbor{LocalIfIndex: localIfIndex}}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colCDPAddressType:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.addrType = v
			}
		case colCDPAddress:
			r.addr = snmpcore.AsBytes(b.Value)
		case colCDPDeviceId:
			r.DeviceID = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
		case colCDPDevicePort:
			r.Port = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
		case colCDPPlatform:
			r.Platform = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
		case colCDPCapabilities:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.caps = v
			}
		}
	}

	out := make([]cdpNeighbor, 0, len(order))
	for _, k := range order {
		r := rows[k]
		n := r.cdpNeighbor
		if r.addrType == cdpAddressTypeIP && len(r.addr) == 4 {
			n.MgmtIP = asIPString(r.addr)
		}
		n.Capabilities = cdpCapabilityNames(r.caps)
		out = append(out, n)
	}
	return out
}

func cdpCapabilityNames(caps int) []string {
	out := make([]string, 0, 5)
	if caps&0x01 != 0 {
		out = append(out, "router")
	}
	if caps&0x02 != 0 || caps&0x04 != 0 {
		out = append(out, "bridge")
	}
	if caps&0x08 != 0 {
		out = append(out, "switch")
	}
	if caps&0x10 != 0 {
		out = append(out, "host")
	}
	if caps&0x40 != 0 {
		out = append(out, "repeater")
	}
	return out
}
