package snmppoll

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

func ownBindingTestResult() sweepResult {
	return sweepResult{
		deviceID: "serial:9:SW1",
		ifaces: []ifaceRow{
			{Index: "1", Name: "Ethernet1", Mac: ownMAC},
			{Index: "2", Name: "Ethernet2", Mac: foreignMAC},
		},
		addrs: []ipAddr{
			{IP: "198.51.100.10", IfIndex: "1", Prefix: 24},
			{IP: "198.51.100.20", IfIndex: "2", Prefix: 24},
		},
	}
}

func hasNetworkAddressEntity(obs entity.Observation, ip string) bool {
	for _, ent := range obs.Entities {
		if ent.Type == entityTypeNetworkAddress {
			if id, ok := ent.ID[idKeyNetworkAddress].(string); ok && id == ip {
				return true
			}
		}
	}
	return false
}

func hasBoundToRelation(obs entity.Observation, fromIP, toName string) bool {
	for _, rel := range obs.Relations {
		if rel.Type != relBoundTo {
			continue
		}
		if rel.FromType != entityTypeNetworkAddress || rel.ToType != entityTypeNetworkInterface {
			continue
		}
		from, okFrom := rel.FromID[idKeyNetworkAddress].(string)
		if !okFrom || from != fromIP {
			continue
		}
		if dev, okDev := rel.ToID[idKeyNetworkDevice].(string); !okDev || dev != "serial:9:SW1" {
			continue
		}
		if name, okName := rel.ToID[idKeyInterfaceName].(string); okName && name == toName {
			return true
		}
	}
	return false
}

func TestBuildDropsBoundToForOwnInterfaceAddress(t *testing.T) {
	s := resolveTestSource(t, false)
	res := ownBindingTestResult()

	for _, a := range res.addrs {
		if !entity.AddressEdgeAllowed(a.IP) {
			t.Fatalf("test address %s rejected by AddressEdgeAllowed", a.IP)
		}
	}

	obs, _ := s.build(res, time.Now())

	if !hasNetworkAddressEntity(obs, "198.51.100.10") {
		t.Error("network.address entity for 198.51.100.10 missing")
	}
	if !hasNetworkAddressEntity(obs, "198.51.100.20") {
		t.Error("network.address entity for 198.51.100.20 missing")
	}
	if hasBoundToRelation(obs, "198.51.100.10", "Ethernet1") {
		t.Error("bound_to relation from own-interface address 198.51.100.10 should be omitted")
	}
	if !hasBoundToRelation(obs, "198.51.100.20", "Ethernet2") {
		t.Error("bound_to relation from 198.51.100.20 to Ethernet2 should remain")
	}
}

func TestBuildKeepsBoundToWhenHostMACScanFails(t *testing.T) {
	s := resolveTestSource(t, false)
	s.hostInterfaceMACs = func() (map[string]string, error) {
		return nil, errors.New("boom")
	}
	res := ownBindingTestResult()

	obs, _ := s.build(res, time.Now())

	if !hasBoundToRelation(obs, "198.51.100.10", "Ethernet1") {
		t.Error("bound_to relation from 198.51.100.10 should be kept when host MAC scan fails")
	}
	if !hasBoundToRelation(obs, "198.51.100.20", "Ethernet2") {
		t.Error("bound_to relation from 198.51.100.20 should be kept when host MAC scan fails")
	}
}
