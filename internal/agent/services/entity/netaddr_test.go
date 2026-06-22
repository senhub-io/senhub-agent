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
