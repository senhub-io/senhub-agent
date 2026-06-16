package hostiface

import (
	"errors"
	"testing"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

func entityByID(obs entity.Observation, typ, key, val string) (entity.Entity, bool) {
	for _, e := range obs.Entities {
		if e.Type == typ && e.ID[key] == val {
			return e, true
		}
	}
	return entity.Entity{}, false
}

func relCount(obs entity.Observation, typ string) int {
	n := 0
	for _, r := range obs.Relations {
		if r.Type == typ {
			n++
		}
	}
	return n
}

func TestBuildObservation_InterfacesAndAddresses(t *testing.T) {
	ias := []ifaceAddrs{
		{Name: "eth0", IPs: []string{"10.0.0.5", "fd00::5"}},
		{Name: "eth1", IPs: []string{"192.168.1.20"}},
	}
	obs := buildObservation("h-1", ias)

	// 2 interfaces + 3 distinct addresses.
	if got := relCount(obs, relHasInterface); got != 2 {
		t.Errorf("has_interface count = %d, want 2", got)
	}
	if got := relCount(obs, relBoundTo); got != 3 {
		t.Errorf("bound_to count = %d, want 3", got)
	}

	eth0, ok := entityByID(obs, entityTypeNetworkInterface, idKeyInterfaceName, "eth0")
	if !ok {
		t.Fatalf("eth0 interface not emitted: %+v", obs.Entities)
	}
	if eth0.ID[idKeyHost] != "h-1" {
		t.Errorf("interface identity must carry host.id: %+v", eth0.ID)
	}

	addr, ok := entityByID(obs, entityTypeNetworkAddress, idKeyNetworkAddress, "10.0.0.5")
	if !ok {
		t.Fatalf("address 10.0.0.5 not emitted: %+v", obs.Entities)
	}
	if addr.ID[idKeyNetworkAddress] != "10.0.0.5" {
		t.Errorf("address identity = %v, want bare IP", addr.ID)
	}

	// bound_to: address -> interface; has_interface: host -> interface.
	for _, r := range obs.Relations {
		switch r.Type {
		case relBoundTo:
			if r.FromType != entityTypeNetworkAddress || r.ToType != entityTypeNetworkInterface {
				t.Errorf("bound_to must go address->interface: %+v", r)
			}
		case relHasInterface:
			if r.FromType != entityTypeHost || r.FromID[idKeyHost] != "h-1" || r.ToType != entityTypeNetworkInterface {
				t.Errorf("has_interface must go host->interface: %+v", r)
			}
		}
	}
}

func TestBuildObservation_SharedAddressEmittedOnce(t *testing.T) {
	// Same IP on two interfaces: one address entity, but bound_to each.
	ias := []ifaceAddrs{
		{Name: "eth0", IPs: []string{"10.0.0.5"}},
		{Name: "br0", IPs: []string{"10.0.0.5"}},
	}
	obs := buildObservation("h-1", ias)

	n := 0
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkAddress && e.ID[idKeyNetworkAddress] == "10.0.0.5" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("address entity emitted %d times, want once (deduped)", n)
	}
	if got := relCount(obs, relBoundTo); got != 2 {
		t.Errorf("bound_to count = %d, want 2 (one per interface)", got)
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []ifaceAddrs{{Name: "eth0", IPs: []string{"10.0.0.5"}}}); len(o.Entities) != 0 {
		t.Error("no hostID → empty")
	}
	if o := buildObservation("h", nil); len(o.Entities) != 0 {
		t.Error("no interfaces → empty")
	}
}

func TestResolvableIP_Filtering(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.0.0.5/24", "10.0.0.5"},      // private unicast, CIDR form
		{"192.168.1.20", "192.168.1.20"}, // bare unicast
		{"fd00::5/64", "fd00::5"},        // ULA unicast
		{"127.0.0.1/8", ""},              // loopback
		{"::1/128", ""},                  // loopback v6
		{"169.254.1.1/16", ""},           // link-local v4
		{"fe80::1/64", ""},               // link-local v6
		{"0.0.0.0/0", ""},                // unspecified
		{"224.0.0.1", ""},                // multicast
		{"not-an-ip", ""},                // garbage
	}
	for _, c := range cases {
		if got := resolvableIP(c.in); got != c.want {
			t.Errorf("resolvableIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnumerate_SkipsLoopbackInterfaceAndKeepsUsableIPs(t *testing.T) {
	s := New(func() string { return "h-1" })
	s.interfaces = func() (gnet.InterfaceStatList, error) {
		return gnet.InterfaceStatList{
			{Name: "lo", Flags: []string{"up", "loopback"}, Addrs: gnet.InterfaceAddrList{{Addr: "127.0.0.1/8"}}},
			{Name: "eth0", Flags: []string{"up"}, Addrs: gnet.InterfaceAddrList{
				{Addr: "10.0.0.5/24"},
				{Addr: "fe80::1/64"}, // link-local, dropped
			}},
		}, nil
	}
	ias, err := s.enumerate()
	if err != nil {
		t.Fatal(err)
	}
	if len(ias) != 1 || ias[0].Name != "eth0" {
		t.Fatalf("expected only eth0, got %+v", ias)
	}
	if len(ias[0].IPs) != 1 || ias[0].IPs[0] != "10.0.0.5" {
		t.Errorf("expected only 10.0.0.5, got %+v", ias[0].IPs)
	}
}

func TestObserve_TransientFailureKeepsCache(t *testing.T) {
	calls := 0
	s := New(func() string { return "h-1" })
	s.interfaces = func() (gnet.InterfaceStatList, error) {
		calls++
		if calls == 1 {
			return gnet.InterfaceStatList{
				{Name: "eth0", Flags: []string{"up"}, Addrs: gnet.InterfaceAddrList{{Addr: "10.0.0.5/24"}}},
			}, nil
		}
		return nil, errors.New("boom")
	}
	if _, ok := s.Observe(); !ok {
		t.Fatal("first observe should succeed")
	}
	// Force staleness so the second call re-enumerates and fails.
	s.last = s.last.Add(-2 * defaultRefresh)
	if _, ok := s.Observe(); ok {
		t.Error("transient failure must report ok=false (not delete the interfaces)")
	}
}
