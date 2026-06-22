package snmppoll

import "testing"

func TestParseInterfaces(t *testing.T) {
	names := []snmpRawBind{
		{OID: ifName + ".1", Value: []byte("Gi0/1")},
		{OID: ifName + ".2", Value: []byte("Gi0/2")},
		{OID: ifName + ".3", Value: []byte("")}, // unnamed → dropped
	}
	speeds := []snmpRawBind{
		{OID: ifHighSpeed + ".1", Value: 1000}, // Mbit/s
		{OID: ifHighSpeed + ".2", Value: 10000},
	}
	opers := []snmpRawBind{
		{OID: ifOperStatus + ".1", Value: ifOperUp},
		{OID: ifOperStatus + ".2", Value: ifOperDown},
	}
	types := []snmpRawBind{
		{OID: ifType + ".1", Value: 6},  // ethernetCsmacd → physical
		{OID: ifType + ".2", Value: 53}, // propVirtual → virtual
	}
	mtus := []snmpRawBind{
		{OID: ifMtu + ".1", Value: 1500},
		{OID: ifMtu + ".2", Value: 9000},
	}
	macs := []snmpRawBind{
		{OID: ifPhysAddr + ".1", Value: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}},
		{OID: ifPhysAddr + ".2", Value: []byte{0, 0, 0, 0, 0, 0}}, // all-zero → ""
	}
	duplexes := []snmpRawBind{
		{OID: dot3Duplex + ".1", Value: 3}, // full
		{OID: dot3Duplex + ".2", Value: 2}, // half
	}

	rows := parseInterfaces(names, speeds, opers, types, mtus, macs, duplexes)
	if len(rows) != 2 {
		t.Fatalf("rows = %d (%+v), want 2 (unnamed dropped)", len(rows), rows)
	}
	// First-seen order preserved (ifIndex 1 then 2).
	if rows[0].Name != "Gi0/1" || rows[0].SpeedMbps != 1000 || rows[0].OperStatus != ifOperUp ||
		rows[0].IfType != 6 || rows[0].Mtu != 1500 || rows[0].Mac != "aa:bb:cc:dd:ee:ff" || rows[0].Duplex != 3 {
		t.Errorf("row[0] = %+v", rows[0])
	}
	if rows[1].Name != "Gi0/2" || rows[1].SpeedMbps != 10000 || rows[1].OperStatus != ifOperDown ||
		rows[1].IfType != 53 || rows[1].Mtu != 9000 || rows[1].Mac != "" || rows[1].Duplex != 2 {
		t.Errorf("row[1] = %+v", rows[1])
	}
}

func TestParseInterfaces_NameOnly(t *testing.T) {
	// Speed/oper walks may fail (best-effort) — a named interface still parses.
	rows := parseInterfaces([]snmpRawBind{{OID: ifName + ".7", Value: []byte("Te1/1")}}, nil, nil, nil, nil, nil, nil)
	if len(rows) != 1 || rows[0].Name != "Te1/1" || rows[0].SpeedMbps != 0 || rows[0].OperStatus != 0 {
		t.Errorf("rows = %+v, want one Te1/1 with zero speed/oper", rows)
	}
}

func TestMacString(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, "aa:bb:cc:dd:ee:ff"},
		{[]byte{0x00, 0x1b, 0x21, 0x00, 0x00, 0x01}, "00:1b:21:00:00:01"},
		{[]byte{0, 0, 0, 0, 0, 0}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := macString(c.in); got != c.want {
			t.Errorf("macString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIfTypeName(t *testing.T) {
	cases := map[int]string{
		6: "physical", 117: "physical", 24: "loopback", 71: "wireless",
		53: "virtual", 161: "virtual", 0: "", 999: "",
	}
	for in, want := range cases {
		if got := ifTypeName(in); got != want {
			t.Errorf("ifTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDuplexName(t *testing.T) {
	cases := map[int]string{2: "half", 3: "full", 1: "unknown", 0: "", 99: ""}
	for in, want := range cases {
		if got := duplexName(in); got != want {
			t.Errorf("duplexName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestOperStateName(t *testing.T) {
	cases := map[int]string{
		ifOperUp: "up", ifOperDown: "down", ifOperTesting: "testing",
		ifOperDormant: "dormant", ifOperLowerLayerDown: "lowerLayerDown",
		4: "unknown", 99: "unknown",
	}
	for in, want := range cases {
		if got := operStateName(in); got != want {
			t.Errorf("operStateName(%d) = %q, want %q", in, got, want)
		}
	}
}
