package network

import "net"

// linkState is what an interface says about itself beside its counters:
// the speed it negotiated and whether it is operationally up. Both carry
// a flag because absent is not zero — a virtual interface has no speed,
// and that is different from a link running at nothing.
type linkState struct {
	SpeedBits float64
	HaveSpeed bool
	Up        float64
	HaveUp    bool
}

// fallbackLinkState fills in the operational state from the interface
// flags, which every platform exposes. It is what runs outside Linux,
// and on Linux when the kernel reports the state as unknown.
func fallbackLinkState(name string, out linkState) linkState {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return out
	}
	out.HaveUp = true
	if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0 {
		out.Up = 1
	}
	return out
}
