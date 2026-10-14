package snmppoll

import (
	"reflect"
	"testing"
)

func TestParseARPTablesPrimary(t *testing.T) {
	primary := []snmpRawBind{
		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.1", Value: []byte{0x00, 0x0c, 0x29, 0x11, 0x22, 0x33}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.1", Value: 4},
		{OID: ipNetToPhysicalEntry + ".7.1.1.4.192.0.2.1", Value: 1},

		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.2", Value: []byte{0x00, 0x0c, 0x29, 0x44, 0x55, 0x66}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.2", Value: 3},
	}
	got := parseARPTables(primary, nil)
	want := []arpEntry{
		{IfIndex: 1, IP: "192.0.2.1", MAC: "00-0C-29-11-22-33", Static: true},
		{IfIndex: 1, IP: "192.0.2.2", MAC: "00-0C-29-44-55-66", Static: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseARPTablesFallsBackToLegacy(t *testing.T) {
	legacy := []snmpRawBind{
		{OID: ipNetToMediaEntry + ".2.7.192.0.2.10", Value: []byte{0x00, 0x0c, 0x29, 0x11, 0x22, 0x33}},
		{OID: ipNetToMediaEntry + ".4.7.192.0.2.10", Value: 4},
	}
	want := []arpEntry{
		{IfIndex: 7, IP: "192.0.2.10", MAC: "00-0C-29-11-22-33", Static: true},
	}

	got := parseARPTables(nil, legacy)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	// A primary table with only unusable rows behaves like an empty table.
	badPrimary := []snmpRawBind{
		{OID: ipNetToPhysicalEntry + ".4.1.3.4.192.0.2.9", Value: []byte{0x00, 0x0c, 0x29, 0x11, 0x22, 0x33}},
		{OID: ipNetToPhysicalEntry + ".6.1.3.4.192.0.2.9", Value: 4},
	}
	got = parseARPTables(badPrimary, legacy)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback after bad primary got %#v, want %#v", got, want)
	}
}

func TestParseARPDropsUnusableRows(t *testing.T) {
	binds := []snmpRawBind{
		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.1", Value: []byte{0x00, 0x0c, 0x29, 0x11, 0x22, 0x33}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.1", Value: 2}, // invalid
		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.2", Value: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.2", Value: 4},
		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.3", Value: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.3", Value: 4},
		{OID: ipNetToPhysicalEntry + ".4.1.1.4.192.0.2.4", Value: []byte{0x01, 0x00, 0x5e, 0x00, 0x00, 0x01}},
		{OID: ipNetToPhysicalEntry + ".6.1.1.4.192.0.2.4", Value: 4},
		{OID: ipNetToPhysicalEntry + ".4.2.3.4.192.0.2.9", Value: []byte{0x00, 0x0c, 0x29, 0x11, 0x22, 0x33}},
		{OID: ipNetToPhysicalEntry + ".6.2.3.4.192.0.2.9", Value: 4},
	}
	got := parseIPNetToPhysicalTable(binds)
	if len(got) != 0 {
		t.Fatalf("expected no usable rows, got %#v", got)
	}
}

