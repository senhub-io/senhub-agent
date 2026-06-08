package snmppoll

import "testing"

func TestParseARP(t *testing.T) {
	// index = ifIndex.a.b.c.d ; value = PhysAddress (6 octets).
	rows := parseARP([]snmpRawBind{
		{OID: ipNetToMediaPhysAddress + ".2.10.0.0.254", Value: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}},
		{OID: ipNetToMediaPhysAddress + ".2.10.0.0.0", Value: []byte{}}, // empty MAC → skipped
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d (%+v)", len(rows), rows)
	}
	r := rows[0]
	if r.IfIndex != "2" || r.IP != "10.0.0.254" || r.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("row = %+v", r)
	}
}
