package hostnet

import "testing"

func TestHexLEToIP(t *testing.T) {
	cases := map[string]string{
		"0101A8C0": "192.168.1.1", // C0.A8.01.01 little-endian
		"00000000": "0.0.0.0",
		"0A0A0A0A": "10.10.10.10",
		"bad":      "",
	}
	for in, want := range cases {
		if got := hexLEToIP(in); got != want {
			t.Errorf("hexLEToIP(%q) = %q, want %q", in, got, want)
		}
	}
}

const routeSample = `Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask	MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	0	00000000	0	0	0
eth0	0001A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0
`

const arpSample = `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         AA:BB:CC:DD:EE:FF     *        eth0
192.168.1.50     0x1         0x0         00:00:00:00:00:00     *        eth0
`

func TestParseProcRoute(t *testing.T) {
	gws := parseProcRoute([]byte(routeSample))
	if len(gws) != 1 || gws[0] != "192.168.1.1" {
		t.Fatalf("gateways = %+v, want [192.168.1.1]", gws)
	}
}

func TestParseProcARP(t *testing.T) {
	arp := parseProcARP([]byte(arpSample))
	if arp["192.168.1.1"] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("arp[192.168.1.1] = %q (lowercased)", arp["192.168.1.1"])
	}
	if _, ok := arp["192.168.1.50"]; ok {
		t.Error("incomplete (zero-MAC) entry should be dropped")
	}
}

func TestBuildObservation_ConvergesGatewayViaARP(t *testing.T) {
	obs := buildObservation("h1", []string{"192.168.1.1"}, map[string]string{"192.168.1.1": "aa:bb:cc:dd:ee:ff"})
	if len(obs.Entities) != 1 || obs.Entities[0].ID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Fatalf("entities = %+v (want gateway converged to mac:)", obs.Entities)
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("relations = %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != relRoutesVia || r.FromType != entityTypeHost || r.FromID[idKeyHost] != "h1" ||
		r.ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("relation wrong: %+v", r)
	}
	if r.Attributes["source"] != "host-route" {
		t.Errorf("expected source=host-route, got %v", r.Attributes["source"])
	}
}

func TestBuildObservation_MgmtWhenNoARP(t *testing.T) {
	obs := buildObservation("h1", []string{"192.168.1.1"}, nil)
	if obs.Entities[0].ID[idKeyNetworkDevice] != "mgmt:192.168.1.1" {
		t.Errorf("gateway id = %v, want mgmt:192.168.1.1", obs.Entities[0].ID)
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []string{"192.168.1.1"}, nil); len(o.Entities) != 0 {
		t.Error("no hostID → empty")
	}
	if o := buildObservation("h1", nil, nil); len(o.Entities) != 0 {
		t.Error("no gateways → empty")
	}
}

func TestObserve_InjectedReaders(t *testing.T) {
	s := &Source{
		hostID:    func() string { return "h1" },
		readRoute: func() ([]byte, error) { return []byte(routeSample), nil },
		readARP:   func() ([]byte, error) { return []byte(arpSample), nil },
	}
	obs := s.Observe()
	if len(obs.Entities) != 1 || len(obs.Relations) != 1 {
		t.Fatalf("observe = %+v", obs)
	}
	if obs.Relations[0].ToID[idKeyNetworkDevice] != "mac:aa:bb:cc:dd:ee:ff" {
		t.Errorf("gateway should converge via ARP, got %v", obs.Relations[0].ToID)
	}
}
