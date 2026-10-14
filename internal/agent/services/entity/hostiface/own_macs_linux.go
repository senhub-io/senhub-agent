//go:build linux

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
	links, err := readNetlinkLinks()
	if err != nil {
		return nil, err
	}
	byIndex := make(map[int]linuxLink, len(links))
	for _, l := range links {
		byIndex[l.ifindex] = l
	}

	var candidates []macOwnerCandidate
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		link, haveLink := byIndex[ifc.Index]
		// Bond/team masters borrow their MAC; they do not own one.
		if haveLink && (link.kind == "bond" || link.kind == "team") {
			continue
		}

		var mac []byte
		if haveLink && link.master != 0 {
			if master, ok := byIndex[link.master]; ok && (master.kind == "bond" || master.kind == "team") {
				mac = link.permMAC
			}
		}
		if mac == nil {
			mac = ifc.HardwareAddr
		}
		if len(mac) == 0 || allZeroMAC(mac) {
			continue
		}
		canon := canonicalMAC(net.HardwareAddr(mac).String())
		if canon == "" {
			continue
		}
		candidates = append(candidates, macOwnerCandidate{name: ifc.Name, mac: canon})
	}
	return dedupeOwnedMACs(candidates), nil
}
