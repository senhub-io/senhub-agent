package snmppoll

import "testing"

func TestParseIPAddrs(t *testing.T) {
	// ipAdEntIfIndex is indexed by the IP itself; value is the ifIndex.
	binds := []snmpRawBind{
		{OID: ipAdEntIfIndex + ".10.0.0.1", Value: 1},
		{OID: ipAdEntIfIndex + ".127.0.0.1", Value: 1},   // loopback → dropped
		{OID: ipAdEntIfIndex + ".0.0.0.0", Value: 2},     // unspecified → dropped
		{OID: ipAdEntIfIndex + ".192.168.1.5", Value: 3}, // kept
	}
	addrs := parseIPAddrs(binds)
	if len(addrs) != 2 {
		t.Fatalf("addrs = %+v, want 2 (loopback + unspecified dropped)", addrs)
	}
	got := map[string]string{}
	for _, a := range addrs {
		got[a.IP] = a.IfIndex
	}
	if got["10.0.0.1"] != "1" || got["192.168.1.5"] != "3" {
		t.Errorf("parsed = %+v, want 10.0.0.1→1 and 192.168.1.5→3", got)
	}
}
