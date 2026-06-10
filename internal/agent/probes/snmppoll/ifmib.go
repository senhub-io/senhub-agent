package snmppoll

import (
	"fmt"
	"strings"
)

// IF-MIB interface inventory (entity rail): the device's ports as
// network.interface entities. ifName (ifXTable) is the port identity component;
// ifOperStatus (ifTable) and ifHighSpeed (ifXTable) are descriptive.
const (
	ifName       = "1.3.6.1.2.1.31.1.1.1.1"  // ifXTable: textual port name (Gi0/1)
	ifHighSpeed  = "1.3.6.1.2.1.31.1.1.1.15" // ifXTable: speed in Mbit/s
	ifOperStatus = "1.3.6.1.2.1.2.2.1.8"     // ifTable: IF-MIB OperStatus enum

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
	OperStatus int   // raw IF-MIB enum (0 when unread)
	SpeedMbps  int64 // ifHighSpeed (Mbit/s); 0 when unread
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
	return parseInterfaces(nameBinds, speedBinds, operBinds), nil
}

func parseInterfaces(nameBinds, speedBinds, operBinds []snmpRawBind) []ifaceRow {
	rows := map[string]*ifaceRow{}
	var order []string

	for _, b := range nameBinds {
		idx, ok := strings.CutPrefix(b.OID, ifName+".")
		if !ok {
			continue
		}
		name := octetText(asBytes(b.Value))
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
				if v, ok := asIntVal(b.Value); ok {
					r.SpeedMbps = int64(v)
				}
			}
		}
	}
	for _, b := range operBinds {
		if idx, ok := strings.CutPrefix(b.OID, ifOperStatus+"."); ok {
			if r := rows[idx]; r != nil {
				if v, ok := asIntVal(b.Value); ok {
					r.OperStatus = v
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

// operStateName maps the IF-MIB ifOperStatus enum to the dotted lowercase
// oper.state attribute value (frozen casing, Toise #87). notPresent is filtered
// upstream (the interface is not emitted), so it has no name here.
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
