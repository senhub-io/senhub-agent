package entity

import "testing"

func TestIsHostLocalAddressStr(t *testing.T) {
	// hostLocal=true means "must NOT be emitted as a shared network.address".
	cases := map[string]bool{
		// Globally-unique → emittable.
		"10.0.0.1":       false,
		"192.168.1.50":   false,
		"172.16.0.1":     false, // RFC1918 but NOT the Docker default bridge
		"172.18.0.1":     false, // a Docker user-defined bridge is /16 18+, not 17
		"8.8.8.8":        false,
		"2001:db8::1":    false,
		"203.0.113.7/24": false, // CIDR form is parsed
		// Host-local → must be skipped (the contract's named set + Toise's list).
		"172.17.0.1":     true, // Docker default bridge gateway — same on every host
		"172.17.255.254": true, // anywhere in 172.17.0.0/16
		"127.0.0.1":      true, // loopback
		"127.1.2.3":      true,
		"::1":            true,
		"0.0.0.0":        true, // wildcard
		"::":             true,
		"169.254.10.20":  true, // link-local
		"fe80::1":        true,
		"224.0.0.1":      true, // multicast
		"not-an-ip":      true, // unparseable is never a shared identity
		"":               true,
	}
	for in, want := range cases {
		if got := IsHostLocalAddressStr(in); got != want {
			t.Errorf("IsHostLocalAddressStr(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCanonicalHostScopedAddr(t *testing.T) {
	cases := []struct {
		in        string
		canonical string
		scoped    bool
		ok        bool
	}{
		{"127.0.0.1", "127.0.0.1", true, true},
		{"127.5.6.7", "127.5.6.7", true, true},
		{"::1", "::1", true, true},
		{"169.254.169.254", "169.254.169.254", true, true}, // cloud metadata
		{"fe80::1", "fe80::1", true, true},
		{"fe80::1%eth0", "fe80::1%eth0", true, true},       // zone kept
		{"fe80::1%ETH0", "fe80::1%eth0", true, true},       // zone lowercased
		{"::ffff:127.0.0.1", "127.0.0.1", true, true},      // IPv4-mapped unmapped → loopback
		{"8.8.8.8", "8.8.8.8", false, true},                // routable
		{"2001:db8::1", "2001:db8::1", false, true},        // routable v6
		{"2001:DB8:0:0:0:0:0:1", "2001:db8::1", false, true}, // RFC 5952 canonical
		{"not-an-ip", "", false, false},
		{"", "", false, false},
	}
	for _, tc := range cases {
		gotC, gotS, gotOK := CanonicalHostScopedAddr(tc.in)
		if gotOK != tc.ok || gotS != tc.scoped || gotC != tc.canonical {
			t.Errorf("CanonicalHostScopedAddr(%q) = (%q,%v,%v), want (%q,%v,%v)",
				tc.in, gotC, gotS, gotOK, tc.canonical, tc.scoped, tc.ok)
		}
	}
}

func TestIsContainerBridgeIface(t *testing.T) {
	cases := map[string]bool{
		"docker0":         true,
		"br-dc4ddc994709": true, // Docker user-defined bridge (carries 172.18+)
		"virbr0":          true, // libvirt
		"cni0":            true,
		"flannel.1":       true,
		"lxcbr0":          true,
		// Real, routed interfaces must NOT match.
		"eth0":    false,
		"ens3":    false,
		"bond0":   false,
		"br0":     false, // a plain router bridge (no dash) keeps its address
		"bridge0": false,
		"vlan100": false,
		"":        false,
	}
	for in, want := range cases {
		if got := IsContainerBridgeIface(in); got != want {
			t.Errorf("IsContainerBridgeIface(%q) = %v, want %v", in, got, want)
		}
	}
}
