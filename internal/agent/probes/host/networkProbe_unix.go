//go:build !windows

// internal/agent/probes/host/networkProbe_unix.go
//
package host

import (
	"fmt"
	psnet "github.com/shirou/gopsutil/v3/net" // Renommé pour éviter le conflit
	"net"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

type counterWithTime struct {
	Stats     psnet.IOCountersStat
	Timestamp time.Time
}

type unixNetworkCollector struct {
	logger       *logger.Logger
	lastCounters map[string]counterWithTime
}

func newNetworkCollector(config map[string]interface{}, logger *logger.Logger) (osNetworkCollector, error) {
	return &unixNetworkCollector{
		logger:       logger,
		lastCounters: make(map[string]counterWithTime),
	}, nil
}

func (u *unixNetworkCollector) isInterfaceMonitored(interfaceName string) (bool, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return false, fmt.Errorf("error getting interface %s: %v", interfaceName, err)
	}

	// Exclure si l'interface est loopback
	if iface.Flags&net.FlagLoopback != 0 {
		fmt.Printf("Interface %s is loopback, skipping\n", interfaceName)
		return false, nil
	}

	// Vérifier si l'interface est up
	if iface.Flags&net.FlagUp == 0 {
		fmt.Printf("Interface %s is not up, skipping\n", interfaceName)
		return false, nil
	}

	// Vérifier si l'interface est running
	if iface.Flags&net.FlagRunning == 0 {
		fmt.Printf("Interface %s is not running, skipping\n", interfaceName)
		return false, nil
	}

	return true, nil
}

func (u *unixNetworkCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return nil, fmt.Errorf("error getting network metrics: %v", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	dataPoints := make([]data_store.DataPoint, 0)

	for _, counter := range counters {
		// Vérifier si l'interface doit être monitorée
		shouldMonitor, err := u.isInterfaceMonitored(counter.Name)
		if err != nil {
			fmt.Printf("Error checking interface %s status: %v\n", counter.Name, err)
			continue
		}
		if !shouldMonitor {
			continue
		}

		interfaceTags := append([]tags.Tag{}, baseTags...)
		interfaceTags = append(interfaceTags, tags.Tag{
			Key:     "interface",
			Value:   counter.Name,
			Private: false,
		})

		// Calcul des taux par seconde si nous avons des données précédentes
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
						Value:     float32(metric.value),
						Tags:      interfaceTags,
					})
				}
			}
		}

		// Sauvegarde des compteurs actuels pour la prochaine itération
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
