package snmppoll

import (
	"fmt"
	"strconv"
	"strings"
)

// Bridge forwarding database — dot1dTpFdbTable (BRIDGE-MIB). The classic,
// widely-implemented IEEE 802.1D FDB; the VLAN-aware dot1qTpFdbTable
// (Q-BRIDGE-MIB) is a later concern. The entry index IS the learned MAC
// (6 sub-identifiers), so walking the Port column yields both.
const (
	dot1dTpFdbPort   = "1.3.6.1.2.1.17.4.3.1.2"
	dot1dTpFdbStatus = "1.3.6.1.2.1.17.4.3.1.3"

	fdbStatusLearned = 3 // dot1dTpFdbStatus: 3 = learned
)

// fdbEntry is one learned forwarding entry.
type fdbEntry struct {
	MAC        string // lowercase colon hex (same form as macHex)
	BridgePort string
}

func collectFDB(client snmpClient) ([]fdbEntry, error) {
	portBinds, err := client.WalkRaw(dot1dTpFdbPort)
	if err != nil {
		return nil, fmt.Errorf("dot1dTpFdb walk: %w", err)
	}
	statusBinds, _ := client.WalkRaw(dot1dTpFdbStatus) // best-effort filter
	return parseFDB(portBinds, statusBinds), nil
}

func parseFDB(portBinds, statusBinds []snmpRawBind) []fdbEntry {
	status := map[string]int{}
	for _, b := range statusBinds {
		if idx, ok := strings.CutPrefix(b.OID, dot1dTpFdbStatus+"."); ok {
			if v, ok := asIntVal(b.Value); ok {
				status[idx] = v
			}
		}
	}

	var out []fdbEntry
	for _, b := range portBinds {
		idx, ok := strings.CutPrefix(b.OID, dot1dTpFdbPort+".")
		if !ok {
			continue
		}
		// Keep only learned entries when status is available (skip self/mgmt).
		if st, known := status[idx]; known && st != fdbStatusLearned {
			continue
		}
		mac := macFromIndex(idx)
		if mac == "" {
			continue
		}
		port := ""
		if v, ok := asIntVal(b.Value); ok {
			port = strconv.Itoa(v)
		}
		out = append(out, fdbEntry{MAC: mac, BridgePort: port})
	}
	return out
}

// macFromIndex turns a dot1dTpFdb index "0.17.34.51.68.85" (6 decimal MAC
// octets) into lowercase colon hex.
func macFromIndex(idx string) string {
	parts := strings.Split(idx, ".")
	if len(parts) != 6 {
		return ""
	}
	b := make([]byte, 6)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return ""
		}
		b[i] = byte(n)
	}
	return macHex(b)
}
