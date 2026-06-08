package snmppoll

import "testing"

func TestMacFromIndex(t *testing.T) {
	if got := macFromIndex("0.17.34.51.68.85"); got != "00:11:22:33:44:55" {
		t.Errorf("macFromIndex = %q, want 00:11:22:33:44:55", got)
	}
	if macFromIndex("1.2.3") != "" {
		t.Error("short index should yield empty")
	}
	if macFromIndex("0.17.34.51.68.999") != "" {
		t.Error("out-of-range octet should yield empty")
	}
}

func TestParseFDB(t *testing.T) {
	const learnedMAC = "0.17.34.51.68.85" // 00:11:22:33:44:55
	const otherMAC = "10.20.30.40.50.60"  // filtered by non-learned status
	port := func(idx string, p int) snmpRawBind {
		return snmpRawBind{OID: dot1dTpFdbPort + "." + idx, Value: p}
	}
	stat := func(idx string, s int) snmpRawBind {
		return snmpRawBind{OID: dot1dTpFdbStatus + "." + idx, Value: s}
	}
	rows := parseFDB(
		[]snmpRawBind{port(learnedMAC, 5), port(otherMAC, 9)},
		[]snmpRawBind{stat(learnedMAC, fdbStatusLearned), stat(otherMAC, 1)},
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d (%+v)", len(rows), rows)
	}
	if rows[0].MAC != "00:11:22:33:44:55" || rows[0].BridgePort != "5" {
		t.Errorf("row = %+v", rows[0])
	}
}
