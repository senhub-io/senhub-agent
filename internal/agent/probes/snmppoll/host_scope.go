package snmppoll

import (
	"errors"
	"fmt"
	"net"
	"sort"

	"senhub-agent.go/internal/agent/services/entity/hostnet"
)

// defaultHostScope reads the host's default gateways and the private IPv4
// networks of its up, non-loopback interfaces. Each half is returned even
// when the other fails, so a platform without a route reader still bounds
// the crawl to its own networks.
func defaultHostScope() ([]string, []*net.IPNet, error) {
	var errs []error
	gateways, err := hostnet.DefaultGateways()
	if err != nil {
		errs = append(errs, fmt.Errorf("reading default gateways: %w", err))
	}
	networks, err := hostPrivateNetworks()
	if err != nil {
		errs = append(errs, fmt.Errorf("listing host networks: %w", err))
	}
	return gateways, networks, errors.Join(errs...)
}

func hostPrivateNetworks() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var out []*net.IPNet
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			} else {
				continue
			}
			if !ip.IsPrivate() {
				continue
			}
			prefix, bits := ipnet.Mask.Size()
			if bits != 32 {
				continue
			}
			if prefix < 16 {
				prefix = 16
			}
			mask := net.CIDRMask(prefix, 32)
			network := &net.IPNet{IP: ip.Mask(mask), Mask: mask}
			key := network.String()
			if !seen[key] {
				seen[key] = true
				out = append(out, network)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].String() < out[j].String()
	})
	return out, nil
}
