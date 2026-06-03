package snmppoll

import (
	"reflect"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestResolveDeviceID_Precedence(t *testing.T) {
	mac := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	full := deviceIdentity{
		Serial: "FOC1234", EngineID: []byte{0x80, 0x01}, ChassisMAC: mac,
		SysName: "core-sw-1", MgmtIP: "192.0.2.10",
	}
	cases := []struct {
		name string
		id   deviceIdentity
		want string
	}{
		{"serial wins", full, "serial:FOC1234"},
		{"engine when no serial", deviceIdentity{EngineID: []byte{0x80, 0x01, 0x02}, ChassisMAC: mac, SysName: "x"}, "engine:800102"},
		{"mac when no serial/engine", deviceIdentity{ChassisMAC: mac, SysName: "x"}, "mac:00:11:22:33:44:55"},
		{"name when only name", deviceIdentity{SysName: "  sw1 "}, "name:sw1"},
		{"mgmt last resort v4", deviceIdentity{MgmtIP: "192.0.2.10"}, "mgmt:192.0.2.10"},
		{"mgmt canon v6", deviceIdentity{MgmtIP: "2001:DB8::1"}, "mgmt:2001:db8::1"},
		{"empty", deviceIdentity{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveDeviceID(c.id); got != c.want {
				t.Errorf("resolveDeviceID(%+v) = %q, want %q", c.id, got, c.want)
			}
		})
	}
}

func TestNeighborIdentity(t *testing.T) {
	// MAC chassis subtype → usable mac id.
	macN := lldpNeighbor{ChassisIdSubtype: subtypeMacAddress, ChassisId: []byte{0xaa, 0xbb}, SysName: "n1"}
	if got := resolveDeviceID(neighborIdentity(macN)); got != "mac:aa:bb" {
		t.Errorf("mac neighbor id = %q, want mac:aa:bb", got)
	}
	// Non-MAC chassis subtype → fall back to advertised sysName.
	localN := lldpNeighbor{ChassisIdSubtype: 7, ChassisId: []byte("edge-b"), SysName: "n2"}
	if got := resolveDeviceID(neighborIdentity(localN)); got != "name:n2" {
		t.Errorf("non-mac neighbor id = %q, want name:n2", got)
	}
}

func TestRenderPortID(t *testing.T) {
	if got := renderPortID(portSubtypeIfName, []byte("Gi0/1")); got != "Gi0/1" {
		t.Errorf("ifName port = %q, want Gi0/1", got)
	}
	if got := renderPortID(subtypeMacAddress, []byte{0xaa, 0xbb}); got != "aa:bb" {
		t.Errorf("mac port = %q, want aa:bb", got)
	}
}

func TestOctetText(t *testing.T) {
	if got := octetText([]byte("host1\x00")); got != "host1" {
		t.Errorf("printable+NUL = %q, want host1", got)
	}
	if got := octetText([]byte{0x00, 0xff, 0x10}); got != "00ff10" {
		t.Errorf("binary = %q, want hex 00ff10", got)
	}
}

func TestParseLLDPLocal(t *testing.T) {
	binds := []snmpRawBind{
		{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: 4},
		{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}},
		{OID: lldpLocSysName, Type: gosnmp.OctetString, Value: []byte("core-sw-1")},
		{OID: "1.0.8802.1.1.2.1.3.7.1.4.1", Type: gosnmp.OctetString, Value: []byte("ignored port row")},
	}
	loc := parseLLDPLocal(binds)
	if loc.ChassisIdSubtype != 4 || loc.SysName != "core-sw-1" {
		t.Fatalf("local parsed wrong: %+v", loc)
	}
	if !reflect.DeepEqual(loc.ChassisId, []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}) {
		t.Errorf("chassisId = %x", loc.ChassisId)
	}
	// And the contract-bound identity on top of the parse (chassis MAC rung):
	if got := resolveDeviceID(deviceIdentity{ChassisMAC: loc.ChassisId}); got != "mac:de:ad:be:ef:00:01" {
		t.Errorf("resolved local id = %q", got)
	}
}

