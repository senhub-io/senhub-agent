package snmppoll

import (
	"fmt"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/snmpcore"
)

const (
	dot1qTpFdbEntry   = "1.3.6.1.2.1.17.7.1.2.2.1"
	colDot1qFDBPort   = "2"
	colDot1qFDBStatus = "3"

	dot1dTpFdbEntry   = "1.3.6.1.2.1.17.4.3.1"
	colDot1dFDBPort   = "2"
	colDot1dFDBStatus = "3"

	dot1dBasePortIfIndex = "1.3.6.1.2.1.17.1.4.1.2"

	fdbStatusLearned = 3
)

type fdbEntry struct {
	MAC     string
	IfIndex int
	VLAN    int
}

type fdbRow struct {
	mac    string
	vlan   int
	port   int
	status int
}

func collectFDB(client snmpClient) ([]fdbEntry, error) {
	qBinds, qErr := client.WalkRaw(dot1qTpFdbEntry)
	legacyBinds, lErr := client.WalkRaw(dot1dTpFdbEntry)
	basePortBinds, bErr := client.WalkRaw(dot1dBasePortIfIndex)
	if qErr != nil && lErr != nil && bErr != nil {
		return nil, fmt.Errorf("bridge FDB walks: q-bridge %w; legacy %v; basePort %v", qErr, lErr, bErr)
	}
	return parseFDBTables(qBinds, legacyBinds, basePortBinds), nil
}

func parseFDBTables(qBinds, legacyBinds, basePortBinds []snmpRawBind) []fdbEntry {
	portIfIndex := parseBridgePortIfIndex(basePortBinds)
	if rows := parseQBridgeFDB(qBinds, portIfIndex); len(rows) > 0 {
		return rows
	}
	return parseLegacyFDB(legacyBinds, portIfIndex)
}

func parseBridgePortIfIndex(binds []snmpRawBind) map[int]int {
	out := map[int]int{}
	prefix := dot1dBasePortIfIndex + "."
	for _, b := range binds {
		idx, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		basePort, err := strconv.Atoi(idx)
		if err != nil {
			continue
		}
		if ifIndex, ok := snmpcore.AsInt(b.Value); ok {
			out[basePort] = ifIndex
		}
	}
	return out
}

func parseQBridgeFDB(binds []snmpRawBind, portIfIndex map[int]int) []fdbEntry {
	rows := map[string]*fdbRow{}
	var order []string
	prefix := dot1qTpFdbEntry + "."

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
			if len(p) != 7 {
				continue
			}
			fdbID, err := strconv.Atoi(p[0])
			if err != nil {
				continue
			}
			macBytes := macIndexBytes(p, 1)
			mac := validUnicastMAC(macBytes)
			if mac == "" {
				continue
			}
			r = &fdbRow{mac: mac, vlan: fdbID}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colDot1qFDBPort:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.port = v
			}
		case colDot1qFDBStatus:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.status = v
			}
		}
	}

	out := make([]fdbEntry, 0, len(order))
	for _, k := range order {
		r := rows[k]
		if r.status != fdbStatusLearned {
			continue
		}
		ifIndex, ok := portIfIndex[r.port]
		if !ok {
			continue
		}
		out = append(out, fdbEntry{MAC: r.mac, IfIndex: ifIndex, VLAN: r.vlan})
	}
	return out
}

func parseLegacyFDB(binds []snmpRawBind, portIfIndex map[int]int) []fdbEntry {
	rows := map[string]*fdbRow{}
	var order []string
	prefix := dot1dTpFdbEntry + "."

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
			macBytes := macIndexBytes(p, 0)
			mac := validUnicastMAC(macBytes)
			if mac == "" {
				continue
			}
			r = &fdbRow{mac: mac}
			rows[rowKey] = r
			order = append(order, rowKey)
		}
		switch col {
		case colDot1dFDBPort:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.port = v
			}
		case colDot1dFDBStatus:
			if v, ok := snmpcore.AsInt(b.Value); ok {
				r.status = v
			}
		}
	}

	out := make([]fdbEntry, 0, len(order))
	for _, k := range order {
		r := rows[k]
		if r.status != fdbStatusLearned {
			continue
		}
		ifIndex, ok := portIfIndex[r.port]
		if !ok {
			continue
		}
		out = append(out, fdbEntry{MAC: r.mac, IfIndex: ifIndex, VLAN: r.vlan})
	}
	return out
}

func macIndexBytes(p []string, start int) []byte {
	if len(p)-start != 6 {
		return nil
	}
	out := make([]byte, 6)
	for i := range out {
		v, err := strconv.Atoi(p[start+i])
		if err != nil || v < 0 || v > 255 {
			return nil
		}
		out[i] = byte(v)
	}
	return out
}
