package snmppoll

import (
	"encoding/hex"
	"fmt"
	"net"
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
	// Local system group (scalars at .0) + local port table (lldpLocPortTable
	// at .7, indexed by lldpLocPortNum — the same numbering as a neighbour's
	// lldpRemLocalPortNum, so it names the local end of each link).
	lldpLocBase             = "1.0.8802.1.1.2.1.3"
	lldpLocChassisIdSubtype = "1.0.8802.1.1.2.1.3.1.0"
	lldpLocChassisId        = "1.0.8802.1.1.2.1.3.2.0"
	lldpLocSysName          = "1.0.8802.1.1.2.1.3.3.0"
	lldpLocPortIdSubtype    = "1.0.8802.1.1.2.1.3.7.1.2"
	lldpLocPortId           = "1.0.8802.1.1.2.1.3.7.1.3"

	// Remote systems table lldpRemTable / lldpRemEntry.
	// Row index = lldpRemTimeMark . lldpRemLocalPortNum . lldpRemIndex.
	lldpRemEntry           = "1.0.8802.1.1.2.1.4.1.1"
	colRemChassisIdSubtype = "4"
	colRemChassisId        = "5"
	colRemPortIdSubtype    = "6"
	colRemPortId           = "7"
	colRemSysName          = "9"

	// Remote management address table. The neighbour's management IP is encoded
	// in the ROW INDEX (not a column value):
	//   timeMark . localPort . remIndex . addrSubtype . addrLen . <addr bytes>
	// The first three sub-ids match the lldpRemTable row key, so an entry maps
	// a neighbour to its management address — what the crawl needs to poll it.
	// Walking any column of the table exposes the index; we walk ifId (col 4).
	lldpRemManAddrIfId = "1.0.8802.1.1.2.1.4.2.1.4"
	manAddrSubtypeIPv4 = "1"
	manAddrLenIPv4     = 4
)

// IdSubtype enumerations from the LLDP-MIB (LldpChassisIdSubtype /
// LldpPortIdSubtype). Only the values we render specially are named.
const (
	subtypeMacAddress = 4 // chassis & port: MAC address
	portSubtypeIfName = 5 // port: interfaceName
	portSubtypeLocal  = 7 // port: locally assigned
)

// lldpLocal is the polled device's own LLDP identity, plus the local port
// table (lldpLocPortNum → port name) used to name the local end of a link.
type lldpLocal struct {
	ChassisIdSubtype int
	ChassisId        []byte
	SysName          string
	Ports            map[string]string // lldpLocPortNum → port name (ifName/local subtype)
}

// lldpNeighbor is one decoded remote-system table row.
type lldpNeighbor struct {
	LocalPortNum     string
	ChassisIdSubtype int
	ChassisId        []byte
	PortIdSubtype    int
	PortId           []byte
	SysName          string
	MgmtIP           string // lldpRemManAddr (neighbour management IP) — crawl seed
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
	// Neighbour management addresses are best-effort: a device that omits the
	// table still yields neighbours (without a crawl seed IP).
	manBinds, _ := client.WalkRaw(lldpRemManAddrIfId)
	topo.Neighbors = parseLLDPNeighbors(remBinds, parseLLDPManAddrs(manBinds))

	return topo, nil
}

// parseLLDPManAddrs maps a neighbour row key (timeMark.localPort.remIndex) to
// its management IPv4 address, decoded from the lldpRemManAddrTable row index.
// First usable IPv4 per neighbour wins.
func parseLLDPManAddrs(binds []snmpRawBind) map[string]string {
	out := map[string]string{}
	prefix := lldpRemManAddrIfId + "."
	for _, b := range binds {
		idx, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		p := strings.Split(idx, ".")
		// timeMark.localPort.remIndex.subtype.len.<4 addr octets>
		if len(p) < 5+manAddrLenIPv4 || p[3] != manAddrSubtypeIPv4 {
			continue
		}
		key := p[0] + "." + p[1] + "." + p[2]
		if _, exists := out[key]; exists {
			continue
		}
		ip := strings.Join(p[5:5+manAddrLenIPv4], ".")
		if net.ParseIP(ip) != nil {
			out[key] = ip
		}
	}
	return out
}

func parseLLDPLocal(binds []snmpRawBind) lldpLocal {
	var loc lldpLocal
	portSubtype := map[string]int{}  // lldpLocPortNum → subtype
	portRawID := map[string][]byte{} // lldpLocPortNum → raw lldpLocPortId
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
		default:
			if num, ok := strings.CutPrefix(b.OID, lldpLocPortIdSubtype+"."); ok {
				if v, ok := asIntVal(b.Value); ok {
					portSubtype[num] = v
				}
			} else if num, ok := strings.CutPrefix(b.OID, lldpLocPortId+"."); ok {
				portRawID[num] = asBytes(b.Value)
			}
		}
	}
	// Keep only ports whose id is a usable name (interfaceName / local); a
	// MAC-only local port has no name to anchor connected_to.
	for num, raw := range portRawID {
		switch portSubtype[num] {
		case portSubtypeIfName, portSubtypeLocal:
			if name := octetText(raw); name != "" {
				if loc.Ports == nil {
					loc.Ports = map[string]string{}
				}
				loc.Ports[num] = name
			}
		}
	}
	return loc
}