func TestParseLLDPNeighbors(t *testing.T) {
	// Two rows: keys "0.5.1" (local port 5) and "0.7.2" (local port 7).
	b := func(col, row string, typ gosnmp.Asn1BER, v any) snmpRawBind {
		return snmpRawBind{OID: lldpRemEntry + "." + col + "." + row, Type: typ, Value: v}
	}
	binds := []snmpRawBind{
		b(colRemChassisIdSubtype, "0.5.1", gosnmp.Integer, 4),
		b(colRemChassisId, "0.5.1", gosnmp.OctetString, []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}),
		b(colRemPortIdSubtype, "0.5.1", gosnmp.Integer, 5),
		b(colRemPortId, "0.5.1", gosnmp.OctetString, []byte("Gi0/1")),
		b(colRemSysName, "0.5.1", gosnmp.OctetString, []byte("neighbor-a")),

		b(colRemChassisIdSubtype, "0.7.2", gosnmp.Integer, 7),
		b(colRemChassisId, "0.7.2", gosnmp.OctetString, []byte("edge-b")),
		b(colRemSysName, "0.7.2", gosnmp.OctetString, []byte("neighbor-b")),
	}

	ns := parseLLDPNeighbors(binds)
	if len(ns) != 2 {
		t.Fatalf("expected 2 neighbors, got %d (%+v)", len(ns), ns)
	}

	a := ns[0]
	if a.LocalPortNum != "5" || a.ChassisIdSubtype != 4 || a.SysName != "neighbor-a" {
		t.Errorf("neighbor a wrong: %+v", a)
	}
	if resolveDeviceID(neighborIdentity(a)) != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("neighbor a id = %q", resolveDeviceID(neighborIdentity(a)))
	}
	if renderPortID(a.PortIdSubtype, a.PortId) != "Gi0/1" {
		t.Errorf("neighbor a port = %q", renderPortID(a.PortIdSubtype, a.PortId))
	}

	bn := ns[1]
	if bn.LocalPortNum != "7" || resolveDeviceID(neighborIdentity(bn)) != "name:neighbor-b" {
		t.Errorf("neighbor b wrong: %+v -> %q", bn, resolveDeviceID(neighborIdentity(bn)))
	}
}

func TestCollectLLDP(t *testing.T) {
	fc := &fakeClient{
		walkRawResult: map[string][]snmpRawBind{
			lldpLocBase: {
				{OID: lldpLocChassisIdSubtype, Type: gosnmp.Integer, Value: 4},
				{OID: lldpLocChassisId, Type: gosnmp.OctetString, Value: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}},
				{OID: lldpLocSysName, Type: gosnmp.OctetString, Value: []byte("core-sw-1")},
			},
			lldpRemEntry: {
				{OID: lldpRemEntry + "." + colRemChassisIdSubtype + ".0.5.1", Type: gosnmp.Integer, Value: 4},
				{OID: lldpRemEntry + "." + colRemChassisId + ".0.5.1", Type: gosnmp.OctetString, Value: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}},
				{OID: lldpRemEntry + "." + colRemSysName + ".0.5.1", Type: gosnmp.OctetString, Value: []byte("neighbor-a")},
			},
		},
	}

	topo, err := collectLLDP(fc)
	if err != nil {
		t.Fatalf("collectLLDP: %v", err)
	}
	if topo.Local.SysName != "core-sw-1" {
		t.Errorf("local sysName = %q", topo.Local.SysName)
	}
	if len(topo.Neighbors) != 1 || topo.Neighbors[0].SysName != "neighbor-a" {
		t.Fatalf("neighbors = %+v", topo.Neighbors)
	}
}

func TestCollectLLDP_WalkError(t *testing.T) {
	fc := &fakeClient{walkRawErr: errContext("snmp down")}
	if _, err := collectLLDP(fc); err == nil {
		t.Fatal("expected error when WalkRaw fails")
	}
}

type errContext string

func (e errContext) Error() string { return string(e) }