func TestParseFDBQBridgeAndFallback(t *testing.T) {
	basePorts := []snmpRawBind{
		{OID: dot1dBasePortIfIndex + ".1", Value: 101},
		{OID: dot1dBasePortIfIndex + ".2", Value: 102},
	}

	qRowKey := "1.0.12.41.17.34.51"
	q := []snmpRawBind{
		{OID: dot1qTpFdbEntry + ".2." + qRowKey, Value: 1},
		{OID: dot1qTpFdbEntry + ".3." + qRowKey, Value: 3},
		// self entry
		{OID: dot1qTpFdbEntry + ".2.1.0.12.41.17.34.99", Value: 2},
		{OID: dot1qTpFdbEntry + ".3.1.0.12.41.17.34.99", Value: 4},
		// learned, but no bridge-port to ifIndex mapping
		{OID: dot1qTpFdbEntry + ".2.1.0.12.41.17.34.98", Value: 99},
		{OID: dot1qTpFdbEntry + ".3.1.0.12.41.17.34.98", Value: 3},
	}
	got := parseFDBTables(q, nil, basePorts)
	want := []fdbEntry{{MAC: "00-0C-29-11-22-33", IfIndex: 101, VLAN: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("q-bridge got %#v, want %#v", got, want)
	}

	legacyRowKey := "0.12.41.17.34.52"
	legacy := []snmpRawBind{
		{OID: dot1dTpFdbEntry + ".2." + legacyRowKey, Value: 1},
		{OID: dot1dTpFdbEntry + ".3." + legacyRowKey, Value: 3},
	}
	got = parseFDBTables(nil, legacy, basePorts)
	want = []fdbEntry{{MAC: "00-0C-29-11-22-34", IfIndex: 101, VLAN: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy fallback got %#v, want %#v", got, want)
	}
}

func TestParsePortListUsesMSBFirstOctetAsPort1(t *testing.T) {
	got := parsePortList([]byte{0x81, 0x01})
	want := []int{1, 8, 16}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseVLANs(t *testing.T) {
	basePorts := []snmpRawBind{
		{OID: dot1dBasePortIfIndex + ".1", Value: 101},
		{OID: dot1dBasePortIfIndex + ".2", Value: 102},
	}
	static := []snmpRawBind{
		{OID: dot1qVlanStaticEntry + ".1.10", Value: []byte("vlan10")},
		{OID: dot1qVlanStaticEntry + ".2.10", Value: []byte{0xC0, 0x00}}, // ports 1 and 2
		{OID: dot1qVlanStaticEntry + ".4.10", Value: []byte{0x80, 0x00}}, // port 1 untagged
		{OID: dot1qVlanStaticEntry + ".1.20", Value: []byte("vlan20")},
		{OID: dot1qVlanStaticEntry + ".2.20", Value: []byte{0x40, 0x00}}, // port 2 tagged
	}
	pvid := []snmpRawBind{
		{OID: dot1qPvid + ".1", Value: 10},
		{OID: dot1qPvid + ".2", Value: 20},
	}

	gotPorts, gotNames := parseVLANs(static, pvid, basePorts)
	want101 := portVLANs{IfIndex: 101, PVID: 10, Tagged: []int{}, Untagged: []int{10}}
	want102 := portVLANs{IfIndex: 102, PVID: 20, Tagged: []int{10, 20}, Untagged: []int{}}
	if !reflect.DeepEqual(gotPorts[101], want101) {
		t.Fatalf("port 101 got %#v, want %#v", gotPorts[101], want101)
	}
	if !reflect.DeepEqual(gotPorts[102], want102) {
		t.Fatalf("port 102 got %#v, want %#v", gotPorts[102], want102)
	}
	wantNames := map[int]string{10: "vlan10", 20: "vlan20"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names got %#v, want %#v", gotNames, wantNames)
	}
}

func TestParseCDPNeighbors(t *testing.T) {
	rowKey := "5.42"
	binds := []snmpRawBind{
		{OID: cdpCacheEntry + ".3." + rowKey, Value: 1},
		{OID: cdpCacheEntry + ".4." + rowKey, Value: []byte{192, 0, 2, 10}},
		{OID: cdpCacheEntry + ".6." + rowKey, Value: []byte("switch.example")},
		{OID: cdpCacheEntry + ".7." + rowKey, Value: []byte("Gig1/0/1")},
		{OID: cdpCacheEntry + ".8." + rowKey, Value: []byte("Cisco IOS XE")},
		{OID: cdpCacheEntry + ".9." + rowKey, Value: 0x09}, // router + switch
	}
	got := parseCDPNeighbors(binds)
	want := []cdpNeighbor{
		{
			LocalIfIndex: 5,
			DeviceID:     "switch.example",
			Port:         "Gig1/0/1",
			Platform:     "Cisco IOS XE",
			MgmtIP:       "192.0.2.10",
			Capabilities: []string{"router", "switch"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDeviceClassFromCaps(t *testing.T) {
	tests := []struct {
		name        string
		caps        any
		sysObjectID string
		want        string
	}{
		{"lldp router", []byte{0x08}, "", "router"},
		{"lldp bridge", []byte{0x20}, "", "switch"},
		{"lldp access point", []byte{0x10}, "", "access_point"},
		{"lldp router wins over bridge", []byte{0x08 | 0x20}, "", "router"},
		{"lldp other bit", []byte{0x80}, "", "other"},
		{"cdp router wins over switch", []string{"router", "switch"}, "", "router"},
		{"cdp bridge", []string{"bridge"}, "", "switch"},
		{"cdp host only", []string{"host"}, "", "other"},
		{"firewall fortinet", []string{"router"}, "1.3.6.1.4.1.12356.100.1", "firewall"},
		{"firewall palo alto", []byte{0x08}, "1.3.6.1.4.1.25461.1", "firewall"},
		{"non firewall prefix", []byte{0x08}, "1.3.6.1.4.1.19999.1", "router"},
		{"freebsd is not firewall", []byte{0x08}, "1.3.6.1.4.1.12325.1", "router"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceClassFromCaps(tt.caps, tt.sysObjectID); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
