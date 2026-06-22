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
