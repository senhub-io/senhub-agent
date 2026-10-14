package hostnet

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/macindex"
)

func newTestNeighborSource(hostID string) *NeighborSource {
	src := NewNeighborSource(func() string { return hostID })
	src.macIndex = macindex.New(time.Minute)
	return src
}

func mustObserve(t *testing.T, src *NeighborSource) entity.Observation {
	t.Helper()
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	return obs
}

func TestNeighborSourceObserveBuildsEntitiesScopeAndRelations(t *testing.T) {
	src := newTestNeighborSource("host-1")
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{
			{IP: "192.0.2.10", MAC: "aa-bb-cc-dd-ee-ff", Iface: "eth0", State: "reachable"},
			{IP: "192.0.2.10", MAC: "aa-bb-cc-dd-ee-ff", Iface: "eth0", State: "stale"},
			{IP: "192.0.2.11", MAC: "aa-bb-cc-dd-ee-ff", Iface: "eth1", State: "reachable"},
			{IP: "192.0.2.10", MAC: "AA-BB-CC-DD-EE-FF", Iface: "eth2", State: "reachable"},
		}, nil
	}

	obs := mustObserve(t, src)

	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "network.neighbor" {
		t.Fatalf("entity type = %q, want network.neighbor", e.Type)
	}
	wantID := map[string]any{"host.id": "host-1", "mac": "AA-BB-CC-DD-EE-FF"}
	if !reflect.DeepEqual(e.ID, wantID) {
		t.Fatalf("entity ID = %#v, want %#v", e.ID, wantID)
	}
	addrs, ok := e.Attributes["network.neighbor.addresses"].([]string)
	if !ok || !reflect.DeepEqual(addrs, []string{"192.0.2.10", "192.0.2.11"}) {
		t.Fatalf("addresses = %#v, ok=%v; want sorted deduped [192.0.2.10 192.0.2.11]", e.Attributes["network.neighbor.addresses"], ok)
	}
	if iface := e.Attributes["network.neighbor.observed.interface"]; iface != "eth0" {
		t.Fatalf("observed.interface = %#v, want eth0", iface)
	}
	if e.Scope != entity.ScopeHostARP {
		t.Fatalf("scope = %q, want %q", e.Scope, entity.ScopeHostARP)
	}
	if len(obs.Relations) != 3 {
		t.Fatalf("got %d relations, want 3", len(obs.Relations))
	}
	for i, wantIface := range []string{"eth0", "eth1", "eth2"} {
		rel := obs.Relations[i]
		if rel.Type != "neighbor_of" || rel.FromType != "network.neighbor" || rel.ToType != "network.interface" {
			t.Fatalf("relation %d = %#v", i, rel)
		}
		if !reflect.DeepEqual(rel.FromID, wantID) {
			t.Fatalf("relation %d FromID = %#v, want %#v", i, rel.FromID, wantID)
		}
		wantToID := map[string]any{"host.id": "host-1", "network.interface.name": wantIface}
		if !reflect.DeepEqual(rel.ToID, wantToID) {
			t.Fatalf("relation %d ToID = %#v, want %#v", i, rel.ToID, wantToID)
		}
	}
}

func TestNeighborSourceFiltersRepeatedRanges(t *testing.T) {
	src := newTestNeighborSource("host-1")
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{
			{IP: "169.254.0.10", MAC: "00-11-22-33-44-55", Iface: "eth0", State: "reachable"},
			{IP: "fe80::1", MAC: "00-11-22-33-44-55", Iface: "eth1", State: "reachable"},
			{IP: "172.17.0.2", MAC: "00-11-22-33-44-55", Iface: "eth2", State: "reachable"},
			{IP: "10.88.0.2", MAC: "00-11-22-33-44-55", Iface: "eth3", State: "reachable"},
		}, nil
	}

	obs := mustObserve(t, src)
	if len(obs.Entities) != 0 {
		t.Fatalf("got %d entities, want 0", len(obs.Entities))
	}
}

