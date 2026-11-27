//go:build !windows

package network

import (
	"fmt"
	"net"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
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
	if !shouldMonitorInterfaceName(interfaceName, u.includeVirtualInterfaces) {
		u.logger.Debug().
			Str("interface", interfaceName).
			Msg("Virtual/container interface skipped by default")
		return interfaceInfo{isMonitored: false}
	}

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
	logger                   *logger.Logger
	lastCounters             map[string]counterWithTime
	includeVirtualInterfaces bool
	addressLabel             bool
	tcp                      tcpSource
	tcpStats                 bool
}

type counterWithTime struct {
	Stats     psnet.IOCountersStat
	Timestamp time.Time
}

func newNetworkCollector(config map[string]interface{}, logger *logger.Logger) (hostpoll.Collector, error) {
	includeVirtual, _ := types.BoolParam(config, "include_virtual_interfaces")
	addressLabel, _ := types.BoolParam(config, "address_label")
	tcpStats, ok := types.BoolParam(config, "tcp_stats")
	if !ok {
		tcpStats = true
	}

	return &unixNetworkCollector{
		logger:                   logger,
		lastCounters:             make(map[string]counterWithTime),
		includeVirtualInterfaces: includeVirtual,
		addressLabel:             addressLabel,
		tcp:                      defaultTCPSource(),
		tcpStats:                 tcpStats,
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
		// network.interface.name ties this series to its network.interface
		// entity, which is keyed on the same name. On Unix it holds the same
		// value as `interface` above; both are emitted because the entity
		// join must not depend on which platform produced the series (#748),
		// and on Windows the two differ.
		interfaceTags = append(interfaceTags, tags.Tag{
			Key:     interfaceNameTag,
			Value:   counter.Name,
			Private: false,
		})

		// The primary address label is opt-in. An address changes on a DHCP
		// renewal or an IPv6 temporary-address rotation; keeping it out of the
		// default series avoids a new series for every interface metric on each
		// rotation.
		interfaceTags = appendIPLabel(interfaceTags, u.addressLabel, firstIP(interfaceInfo.addresses))

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
			metrics := []struct {
				name string
				cur  uint64
				prev uint64
			}{
				{"bytes_sent", counter.BytesSent, lastCounter.Stats.BytesSent},
				{"bytes_received", counter.BytesRecv, lastCounter.Stats.BytesRecv},
				{"packets_sent", counter.PacketsSent, lastCounter.Stats.PacketsSent},
				{"packets_received", counter.PacketsRecv, lastCounter.Stats.PacketsRecv},
				{"errors_sent", counter.Errout, lastCounter.Stats.Errout},
				{"errors_received", counter.Errin, lastCounter.Stats.Errin},
				{"discards_sent", counter.Dropout, lastCounter.Stats.Dropout},
				{"discards_received", counter.Dropin, lastCounter.Stats.Dropin},
			}

			for _, metric := range metrics {
				rate, ok := counterRate(metric.cur, metric.prev, timeDiff)
				if !ok {
					continue
				}
				dataPoints = append(dataPoints, data_store.DataPoint{
					Name:      metric.name,
					Timestamp: timestamp,
					Value:     rate,
					Tags:      interfaceTags,
				})
			}
		}

		// Save current counters for next iteration
		u.lastCounters[counter.Name] = counterWithTime{
			Stats:     counter,
			Timestamp: timestamp,
		}
	}

	if u.tcpStats {
		pts, errs := hostTCPDataPoints(u.tcp, baseTags, timestamp)
		dataPoints = append(dataPoints, pts...)
		for _, err := range errs {
			u.logger.Debug().Err(err).Msg("Host TCP counters unavailable")
		}
	}
	return dataPoints, nil
}

func (u *unixNetworkCollector) Close() error {
	return nil
}

// counterRate computes a per-second rate from two cumulative counters.
// If the counter went backwards, the interface has been reset (counter
// zeroed or wrapped); the interval says nothing useful, so report false
// and let the next reading start from the new base value.
func counterRate(cur, prev uint64, seconds float64) (float64, bool) {
	if seconds <= 0 || cur < prev {
		return 0, false
	}
	return float64(cur-prev) / seconds, true
}
