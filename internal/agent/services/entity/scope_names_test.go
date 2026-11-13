package entity

import "testing"

// TestScopeNamesAreFrozen pins the instrumentation-scope names: they are the
// provenance a backend reads, agreed with the receiving graph, so a rename
// is a contract change and must fail here first.
func TestScopeNamesAreFrozen(t *testing.T) {
	want := map[string]string{
		ScopeSNMPLLDP:  "senhub-agent/snmp-lldp",
		ScopeSNMPRoute: "senhub-agent/snmp-route",
		ScopeSNMPIFMIB: "senhub-agent/snmp-ifmib",
		ScopeSNMPFDB:   "senhub-agent/snmp-fdb",
		ScopeSNMPARP:   "senhub-agent/snmp-arp",
		ScopeHostRoute: "senhub-agent/host-route",
		ScopeHostARP:   "senhub-agent/host-arp",
		ScopeHostSvc:   "senhub-agent/host-svc",
		ScopeHostIface: "senhub-agent/host-iface",
		ScopeHostDep:   "senhub-agent/host-dep",
	}
	if len(want) != 10 {
		t.Fatalf("two scope constants share a value: %v", want)
	}
	for got, name := range want {
		if got != name {
			t.Errorf("scope %q, want %q", got, name)
		}
	}
}
