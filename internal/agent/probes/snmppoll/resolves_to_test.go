package snmppoll

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity/macindex"
)

const (
	ownMAC     = "00-50-56-AA-BB-01"
	foreignMAC = "00-50-56-CC-DD-02"
	switchID   = "serial:9:SW1"
)

func resolveTestSource(t *testing.T, enabled bool) *snmpEntitySource {
	t.Helper()
	s := newEntitySource(&config{
		Target:       "192.0.2.20",
		MaxNeighbors: defaultMaxNeighbors,
	}, testLogger(t))
	s.hostID = func() string { return "host-1" }
	s.hostInterfaceMACs = func() (map[string]string, error) {
		return map[string]string{ownMAC: "en0"}, nil
	}
	idx := macindex.New(time.Hour)
	idx.SetEnabled(enabled)
	s.macIndex = idx
	return s
}

func switchSweep() sweepResult {
	return sweepResult{
		deviceID: switchID,
		ifNames:  map[string]string{"7": "Ethernet7", "8": "Ethernet8"},
		fdb: []fdbEntry{
			{MAC: ownMAC, IfIndex: 7},
			{MAC: foreignMAC, IfIndex: 8},
		},
	}
}

func TestResolvesTo_DiscoveredDeviceRegistersItsMACs(t *testing.T) {
	s := resolveTestSource(t, true)
	s.registerInterfaceMacs("serial:9:R1", []ifaceRow{{Index: "1", Name: "Ethernet1", Mac: "00-1C-73-00-00-01"}})
	got, ok := s.macIndex.Lookup("00-1C-73-00-00-01")
	if !ok || got.DeviceID != "serial:9:R1" || got.Name != "Ethernet1" {
		t.Fatalf("lookup = %+v, %v", got, ok)
	}
}
