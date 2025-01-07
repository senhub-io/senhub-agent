//go:build windows

// internal/agent/probes/host/networkProbe_windows.go
//
package host

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

var networkCounterPaths = map[string]MetricDefinition{
	"bytes_sent": {
		path:     "\\Network Interface\\Bytes Sent/sec",
		instance: "*",
	},
	"bytes_received": {
		path:     "\\Network Interface\\Bytes Received/sec",
		instance: "*",
	},
	"packets_sent": {
		path:     "\\Network Interface\\Packets Sent/sec",
		instance: "*",
	},
	"packets_received": {
		path:     "\\Network Interface\\Packets Received/sec",
		instance: "*",
	},
	"errors_sent": {
		path:     "\\Network Interface\\Packets Outbound Errors",
		instance: "*",
	},
	"errors_received": {
		path:     "\\Network Interface\\Packets Received Errors",
		instance: "*",
	},
	"discards_sent": {
		path:     "\\Network Interface\\Packets Outbound Discarded",
		instance: "*",
	},
	"discards_received": {
		path:     "\\Network Interface\\Packets Received Discarded",
		instance: "*",
	},
}

type windowsNetworkCollector struct {
	query       *pdh.Query
	paths       map[string]pathInfo
	mu          sync.Mutex
	initialized bool
}

func newNetworkCollector(config map[string]interface{}, logger *logger.Logger) (osNetworkCollector, error) {
	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	collector := &windowsNetworkCollector{
		query: query,
		paths: make(map[string]pathInfo),
	}

	if err := collector.initializeCounters(); err != nil {
		query.Close()
		return nil, err
	}

	if err := query.Collect(); err != nil {
		query.Close()
		return nil, fmt.Errorf("failed initial collection: %v", err)
	}

	return collector, nil
}

func (w *windowsNetworkCollector) initializeCounters() error {
	fmt.Printf("Initializing Network probe with counters\n")

	for metricName, def := range networkCounterPaths {
		if def.instance == "*" {
			instances, err := pdh.GetInstancesList("Network Interface", false)
			if err != nil {
				return fmt.Errorf("failed to get Network Interface instances: %v", err)
			}

			for _, instance := range instances {
				path := pdh.BuildCounterPath(def.path, instance)
				w.paths[fmt.Sprintf("%s|%s", metricName, instance)] = pathInfo{
					path:     path,
					instance: instance,
				}

				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (instance %s): %v", metricName, instance, err)
				}
				fmt.Printf("Added counter %s for interface %s\n", metricName, instance)
			}
		}
	}
	return nil
}

func (w *windowsNetworkCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized {
		if err := w.query.Collect(); err != nil {
			return nil, fmt.Errorf("failed initial sample collection: %v", err)
		}
		time.Sleep(1 * time.Second)
		w.initialized = true
	}

	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %v", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	for name, pathInfo := range w.paths {
		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			fmt.Printf("Error getting counter value for %s: %v\n", name, err)
			continue
		}

		metricTags := append([]tags.Tag{}, baseTags...)
		metricTags = append(metricTags, tags.Tag{
			Key:     "interface",
			Value:   pathInfo.instance,
			Private: false,
		})

		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      strings.Split(name, "|")[0],
			Timestamp: timestamp,
			Value:     float32(value),
			Tags:      metricTags,
		})

		fmt.Printf("Collected metric %s = %f for interface %s\n", name, value, pathInfo.instance)
	}

	return dataPoints, nil
}

func (w *windowsNetworkCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}
