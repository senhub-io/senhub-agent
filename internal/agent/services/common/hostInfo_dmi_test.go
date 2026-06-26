//go:build linux || windows

package common

import "testing"

func TestClassifyVirtFromDMI(t *testing.T) {
	cases := []struct {
		name, sig, want string
	}{
		{"openstack-kvm", "openstack foundation openstack nova virtual machine seabios", "kvm"},
		{"vmware", "vmware, inc. vmware virtual platform phoenix", "vmware"},
		{"hyperv", "microsoft corporation virtual machine", "hyperv"},
		{"xen", "xen hvm domu xen", "xen"},
		{"virtualbox", "innotek gmbh virtualbox", "virtualbox"},
		{"qemu", "qemu standard pc (i440fx + piix, 1996) seabios", "kvm"},
		{"generic-vm", "acme cloud virtual machine acmebios", "unknown"},
		{"baremetal", "dell inc. poweredge r740 dell inc.", ""},
		{"empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyVirtFromDMI(c.sig); got != c.want {
				t.Fatalf("classifyVirtFromDMI(%q) = %q, want %q", c.sig, got, c.want)
			}
		})
	}
}
