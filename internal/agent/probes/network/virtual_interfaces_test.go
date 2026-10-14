package network

import "testing"

func TestShouldMonitorInterfaceName(t *testing.T) {
	tests := []struct {
		name           string
		includeVirtual bool
		want           bool
	}{
		{"eth0", false, true},
		{"docker0", false, false},
		{"veth1a2b", false, false},
		{"cni0", false, false},
		{"eth0", true, true},
		{"docker0", true, true},
		{"veth1a2b", true, true},
		{"cni0", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldMonitorInterfaceName(tt.name, tt.includeVirtual); got != tt.want {
				t.Fatalf("shouldMonitorInterfaceName(%q, %v) = %v, want %v", tt.name, tt.includeVirtual, got, tt.want)
			}
		})
	}
}

func TestIsWindowsVirtualAdapter(t *testing.T) {
	tests := []struct {
		name           string
		connectionName string
		adapterName    string
		want           bool
	}{
		{"connection matches virtual prefix", "flannel.1", "Hyper-V", true},
		{"Hyper-V vEthernet adapter is kept (it may carry the host's address)", "vEthernet (External)", "Hyper-V Virtual Ethernet Adapter", false},
		{"physical adapter", "Ethernet0", "Intel I210", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWindowsVirtualAdapter(tt.connectionName, tt.adapterName); got != tt.want {
				t.Fatalf("isWindowsVirtualAdapter(%q, %q) = %v, want %v", tt.connectionName, tt.adapterName, got, tt.want)
			}
		})
	}
}
