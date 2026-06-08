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

	rows := parseInterfaces(names, speeds, opers)
	if len(rows) != 2 {
		t.Fatalf("rows = %d (%+v), want 2 (unnamed dropped)", len(rows), rows)
	}
	// First-seen order preserved (ifIndex 1 then 2).
	if rows[0].Name != "Gi0/1" || rows[0].SpeedMbps != 1000 || rows[0].OperStatus != ifOperUp {
		t.Errorf("row[0] = %+v", rows[0])
	}
	if rows[1].Name != "Gi0/2" || rows[1].SpeedMbps != 10000 || rows[1].OperStatus != ifOperDown {
		t.Errorf("row[1] = %+v", rows[1])
	}
}

func TestParseInterfaces_NameOnly(t *testing.T) {
	// Speed/oper walks may fail (best-effort) — a named interface still parses.
	rows := parseInterfaces([]snmpRawBind{{OID: ifName + ".7", Value: []byte("Te1/1")}}, nil, nil)
	if len(rows) != 1 || rows[0].Name != "Te1/1" || rows[0].SpeedMbps != 0 || rows[0].OperStatus != 0 {
		t.Errorf("rows = %+v, want one Te1/1 with zero speed/oper", rows)
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
