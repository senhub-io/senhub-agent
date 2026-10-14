package snmppoll

import (
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/snmpcore"
)

const (
	dot1qVlanStaticEntry = "1.3.6.1.2.1.17.7.1.4.3.1"
	colVlanName          = "1"
	colVlanEgress        = "2"
	colVlanUntagged      = "4"

	dot1qPvid = "1.3.6.1.2.1.17.7.1.4.5.1.1"
)

type portVLANs struct {
	IfIndex  int
	PVID     int
	Tagged   []int
	Untagged []int
}

func parseVLANs(staticBinds, pvidBinds, basePortBinds []snmpRawBind) (map[int]portVLANs, map[int]string) {
	vlanNames := map[int]string{}
	egressByVLAN := map[int][]int{}
	untaggedByVLAN := map[int][]int{}

	prefixStatic := dot1qVlanStaticEntry + "."
	for _, b := range staticBinds {
		rest, ok := strings.CutPrefix(b.OID, prefixStatic)
		if !ok {
			continue
		}
		col, vlanStr, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		vlan, err := strconv.Atoi(vlanStr)
		if err != nil {
			continue
		}
		switch col {
		case colVlanName:
			vlanNames[vlan] = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
		case colVlanEgress:
			egressByVLAN[vlan] = append(egressByVLAN[vlan], parsePortList(snmpcore.AsBytes(b.Value))...)
		case colVlanUntagged:
			untaggedByVLAN[vlan] = append(untaggedByVLAN[vlan], parsePortList(snmpcore.AsBytes(b.Value))...)
		}
	}

	pvidByPort := map[int]int{}
	prefixPVID := dot1qPvid + "."
	for _, b := range pvidBinds {
		rest, ok := strings.CutPrefix(b.OID, prefixPVID)
		if !ok {
			continue
		}
		basePort, err := strconv.Atoi(rest)
		if err != nil {
			continue
		}
		if v, ok := snmpcore.AsInt(b.Value); ok {
			pvidByPort[basePort] = v
		}
	}

	portIfIndex := parseBridgePortIfIndex(basePortBinds)

	vlanSet := map[int]bool{}
	for v := range egressByVLAN {
		vlanSet[v] = true
	}
	for v := range untaggedByVLAN {
		vlanSet[v] = true
	}
	vlanOrder := make([]int, 0, len(vlanSet))
	for v := range vlanSet {
		vlanOrder = append(vlanOrder, v)
	}
	sort.Ints(vlanOrder)

	taggedByPort := map[int]map[int]bool{}
	untaggedByPort := map[int]map[int]bool{}
	for _, vlan := range vlanOrder {
		for _, port := range egressByVLAN[vlan] {
			if intsContain(untaggedByVLAN[vlan], port) {
				addPortVLAN(untaggedByPort, port, vlan)
			} else {
				addPortVLAN(taggedByPort, port, vlan)
			}
		}
		// An untagged list without a matching egress port is still untagged.
		for _, port := range untaggedByVLAN[vlan] {
			if taggedByPort[port] != nil {
				delete(taggedByPort[port], vlan)
			}
			addPortVLAN(untaggedByPort, port, vlan)
		}
	}

	allBasePorts := map[int]bool{}
	for port := range portIfIndex {
		allBasePorts[port] = true
	}
	for port := range pvidByPort {
		allBasePorts[port] = true
	}
	for port := range taggedByPort {
		allBasePorts[port] = true
	}
	for port := range untaggedByPort {
		allBasePorts[port] = true
	}

	out := map[int]portVLANs{}
	for basePort := range allBasePorts {
		ifIndex, ok := portIfIndex[basePort]
		if !ok {
			continue
		}
		out[ifIndex] = portVLANs{
			IfIndex:  ifIndex,
			PVID:     pvidByPort[basePort],
			Tagged:   sortedSet(taggedByPort[basePort]),
			Untagged: sortedSet(untaggedByPort[basePort]),
		}
	}
	return out, vlanNames
}

func parsePortList(bs []byte) []int {
	var ports []int
	for i, b := range bs {
		for bit := 0; bit < 8; bit++ {
			if b&(0x80>>bit) != 0 {
				ports = append(ports, i*8+bit+1)
			}
		}
	}
	return ports
}

func addPortVLAN(m map[int]map[int]bool, port, vlan int) {
	if m[port] == nil {
		m[port] = map[int]bool{}
	}
	m[port][vlan] = true
}

func intsContain(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func sortedSet(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}
