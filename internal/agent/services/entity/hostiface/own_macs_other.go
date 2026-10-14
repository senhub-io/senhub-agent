//go:build !linux

package hostiface

import (
	"fmt"
	"net"
)

func OwnInterfaceMACs() (map[string]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	var candidates []macOwnerCandidate
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(ifc.HardwareAddr) == 0 || allZeroMAC(ifc.HardwareAddr) {
			continue
		}
		mac := canonicalMAC(ifc.HardwareAddr.String())
		if mac == "" {
			continue
		}
		candidates = append(candidates, macOwnerCandidate{name: ifc.Name, mac: mac})
	}
	return dedupeOwnedMACs(candidates), nil
}
