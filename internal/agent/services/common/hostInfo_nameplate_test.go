package common

import "testing"

func TestNormalizeVirtualization(t *testing.T) {
	cases := []struct {
		system, role, want string
	}{
		{"kvm", "guest", "kvm"},
		{"vmware", "guest", "vmware"},
		{"microsoft", "guest", "hyperv"},
		{"virtualbox", "guest", "virtualbox"},
		{"", "guest", "unknown"},        // virtualized but undetected
		{"weirdhv", "guest", "unknown"}, // unrecognized system
		{"kvm", "host", "none"},         // a hypervisor host is bare metal itself
		{"", "", "none"},                // bare metal
	}
	for _, c := range cases {
		if got := normalizeVirtualization(c.system, c.role); got != c.want {
			t.Errorf("normalizeVirtualization(%q,%q) = %q, want %q", c.system, c.role, got, c.want)
		}
	}
}

func TestMhzToHz(t *testing.T) {
	cases := []struct {
		mhz  float64
		want int64
	}{
		{2100, 2100000000},
		{2593.906, 2593906000},
		// 2112.006 * 1e6 = 2112005999.9999998 in float64 — truncation would
		// yield 2112005999; the frozen contract wants the rounded integer.
		{2112.006, 2112006000},
		{0, 0},
		{-1, 0},
	}
	for _, c := range cases {
		if got := mhzToHz(c.mhz); got != c.want {
			t.Errorf("mhzToHz(%v) = %d, want %d", c.mhz, got, c.want)
		}
	}
}

func TestChassisName(t *testing.T) {
	cases := []struct {
		code int
		virt string
		want string
	}{
		{7, "none", "desktop"}, // tower
		{10, "none", "laptop"}, // notebook
		{23, "none", "server"}, // rack mount
		{28, "none", "blade"},  // blade
		{1, "kvm", "vm"},       // Other + virtualized → vm
		{2, "vmware", "vm"},    // Unknown + virtualized → vm
		{1, "none", "other"},   // Other + bare metal → other
		{0, "none", "other"},   // no DMI, bare metal → other
		{0, "kvm", "vm"},       // no DMI but virtualized → vm
	}
	for _, c := range cases {
		if got := chassisName(c.code, c.virt); got != c.want {
			t.Errorf("chassisName(%d,%q) = %q, want %q", c.code, c.virt, got, c.want)
		}
	}
}
