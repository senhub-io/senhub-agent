// internal/agent/probes/host/networkProbe_unix.go
//go:build !windows

package network

import (
	"fmt"
	"net"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type interfaceInfo struct {
	isMonitored bool
	addresses   []string
	err         error
	// gone marks the interface as having disappeared between the counter
	// enumeration and the lookup. On a container host the veth interfaces
	// come and go constantly, so this is the expected outcome of a race,
	// not a failure worth an operator's attention.
	gone bool
}

func getValidIPAddresses(addrs []net.Addr) []string {
	var validIPs []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// Skip link-local addresses
		if ipNet.IP.IsLinkLocalUnicast() {
			continue
		}

		validIPs = append(validIPs, ipNet.IP.String())
	}
	return validIPs
}

func (u *unixNetworkCollector) isInterfaceMonitored(interfaceName string) interfaceInfo {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return interfaceInfo{
			isMonitored: false,
			err:         fmt.Errorf("error getting interface %s: %w", interfaceName, err),
			gone:        true,
		}
	}

	// Skip if interface is loopback
	if iface.Flags&net.FlagLoopback != 0 {
		u.logger.Debug().
			Str("interface", iface.Name).
			Msgf("Interface %s is loopback, skipping", interfaceName)
		return interfaceInfo{isMonitored: false}
	}

	// Check if interface is up
	if iface.Flags&net.FlagUp == 0 {
		u.logger.Debug().
			Str("interface", iface.Name).
			Msgf("Interface %s is not up, skipping", interfaceName)
		return interfaceInfo{isMonitored: false}
	}

	// Check if interface is running
	if iface.Flags&net.FlagRunning == 0 {
		u.logger.Debug().
			Str("interface", iface.Name).
			Msgf("Interface %s is not running, skipping", interfaceName)
		return interfaceInfo{isMonitored: false}
	}

	// Check if interface has addresses
	addresses, err := iface.Addrs()
	if err != nil {
		return interfaceInfo{
			isMonitored: false,
			err:         fmt.Errorf("error getting addresses for interface %s: %w", interfaceName, err),
		}
	}

	validIPs := getValidIPAddresses(addresses)
	if len(validIPs) == 0 {
		return interfaceInfo{isMonitored: false}
	}

	return interfaceInfo{
		isMonitored: true,
		addresses:   validIPs,
	}
}

type unixNetworkCollector struct {
	logger       *logger.Logger
	lastCounters map[string]counterWithTime
}

type counterWithTime struct {
	Stats     psnet.IOCountersStat
	Timestamp time.Time
}

func newNetworkCollector(_ map[string]interface{}, logger *logger.Logger) (hostpoll.Collector, error) {
	return &unixNetworkCollector{
		logger:       logger,
		lastCounters: make(map[string]counterWithTime),
	}, nil
}

func (u *unixNetworkCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return nil, fmt.Errorf("error getting network metrics: %w", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}

	dataPoints := make([]data_store.DataPoint, 0)

	for _, counter := range counters {
		// Check if interface should be monitored and get its addresses
		interfaceInfo := u.isInterfaceMonitored(counter.Name)
		if interfaceInfo.err != nil {
			if interfaceInfo.gone {
				u.logger.Debug().
					Str("interface", counter.Name).
					Err(interfaceInfo.err).
					Msg("Interface disappeared between enumeration and lookup, skipping")
			} else {
				u.logger.Warn().
					Str("interface", counter.Name).
					Err(interfaceInfo.err).
					Msg("Cannot read interface status, skipping")
			}
			continue
		}
		if !interfaceInfo.isMonitored {
			continue
		}

		// Create base tags for this interface
		interfaceTags := append([]tags.Tag{}, baseTags...)
		interfaceTags = append(interfaceTags, tags.Tag{
			Key:     "interface",
			Value:   counter.Name,
			Private: false,
		})
		// interface.name ties this series to its network.interface entity,
		// which is keyed on the same name. On Unix it holds the same value as
		// `interface` above; both are emitted because the entity join must not
		// depend on which platform produced the series (#748), and because
		// `interface` is renamed to network.interface.name downstream while
		// the identity key must survive verbatim.
		interfaceTags = append(interfaceTags, tags.Tag{
			Key:     interfaceNameTag,
			Value:   counter.Name,
			Private: false,
		})

		// Add the primary IP address as a tag. Earlier versions emitted
		// every IP via positional ip_1, ip_2, ... ip_N labels — interfaces
		// with multiple IPv6 addresses produced a cardinality explosion in
		// Prometheus and an unstable label set order. The primary IP is
		// enough to identify the interface; secondary IPs (link-local,
		// IPv6 temporary addresses, etc.) belong in a separate enrichment
		// channel if ever needed.
		if len(interfaceInfo.addresses) > 0 {
			interfaceTags = append(interfaceTags, tags.Tag{
				Key:     "ip",
				Value:   interfaceInfo.addresses[0],
				Private: false,
			})
		}

		// What the link itself says: the speed it negotiated and whether
		// it is operationally up. Gauges, so they are emitted on every
		// tick rather than waiting for a second sample like the rates.
		link := interfaceLink(counter.Name)
		if link.HaveSpeed {
			dataPoints = append(dataPoints, data_store.DataPoint{
				Name:      "interface_speed",
				Timestamp: timestamp,
				Value:     link.SpeedBits,
				Tags:      interfaceTags,
			})
		}
		if link.HaveUp {
			dataPoints = append(dataPoints, data_store.DataPoint{
				Name:      "interface_up",
				Timestamp: timestamp,
				Value:     link.Up,
				Tags:      interfaceTags,
			})
		}

		// Calculate rates per second if we have previous data
		if lastCounter, exists := u.lastCounters[counter.Name]; exists {
			timeDiff := timestamp.Sub(lastCounter.Timestamp).Seconds()
			if timeDiff > 0 {
				metrics := []struct {
					name  string
					value float64
				}{
					{"bytes_sent", float64(counter.BytesSent-lastCounter.Stats.BytesSent) / timeDiff},
					{"bytes_received", float64(counter.BytesRecv-lastCounter.Stats.BytesRecv) / timeDiff},
					{"packets_sent", float64(counter.PacketsSent-lastCounter.Stats.PacketsSent) / timeDiff},
					{"packets_received", float64(counter.PacketsRecv-lastCounter.Stats.PacketsRecv) / timeDiff},
					{"errors_sent", float64(counter.Errout-lastCounter.Stats.Errout) / timeDiff},
					{"errors_received", float64(counter.Errin-lastCounter.Stats.Errin) / timeDiff},
					{"discards_sent", float64(counter.Dropout-lastCounter.Stats.Dropout) / timeDiff},
					{"discards_received", float64(counter.Dropin-lastCounter.Stats.Dropin) / timeDiff},
				}

				for _, metric := range metrics {
					dataPoints = append(dataPoints, data_store.DataPoint{
						Name:      metric.name,
						Timestamp: timestamp,
						Value:     metric.value,
						Tags:      interfaceTags,
					})
				}
			}
		}

		// Save current counters for next iteration
		u.lastCounters[counter.Name] = counterWithTime{
			Stats:     counter,
			Timestamp: timestamp,
		}
	}

	return dataPoints, nil
}

func (u *unixNetworkCollector) Close() error {
	return nil
}
