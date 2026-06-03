package snmppoll

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// LLDP topology parsing (Lot 5a prep — entity rail).
//
// This file decodes the LLDP-MIB local-system scalars and the remote-neighbour
// table into neutral Go structs. It deliberately stops BEFORE building
// entity.Entity / entity.Relation values: the mapping from a parsed neighbour
// to the wire shape (especially the network.device.id format) is the contract
// frozen with the Toise team and is NOT decided yet — see
// docs/audit/LOT5-TOISE-DISCUSSION.md. The single contract-bound piece,
// renderDeviceID, is isolated here so the freeze touches one function.

// LLDP-MIB OIDs (dotted, no leading dot). Base: 1.0.8802.1.1.2.1.
const (
	// Local system group (scalars at .0).
	lldpLocBase             = "1.0.8802.1.1.2.1.3"
	lldpLocChassisIdSubtype = "1.0.8802.1.1.2.1.3.1.0"
	lldpLocChassisId        = "1.0.8802.1.1.2.1.3.2.0"
	lldpLocSysName          = "1.0.8802.1.1.2.1.3.3.0"

	// Remote systems table lldpRemTable / lldpRemEntry.
	// Row index = lldpRemTimeMark . lldpRemLocalPortNum . lldpRemIndex.
	lldpRemEntry           = "1.0.8802.1.1.2.1.4.1.1"
	colRemChassisIdSubtype = "4"
	colRemChassisId        = "5"
	colRemPortIdSubtype    = "6"
	colRemPortId           = "7"
	colRemSysName          = "9"
)

// IdSubtype enumerations from the LLDP-MIB (LldpChassisIdSubtype /
// LldpPortIdSubtype). Only the values we render specially are named.
const (
	subtypeMacAddress  = 4 // chassis & port: MAC
	subtypeNetworkAddr = 5 // chassis: networkAddress / port: not used
	subtypeIfName      = 7 // chassis: local / port: differs — see render funcs
	portSubtypeIfName  = 5 // port: interfaceName
	portSubtypeLocal   = 7 // port: locally assigned
)

// lldpLocal is the polled device's own LLDP identity.
type lldpLocal struct {
	ChassisIdSubtype int
	ChassisId        []byte
	SysName          string
}

// lldpNeighbor is one decoded remote-system table row.
type lldpNeighbor struct {
	LocalPortNum     string
	ChassisIdSubtype int
	ChassisId        []byte
	PortIdSubtype    int
	PortId           []byte
	SysName          string
}

// lldpTopology is the parsed snapshot: the local device + its neighbours.
type lldpTopology struct {
	Local     lldpLocal
	Neighbors []lldpNeighbor
}

// collectLLDP walks the LLDP local scalars and the remote-neighbour table and
// returns the parsed topology. It does not emit entities — that is Lot 5a
// proper, post-freeze.
func collectLLDP(client snmpClient) (lldpTopology, error) {
	var topo lldpTopology

	locBinds, err := client.WalkRaw(lldpLocBase)
	if err != nil {
		return topo, fmt.Errorf("lldp local walk: %w", err)
	}
	topo.Local = parseLLDPLocal(locBinds)

	remBinds, err := client.WalkRaw(lldpRemEntry)
	if err != nil {
		return topo, fmt.Errorf("lldp remote walk: %w", err)
	}
	topo.Neighbors = parseLLDPNeighbors(remBinds)

	return topo, nil
}

func parseLLDPLocal(binds []snmpRawBind) lldpLocal {
	var loc lldpLocal
	for _, b := range binds {
		switch b.OID {
		case lldpLocChassisIdSubtype:
			if v, ok := asIntVal(b.Value); ok {
				loc.ChassisIdSubtype = v
			}
		case lldpLocChassisId:
			loc.ChassisId = asBytes(b.Value)
		case lldpLocSysName:
			loc.SysName = octetText(asBytes(b.Value))
		}
	}
	return loc
}

func parseLLDPNeighbors(binds []snmpRawBind) []lldpNeighbor {
	// Group cells by row key (timeMark.localPort.remIndex), preserving first-seen
	// order so the output is deterministic.
	rows := map[string]*lldpNeighbor{}
	var order []string
	prefix := lldpRemEntry + "."

	for _, b := range binds {
		rest, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		col, rowKey, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		n := rows[rowKey]
		if n == nil {
			n = &lldpNeighbor{LocalPortNum: localPortOf(rowKey)}
			rows[rowKey] = n
			order = append(order, rowKey)
		}
		switch col {
		case colRemChassisIdSubtype:
			if v, ok := asIntVal(b.Value); ok {
				n.ChassisIdSubtype = v
			}
		case colRemChassisId:
			n.ChassisId = asBytes(b.Value)
		case colRemPortIdSubtype:
			if v, ok := asIntVal(b.Value); ok {
				n.PortIdSubtype = v
			}
		case colRemPortId:
			n.PortId = asBytes(b.Value)
		case colRemSysName:
			n.SysName = octetText(asBytes(b.Value))
		}
	}

	out := make([]lldpNeighbor, 0, len(order))
	for _, k := range order {
		out = append(out, *rows[k])
	}
	return out
}

// localPortOf extracts lldpRemLocalPortNum (2nd sub-id) from a row key
// timeMark.localPort.remIndex.
func localPortOf(rowKey string) string {
	parts := strings.Split(rowKey, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// renderDeviceID is THE contract-bound function (frozen with Toise, Lot 5 —
// see docs/audit/LOT5-TOISE-DISCUSSION.md). It maps an LLDP chassis-id +
// subtype to the network.device.id string. The current scheme is the
// recommended subtype-prefixed form; it is provisional until Toise confirms.
// Keep all id-format decisions in this one function.
func renderDeviceID(subtype int, chassisId []byte) string {
	switch subtype {
	case subtypeMacAddress:
		return "mac:" + macHex(chassisId)
	case subtypeNetworkAddr:
		return "addr:" + hex.EncodeToString(chassisId)
	case subtypeIfName: // chassis subtype 7 = locally assigned
		return "local:" + octetText(chassisId)
	default:
		return fmt.Sprintf("chassis%d:%s", subtype, octetText(chassisId))
	}
}

// renderPortID renders an LLDP port-id for relation attributes. Not a frozen
// identity (ports are edge attributes, not node ids), but kept beside
// renderDeviceID for consistency.
func renderPortID(subtype int, portId []byte) string {
	switch subtype {
	case subtypeMacAddress:
		return macHex(portId)
	case portSubtypeIfName, portSubtypeLocal:
		return octetText(portId)
	default:
		return octetText(portId)
	}
}

// --- decode helpers (no contract impact) ---

func asBytes(v any) []byte {
	switch b := v.(type) {
	case []byte:
		return b
	case string:
		return []byte(b)
	default:
		return nil
	}
}

func asIntVal(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}

// octetText renders an OCTET STRING as text when it is printable, else as hex.
func octetText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if isPrintable(b) {
		return strings.TrimRight(string(b), "\x00")
	}
	return hex.EncodeToString(b)
}

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			continue // trailing NULs are common and tolerated
		}
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// macHex renders bytes as lowercase colon-separated hex (00:11:22:...).
func macHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}