func parseLLDPNeighbors(binds []snmpRawBind, manAddrs map[string]string) []lldpNeighbor {
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
		rows[k].MgmtIP = manAddrs[k]
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

// network.device.id — THE contract, frozen with the Toise team (ADR 0018:
// identity is exact, byte-by-byte, observer-independent, no fuzzy merge —
// see docs/audit/LOT5-TOISE-DISCUSSION.md). ALL id-format decisions live in
// resolveDeviceID and the canon helpers — the only contract-bound code on the
// entity rail besides relation endpoints/attributes.

// deviceIdentity carries the raw identifiers read for a device. Empty fields
// are skipped; resolveDeviceID applies the frozen precedence. Identity does
// NOT anchor on LLDP: the polled device's id comes from its serial / engine
// id read over SNMP, so it is stable even when LLDP is disabled.
type deviceIdentity struct {
	Serial     string // entPhysicalSerialNum of the *single* chassis (empty for a stack)
	VendorPEN  string // IANA enterprise number from sysObjectID — namespaces Serial
	EngineID   []byte // snmpEngineID — globally unique (RFC 3411) + stack-scoped fallback
	ChassisMAC []byte // LLDP chassis-id, only when its subtype is MAC
	SysName    string // sysName / lldpRemSysName
	MgmtIP     string // the polled target address (mutable — last resort)
	Services   int    // sysServices bitmask — descriptive device.role (router/switch)
}

// resolveDeviceID produces the single network.device.id with the Toise-frozen
// precedence serial > engine > mac > name > mgmt. The chosen value is
// canonicalized so two observers of the same device derive byte-identical ids.
// Everything not chosen (raw chassis-id, sysName, mgmt IP, serial, vendor)
// belongs in descriptive attributes, never as a second identity key.
func resolveDeviceID(id deviceIdentity) string {
	switch {
	// Serial is vendor-scoped (unique per vendor, not globally), so it is a
	// usable identity ONLY when namespaced by the vendor's IANA enterprise
	// number (from sysObjectID). Without a PEN — or for a stack, where the
	// reader leaves Serial empty — fall through to engine, which RFC 3411
	// makes globally unique and stack-scoped on its own.
	case strings.TrimSpace(id.Serial) != "" && strings.TrimSpace(id.VendorPEN) != "":
		return "serial:" + strings.TrimSpace(id.VendorPEN) + ":" + strings.TrimSpace(id.Serial)
	case len(id.EngineID) > 0:
		return "engine:" + hex.EncodeToString(id.EngineID)
	case len(id.ChassisMAC) > 0:
		return "mac:" + macHex(id.ChassisMAC)
	case strings.TrimSpace(id.SysName) != "":
		return "name:" + strings.TrimSpace(id.SysName)
	case strings.TrimSpace(id.MgmtIP) != "":
		return "mgmt:" + canonIP(id.MgmtIP)
	default:
		return ""
	}
}

// neighborIdentity maps an LLDP-discovered neighbour (LLDP data only — the
// neighbour itself is not polled) to its usable identifiers: its chassis-id
// is a usable id only when it is a MAC; otherwise we fall back to its
// advertised sysName.
func neighborIdentity(n lldpNeighbor) deviceIdentity {
	di := deviceIdentity{SysName: n.SysName}
	if n.ChassisIdSubtype == subtypeMacAddress {
		di.ChassisMAC = n.ChassisId
	}
	return di
}

// canonIP renders an IP in one canonical form (IPv4 dotted no-leading-zeros,
// IPv6 RFC 5952 lowercase compressed). A non-IP literal (hostname) is
// lowercased/trimmed as a last resort.
func canonIP(s string) string {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return strings.ToLower(s)
}

// vendorPEN extracts the IANA Private Enterprise Number from a sysObjectID
// value (an OID under 1.3.6.1.4.1.<PEN>...). Returns "" when the OID is not
// under the enterprise arc.
func vendorPEN(sysObjectID string) string {
	const arc = "1.3.6.1.4.1."
	s := strings.TrimPrefix(strings.TrimSpace(sysObjectID), ".")
	if !strings.HasPrefix(s, arc) {
		return ""
	}
	pen, _, _ := strings.Cut(s[len(arc):], ".")
	return pen
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

// namedPortID renders an LLDP port-id as a network.interface name, but ONLY
// when the subtype is a usable name (interfaceName / locally-assigned). A
// MAC-only or address-only remote port has no name to serve as exact identity,
// so it returns "" and the caller skips the link rather than fabricate a
// phantom port (frozen contract, point 7).
func namedPortID(subtype int, portId []byte) string {
	switch subtype {
	case portSubtypeIfName, portSubtypeLocal:
		return octetText(portId)
	default:
		return ""
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
