package snmppoll

import (
	"fmt"
	"senhub-agent.go/internal/agent/services/snmpcore"
	"strings"
)

// IF-MIB interface inventory (entity rail): the device's ports as
// network.interface entities. ifName (ifXTable) is the port identity component;
// ifOperStatus (ifTable) and ifHighSpeed (ifXTable) are descriptive.
const (
	ifName       = "1.3.6.1.2.1.31.1.1.1.1"  // ifXTable: textual port name (Gi0/1)
	ifHighSpeed  = "1.3.6.1.2.1.31.1.1.1.15" // ifXTable: speed in Mbit/s
	ifOperStatus = "1.3.6.1.2.1.2.2.1.8"     // ifTable: IF-MIB OperStatus enum
	ifType       = "1.3.6.1.2.1.2.2.1.3"     // ifTable: IANAifType
	ifMtu        = "1.3.6.1.2.1.2.2.1.4"     // ifTable: ifMtu (octets)
	ifPhysAddr   = "1.3.6.1.2.1.2.2.1.6"     // ifTable: ifPhysAddress (MAC)
	dot3Duplex   = "1.3.6.1.2.1.10.7.2.1.19" // EtherLike-MIB: dot3StatsDuplexStatus

	ifOperUp             = 1
	ifOperDown           = 2
	ifOperTesting        = 3
	ifOperDormant        = 5
	ifOperNotPresent     = 6
	ifOperLowerLayerDown = 7
)

// ifaceRow is one decoded interface (only the fields the entity rail uses).
type ifaceRow struct {
	Index      string
	Name       string
	OperStatus int    // raw IF-MIB enum (0 when unread)
	SpeedMbps  int64  // ifHighSpeed (Mbit/s); 0 when unread
	Mac        string // ifPhysAddress, formatted aa:bb:cc:dd:ee:ff; "" when unread
	Mtu        int64  // ifMtu (octets); 0 when unread
	IfType     int    // IANAifType; 0 when unread
	Duplex     int    // dot3StatsDuplexStatus (1=unknown,2=half,3=full); 0 when unread
}

// collectInterfaces walks the interface name/status/speed columns. ifName is
// the only required walk (it carries the identity); status and speed are
// best-effort so a device that omits ifXTable speed still yields named ports.
func collectInterfaces(client snmpClient) ([]ifaceRow, error) {
	nameBinds, err := client.WalkRaw(ifName)
	if err != nil {
		return nil, fmt.Errorf("ifName walk: %w", err)
	}
	speedBinds, _ := client.WalkRaw(ifHighSpeed)
	operBinds, _ := client.WalkRaw(ifOperStatus)
	typeBinds, _ := client.WalkRaw(ifType)
	mtuBinds, _ := client.WalkRaw(ifMtu)
	macBinds, _ := client.WalkRaw(ifPhysAddr)
	duplexBinds, _ := client.WalkRaw(dot3Duplex)
	return parseInterfaces(nameBinds, speedBinds, operBinds, typeBinds, mtuBinds, macBinds, duplexBinds), nil
}

func parseInterfaces(nameBinds, speedBinds, operBinds, typeBinds, mtuBinds, macBinds, duplexBinds []snmpRawBind) []ifaceRow {
	rows := map[string]*ifaceRow{}
	var order []string

	for _, b := range nameBinds {
		idx, ok := strings.CutPrefix(b.OID, ifName+".")
		if !ok {
			continue
		}
		name := snmpcore.OctetText(snmpcore.AsBytes(b.Value))
		if name == "" {
			continue
		}
		r := rows[idx]
		if r == nil {
			r = &ifaceRow{Index: idx}
			rows[idx] = r
			order = append(order, idx)
		}
		r.Name = name
	}
	for _, b := range speedBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifHighSpeed+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					r.SpeedMbps = int64(v)
				}
			}
		}
	}
	for _, b := range operBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifOperStatus+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					r.OperStatus = v
				}
			}
		}
	}
	for _, b := range typeBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifType+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					r.IfType = v
				}
			}
		}
	}
	for _, b := range mtuBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifMtu+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					r.Mtu = int64(v)
				}
			}
		}
	}
	for _, b := range macBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifPhysAddr+"."); ok {
			if r := rows[idx]; r != nil {
				r.Mac = macString(snmpcore.AsBytes(b.Value))
			}
		}
	}
	for _, b := range duplexBinds {
		if idx, ok := strings.CutPrefix(b.OID, dot3Duplex+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					r.Duplex = v
				}
			}
		}
	}

	out := make([]ifaceRow, 0, len(order))
	for _, idx := range order {
		out = append(out, *rows[idx])
	}
	return out
}

// operStateName maps the IF-MIB ifOperStatus enum to the oper_state state-key
// value (frozen casing, Toise #87). notPresent is filtered upstream (the
// interface is not emitted), so it has no name here.
func operStateName(v int) string {
	switch v {
	case ifOperUp:
		return "up"
	case ifOperDown:
		return "down"
	case ifOperTesting:
		return "testing"
	case ifOperDormant:
		return "dormant"
	case ifOperLowerLayerDown:
		return "lowerLayerDown"
	default:
		return "unknown"
	}
}

// macString formats ifPhysAddress octets as lowercase colon-separated hex
// (aa:bb:cc:dd:ee:ff). An empty or all-zero address yields "" — many virtual
// and loopback ports report 6 zero bytes, which is not a usable identity.
func macString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	allZero := true
	for _, c := range b {
		if c != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ""
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}

// ifTypeName maps the IANAifType enum to the frozen interface.type vocabulary
// (physical/virtual/wireless/loopback), matching the host interface path
// (hostiface). Unmapped types yield "" so the attribute is omitted rather than
// guessed.
func ifTypeName(v int) string {
	switch v {
	case 6, 7, 62, 69, 117: // ethernetCsmacd, iso88023, fastEther(FX), gigabitEthernet
		return "physical"
	case 24: // softwareLoopback
		return "loopback"
	case 71: // ieee80211
		return "wireless"
	case 53, 131, 135, 136, 150, 161: // propVirtual, tunnel, l2/l3 vlan, mplsTunnel, ieee8023adLag
		return "virtual"
	default:
		return ""
	}
}

// duplexName maps dot3StatsDuplexStatus (EtherLike-MIB) to the frozen duplex
// vocabulary (full/half/unknown), matching the host interface path.
func duplexName(v int) string {
	switch v {
	case 2:
		return "half"
	case 3:
		return "full"
	case 1:
		return "unknown"
	default:
		return ""
	}
}
