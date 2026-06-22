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

func TestOperStateFromFlags(t *testing.T) {
	if got := operStateFromFlags([]string{"up", "broadcast"}); got != "up" {
		t.Errorf("flags with up = %q, want up", got)
	}
	if got := operStateFromFlags([]string{"broadcast", "multicast"}); got != "down" {
		t.Errorf("flags without up = %q, want down", got)
	}
}

func TestBuildObservation_OperStateAttribute(t *testing.T) {
	ias := []ifaceAddrs{
		{Name: "eth0", IPs: []string{"10.0.0.5"}, OperState: "up"},
		{Name: "eth1", IPs: []string{"10.0.0.6"}}, // no oper state → omitted
	}
	obs := buildObservation("h-1", ias)

	eth0, _ := entityByID(obs, entityTypeNetworkInterface, idKeyInterfaceName, "eth0")
	if eth0.Attributes[attrKeyOperState] != "up" {
		t.Errorf("eth0 oper_state = %v, want up", eth0.Attributes[attrKeyOperState])
	}
	eth1, _ := entityByID(obs, entityTypeNetworkInterface, idKeyInterfaceName, "eth1")
	if _, present := eth1.Attributes[attrKeyOperState]; present {
		t.Errorf("eth1 must omit oper_state when unknown: %+v", eth1.Attributes)
	}
}

func TestEnumerate_AttachesMetadataAndEmitsIPLess(t *testing.T) {
	s := New(func() string { return "h-1" })
	s.link = func(name string, flags []string) linkMeta {
		return linkMeta{OperState: "down", Type: "physical", Duplex: "full", Speed: 1_000_000_000}
	}
	s.interfaces = func() (gnet.InterfaceStatList, error) {
		return gnet.InterfaceStatList{
			{Name: "eth0", MTU: 1500, HardwareAddr: "aa:bb:cc:dd:ee:ff", Flags: []string{"up"},
				Addrs: gnet.InterfaceAddrList{{Addr: "10.0.0.5/24"}}},
			{Name: "eth9", MTU: 9000, HardwareAddr: "11:22:33:44:55:66", Flags: []string{}}, // no IP
		}, nil
	}
	ias, err := s.enumerate()
	if err != nil {
		t.Fatal(err)
	}
	if len(ias) != 2 {
		t.Fatalf("both NICs must be emitted incl. the IP-less one: %+v", ias)
	}
	if ias[0].MAC != "aa:bb:cc:dd:ee:ff" || ias[0].MTU != 1500 || ias[0].OperState != "down" ||
		ias[0].Type != "physical" || ias[0].Duplex != "full" || ias[0].Speed != 1_000_000_000 {
		t.Errorf("eth0 metadata wrong: %+v", ias[0])
	}
	if ias[1].Name != "eth9" || len(ias[1].IPs) != 0 {
		t.Errorf("eth9 (IP-less) must still be present: %+v", ias[1])
	}
}

// TestEnumerate_DropsIPLessVirtual pins the AT13 inventory rule (toise#231/#239):
// IP-less virtual interfaces (ephemeral veth/cni plumbing) are dropped, while
// IP-less physical NICs and IP-bearing virtual interfaces (bridges) are kept.
func TestEnumerate_DropsIPLessVirtual(t *testing.T) {
	s := New(func() string { return "h-1" })
	s.link = func(name string, flags []string) linkMeta {
		switch name {
		case "eth0":
			return linkMeta{Type: "physical"}
		case "eth1":
			return linkMeta{Type: "physical"} // IP-less physical → keep (link-down signal)
		case "veth7a3":
			return linkMeta{Type: "virtual"} // IP-less virtual → drop
		case "br0":
			return linkMeta{Type: "virtual"} // virtual WITH an IP → keep
		}
		return linkMeta{}
	}
	s.interfaces = func() (gnet.InterfaceStatList, error) {
		return gnet.InterfaceStatList{
			{Name: "eth0", Flags: []string{"up"}, Addrs: gnet.InterfaceAddrList{{Addr: "10.0.0.5/24"}}},
			{Name: "eth1", Flags: []string{}},
			{Name: "veth7a3", Flags: []string{}},
			{Name: "br0", Flags: []string{"up"}, Addrs: gnet.InterfaceAddrList{{Addr: "172.17.0.1/16"}}},
		}, nil
	}
	ias, err := s.enumerate()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ia := range ias {
		got[ia.Name] = true
	}
	if got["veth7a3"] {
		t.Errorf("IP-less virtual veth7a3 must be dropped: %+v", ias)
	}
	for _, want := range []string{"eth0", "eth1", "br0"} {
		if !got[want] {
			t.Errorf("%s must be kept: %+v", want, ias)
		}
	}
}

func TestBuildObservation_AT13AttributesAndIPLess(t *testing.T) {
	ias := []ifaceAddrs{
		{Name: "eth0", IPs: []string{"10.0.0.5"}, MAC: "aa:bb:cc:dd:ee:ff", MTU: 1500,
			OperState: "up", Type: "physical", Duplex: "full", Speed: 1_000_000_000},
		{Name: "eth9", OperState: "down", Type: "virtual"}, // no IP
	}
	obs := buildObservation("h-1", ias)

	if got := relCount(obs, relHasInterface); got != 2 {
		t.Errorf("has_interface = %d, want 2 (incl. IP-less)", got)
	}
	if got := relCount(obs, relBoundTo); got != 1 {
		t.Errorf("bound_to = %d, want 1 (only eth0 has an IP)", got)
	}
	eth0, _ := entityByID(obs, entityTypeNetworkInterface, idKeyInterfaceName, "eth0")
	a := eth0.Attributes
	if a[attrKeyMAC] != "aa:bb:cc:dd:ee:ff" || a[attrKeyMTU] != int64(1500) ||
		a[attrKeyOperState] != "up" || a[attrKeyType] != "physical" ||
		a[attrKeyDuplex] != "full" || a[attrKeySpeed] != int64(1_000_000_000) {
		t.Errorf("eth0 AT13 attributes wrong: %v", a)
	}
}

func TestBuildObservation_HostLocalAddressNotShared(t *testing.T) {
	// br0 carries the Docker bridge gateway (host-local): the interface entity is
	// kept, but 172.17.0.1 must NOT become a shared network.address (it is the
	// same value on every host). eth0's routable IP is still emitted.
	ias := []ifaceAddrs{
		{Name: "eth0", IPs: []string{"10.0.0.5"}, Type: "physical"},
		{Name: "br0", IPs: []string{"172.17.0.1"}, Type: "virtual"},
	}
	obs := buildObservation("h-1", ias)

	if got := relCount(obs, relHasInterface); got != 2 {
		t.Errorf("has_interface = %d, want 2 (br0 interface kept)", got)
	}
	if _, ok := entityByID(obs, entityTypeNetworkInterface, idKeyInterfaceName, "br0"); !ok {
		t.Error("br0 interface entity must be kept")
	}
	if got := relCount(obs, relBoundTo); got != 1 {
		t.Errorf("bound_to = %d, want 1 (only eth0's routable IP shared)", got)
	}
	if _, ok := entityByID(obs, entityTypeNetworkAddress, idKeyNetworkAddress, "172.17.0.1"); ok {
		t.Error("172.17.0.1 must NOT be emitted as a shared network.address")
	}
	if _, ok := entityByID(obs, entityTypeNetworkAddress, idKeyNetworkAddress, "10.0.0.5"); !ok {
		t.Error("10.0.0.5 (routable) must be emitted as a network.address")
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
