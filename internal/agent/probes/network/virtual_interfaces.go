package network

import "strings"

// virtualInterfacePrefixes returns the case-sensitive prefixes skipped by
// default. The shared entity helper is intentionally not used here: it is a
// lower-case superset for address identity, while this default skips exactly the
// product-defined names.
func virtualInterfacePrefixes() []string {
	return []string{
		"veth", "docker", "br-", "cni", "flannel", "cali", "cilium",
		"vxlan", "weave", "virbr", "lxcbr", "kube-ipvs", "tunl", "genev",
	}
}

// isVirtualInterfaceName reports whether an interface name starts with one of
// the container/virtualisation prefixes skipped by default.
func isVirtualInterfaceName(name string) bool {
	for _, prefix := range virtualInterfacePrefixes() {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// shouldMonitorInterfaceName reports whether an interface name should be
// collected. includeVirtual=true restores the previous behaviour and accepts
// every name.
func shouldMonitorInterfaceName(name string, includeVirtual bool) bool {
	return includeVirtual || !isVirtualInterfaceName(name)
}

// isWindowsVirtualAdapter reports whether a Windows adapter is a container
// or virtualisation interface, by its connection name. Hyper-V "vEthernet"
// adapters are kept: on a Hyper-V host the external virtual switch carries
// the host's own address, and dropping it would drop the main interface.
func isWindowsVirtualAdapter(connectionName, _ string) bool {
	return isVirtualInterfaceName(connectionName)
}