// KVM and most hypervisors hand out locally administered MACs
// (52-54-00-...): filtering them would hide every VM neighbour.
func TestNeighborSourceKeepsLocallyAdministeredMAC(t *testing.T) {
	src := newTestNeighborSource("host-1")
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{
			{IP: "198.51.100.1", MAC: "52-54-00-33-44-55", Iface: "eth0", State: "reachable"},
			{IP: "198.51.100.2", MAC: "52-54-00-33-44-55", Iface: "eth0", State: "reachable"},
		}, nil
	}

	obs := mustObserve(t, src)
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	addrs, ok := obs.Entities[0].Attributes["network.neighbor.addresses"].([]string)
	if !ok || !reflect.DeepEqual(addrs, []string{"198.51.100.1", "198.51.100.2"}) {
		t.Fatalf("addresses = %#v, ok=%v; want [198.51.100.1 198.51.100.2]", obs.Entities[0].Attributes["network.neighbor.addresses"], ok)
	}
}

func TestNeighborSourceResolvesToDeviceInterface(t *testing.T) {
	idx := macindex.New(time.Minute)
	idx.SetEnabled(true)
	idx.Register("aa-bb-cc-dd-ee-ff", "device-1", "Ethernet1")

	src := newTestNeighborSource("host-1")
	src.macIndex = idx
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{
			{IP: "192.0.2.10", MAC: "aa-bb-cc-dd-ee-ff", Iface: "eth0", State: "reachable"},
		}, nil
	}

	obs := mustObserve(t, src)

	var neighborOf []entity.Relation
	var resolvesTo []entity.Relation
	for _, rel := range obs.Relations {
		switch rel.Type {
		case relNeighborOf:
			neighborOf = append(neighborOf, rel)
		case relResolvesTo:
			resolvesTo = append(resolvesTo, rel)
		}
	}
	if len(neighborOf) != 1 {
		t.Fatalf("got %d neighbor_of relations, want 1", len(neighborOf))
	}
	if len(resolvesTo) != 1 {
		t.Fatalf("got %d resolves_to relations, want 1", len(resolvesTo))
	}

	rel := resolvesTo[0]
	if rel.FromType != entityTypeNetworkNeighbor || rel.ToType != entityTypeNetworkInterface {
		t.Fatalf("resolves_to relation = %#v, want network.neighbor -> network.interface", rel)
	}
	wantToID := map[string]any{idKeyNetworkDevice: "device-1", idKeyInterfaceName: "Ethernet1"}
	if !reflect.DeepEqual(rel.ToID, wantToID) {
		t.Fatalf("resolves_to ToID = %#v, want %#v", rel.ToID, wantToID)
	}
	if got := rel.Attributes[attrBasis]; got != "arp" {
		t.Fatalf("basis = %#v, want arp", got)
	}
	if got := rel.Attributes[attrConfidence]; got != 0.9 {
		t.Fatalf("confidence = %#v, want 0.9", got)
	}
}

func TestNeighborSourceResolvesToFlagOff(t *testing.T) {
	idx := macindex.New(time.Minute)
	idx.Register("aa-bb-cc-dd-ee-ff", "device-1", "Ethernet1")

	src := newTestNeighborSource("host-1")
	src.macIndex = idx
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{
			{IP: "192.0.2.10", MAC: "aa-bb-cc-dd-ee-ff", Iface: "eth0", State: "reachable"},
		}, nil
	}

	obs := mustObserve(t, src)
	for _, rel := range obs.Relations {
		if rel.Type == relResolvesTo {
			t.Fatalf("flag off emitted resolves_to: %#v", rel)
		}
	}
}

func TestNeighborSourceReadErrorReturnsFalse(t *testing.T) {
	src := newTestNeighborSource("host-1")
	src.readNeighbors = func() ([]hostNeighbor, error) { return nil, errors.New("boom") }

	if _, ok := src.Observe(); ok {
		t.Fatal("Observe returned ok=true on read error, want false")
	}
}

func TestNeighborSourceEmptyHostIDReturnsFalse(t *testing.T) {
	src := newTestNeighborSource("")
	src.readNeighbors = func() ([]hostNeighbor, error) {
		return []hostNeighbor{{IP: "192.0.2.1", MAC: "00-11-22-33-44-55", Iface: "eth0", State: "reachable"}}, nil
	}

	if _, ok := src.Observe(); ok {
		t.Fatal("Observe returned ok=true with empty host id, want false")
	}
}
