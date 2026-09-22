//go:build !linux

package network

// interfaceLink has no sysfs to read outside Linux: the operational
// state is taken from the interface flags and the negotiated speed is
// simply not reported rather than guessed.
func interfaceLink(name string) linkState {
	return fallbackLinkState(name, linkState{})
}
