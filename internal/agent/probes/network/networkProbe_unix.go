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

const primeDelay = 1 * time.Second

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

	primeWait time.Duration
	primed    bool
}

type counterWithTime struct {
	Stats     psnet.IOCountersStat
	Timestamp time.Time
}

func newNetworkCollector(config map[string]interface{}, logger *logger.Logger) (hostpoll.Collector, error) {
	includeVirtual, _ := types.BoolParam(config, "include_virtual_interfaces")
	addressLabel, _ := types.BoolParam(config, "address_label")

	return &unixNetworkCollector{
		logger:                   logger,
		lastCounters:             make(map[string]counterWithTime),
		includeVirtualInterfaces: includeVirtual,
		addressLabel:             addressLabel,
	}, nil
}

func (u *unixNetworkCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}

	counters, err := psnet.IOCounters(true)
	if err != nil {
		return nil, fmt.Errorf("error getting network metrics: %w", err)
	}

	var firstSnapshot map[string]psnet.IOCountersStat
	var firstElapsed float64

	if !u.primed {
		baseline := counters
		wait := primeDelay
		if u.primeWait != 0 {
			wait = u.primeWait
		}
		start := time.Now()
		time.Sleep(wait)
		firstElapsed = time.Since(start).Seconds()

		counters, err = psnet.IOCounters(true)
		if err != nil {
			return nil, fmt.Errorf("error getting network metrics after priming: %w", err)
		}

		u.primed = true
		firstSnapshot = make(map[string]psnet.IOCountersStat, len(baseline))
		for _, counter := range baseline {
			firstSnapshot[counter.Name] = counter
		}
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

		if firstSnapshot != nil {
			if lastCounter, exists := firstSnapshot[counter.Name]; exists {
				appendNetworkRates(&dataPoints, timestamp, interfaceTags, counter, lastCounter, firstElapsed)
			}
		} else if lastCounter, exists := u.lastCounters[counter.Name]; exists {
			timeDiff := timestamp.Sub(lastCounter.Timestamp).Seconds()
			if timeDiff > 0 {
				appendNetworkRates(&dataPoints, timestamp, interfaceTags, counter, lastCounter.Stats, timeDiff)
			}
		}

		// Save current counters for next iteration. After priming they
		// were read one wait after timestamp; dating them at timestamp
		// would spread the next delta over a longer span than it covers.
		u.lastCounters[counter.Name] = counterWithTime{
			Stats:     counter,
			Timestamp: timestamp.Add(time.Duration(firstElapsed * float64(time.Second))),
		}
	}

	return dataPoints, nil
}

func appendNetworkRates(dataPoints *[]data_store.DataPoint, timestamp time.Time, interfaceTags []tags.Tag, curr, prev psnet.IOCountersStat, elapsed float64) {
	if elapsed <= 0 {
		return
	}

	metrics := []struct {
		name  string
		value float64
	}{
		{"bytes_sent", float64(curr.BytesSent-prev.BytesSent) / elapsed},
		{"bytes_received", float64(curr.BytesRecv-prev.BytesRecv) / elapsed},
		{"packets_sent", float64(curr.PacketsSent-prev.PacketsSent) / elapsed},
		{"packets_received", float64(curr.PacketsRecv-prev.PacketsRecv) / elapsed},
		{"errors_sent", float64(curr.Errout-prev.Errout) / elapsed},
		{"errors_received", float64(curr.Errin-prev.Errin) / elapsed},
		{"discards_sent", float64(curr.Dropout-prev.Dropout) / elapsed},
		{"discards_received", float64(curr.Dropin-prev.Dropin) / elapsed},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     metric.value,
			Tags:      interfaceTags,
		})
	}
}

func (u *unixNetworkCollector) Close() error {
	return nil
}
