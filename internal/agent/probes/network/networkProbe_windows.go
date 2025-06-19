//senhub-agent/internal/agent/probes/host/networkProbe_windows.go
//go:build windows

package network

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/yusufpapurcu/wmi"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// MetricDefinition defines a performance counter with its path and instance
type MetricDefinition struct {
	path     string
	instance string
}

// pathInfo represents path information for performance counters
type pathInfo struct {
	path     string
	instance string
}

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

type Win32_NetworkAdapter struct {
	Name            string
	NetConnectionID string
	PhysicalAdapter bool
	NetEnabled      bool
}

type interfaceInfo struct {
	adapterName    string   // Physical adapter name (from WMI)
	connectionName string   // Connection name (WMI NetConnectionID)
	ipv4           string   // Primary IPv4 address
	ipv6           string   // Primary IPv6 address
	otherIPs       []string // Other IP addresses
	enabled        bool     // Connection state
}

type windowsNetworkCollector struct {
	query       *pdh.Query
	paths       map[string]pathInfo
	interfaces  map[string]interfaceInfo // key = PDH name
	mu          sync.Mutex
	initialized bool
	logger      *logger.ModuleLogger
}

// Helper function to normalize adapter names for comparison
func normalizeAdapterName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Remove trademarks and special symbols
	replacements := [][2]string{
		{"(r)", ""},
		{"[r]", ""},
		{"(tm)", ""},
		{"[tm]", ""},
		{"®", ""},
		{"™", ""},
		{"(", ""},
		{")", ""},
		{"[", ""},
		{"]", ""},
		{"-", " "},
		{"_", " "},
	}

	for _, r := range replacements {
		name = strings.ReplaceAll(name, r[0], r[1])
	}

	// Reduce multiple spaces to single space
	name = strings.Join(strings.Fields(name), " ")
	return strings.TrimSpace(name)
}

func getNetworkInterfaces(logger *logger.ModuleLogger) (map[string]interfaceInfo, error) {
	logger.Debug().Msg("Starting Network Interfaces Detection")

	// 1. WMI Query
	var adapters []Win32_NetworkAdapter
	query := "SELECT Name, NetConnectionID, PhysicalAdapter, NetEnabled FROM Win32_NetworkAdapter WHERE PhysicalAdapter=True"
	logger.Debug().Str("wmi_query", query).Msg("Executing WMI query")

	if err := wmi.Query(query, &adapters); err != nil {
		logger.Debug().Err(err).Msg("WMI query failed")
		return nil, fmt.Errorf("failed to get network adapters from WMI: %v", err)
	}
	logger.Debug().Int("adapter_count", len(adapters)).Msg("Found physical adapters from WMI")

	// 2. Debug WMI results
	logger.Debug().Msg("Network Adapters (WMI)")
	for _, adapter := range adapters {
		logger.Debug().
			Str("name", adapter.Name).
			Str("connection", adapter.NetConnectionID).
			Bool("enabled", adapter.NetEnabled).
			Msg("WMI Adapter")
	}

	// 3. Create mapping
	type adapterInfo struct {
		name    string
		enabled bool
	}
	connectionToAdapter := make(map[string]adapterInfo)
	for _, adapter := range adapters {
		if adapter.NetConnectionID != "" {
			connectionToAdapter[adapter.NetConnectionID] = adapterInfo{
				name:    adapter.Name,
				enabled: adapter.NetEnabled,
			}
			logger.Debug().
				Str("connection", adapter.NetConnectionID).
				Str("adapter", adapter.Name).
				Bool("enabled", adapter.NetEnabled).
				Msg("Registered adapter mapping")
		}
	}

	// 4. Get system interfaces
	logger.Debug().Msg("Retrieving system network interfaces")
	netInterfaces, err := net.Interfaces()
	if err != nil {
		logger.Debug().Err(err).Msg("Failed to get system interfaces")
		return nil, fmt.Errorf("failed to get system network interfaces: %v", err)
	}
	logger.Debug().Int("interface_count", len(netInterfaces)).Msg("Found system interfaces")

	// 5. Get PDH instances
	logger.Debug().Msg("Retrieving PDH interface instances")
	pdhInstances, err := pdh.GetInstancesList("Network Interface", true) // debug enabled
	if err != nil {
		logger.Debug().Err(err).Msg("Failed to get PDH instances")
		return nil, fmt.Errorf("failed to get PDH Network Interface instances: %v", err)
	}
	logger.Debug().Int("instance_count", len(pdhInstances)).Msg("PDH Interface instances found")
	for _, inst := range pdhInstances {
		logger.Debug().Str("instance", inst).Msg("PDH instance")
	}

	// 6. Build final mapping
	logger.Debug().Msg("Building final interface mapping")
	interfaces := make(map[string]interfaceInfo)

	for _, iface := range netInterfaces {
		logger.Debug().Str("interface", iface.Name).Msg("Processing interface")

		// Check for corresponding adapter
		adapterInfo, exists := connectionToAdapter[iface.Name]
		if !exists {
			logger.Debug().Str("interface", iface.Name).Msg("No matching WMI adapter found, skipping")
			continue
		}
		if !adapterInfo.enabled {
			logger.Debug().Str("adapter", iface.Name).Msg("Adapter is disabled, skipping")
			continue
		}

		// Get IP addresses
		addrs, err := iface.Addrs()
		if err != nil {
			logger.Debug().Err(err).Msg("Error getting addresses")
			continue
		}

		var ipv4, ipv6 string
		var otherIPs []string

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip4 := ip.To4(); ip4 != nil {
				if ipv4 == "" {
					ipv4 = ip4.String()
					logger.Debug().Str("ipv4", ipv4).Msg("Primary IPv4")
				} else {
					otherIPs = append(otherIPs, ip4.String())
					logger.Debug().Str("ipv4", ip4.String()).Msg("Additional IPv4")
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ipv6 == "" {
					ipv6 = ip6.String()
					logger.Debug().Str("ipv6", ipv6).Msg("Primary IPv6")
				} else {
					otherIPs = append(otherIPs, ip6.String())
					logger.Debug().Str("ipv6", ip6.String()).Msg("Additional IPv6")
				}
			}
		}

		if ipv4 != "" || ipv6 != "" {
			// Find matching PDH instance using normalized names
			var pdhName string
			for _, inst := range pdhInstances {
				if strings.Contains(normalizeAdapterName(inst), normalizeAdapterName(adapterInfo.name)) {
					pdhName = inst
					logger.Debug().Str("pdh_name", pdhName).Msg("Found matching PDH instance")
					break
				}
			}

			if pdhName != "" {
				interfaces[pdhName] = interfaceInfo{
					adapterName:    adapterInfo.name,
					connectionName: iface.Name,
					ipv4:           ipv4,
					ipv6:           ipv6,
					otherIPs:       otherIPs,
					enabled:        true,
				}
				logger.Debug().
					Str("pdh", pdhName).
					Str("adapter", adapterInfo.name).
					Str("connection", iface.Name).
					Str("ipv4", ipv4).
					Str("ipv6", ipv6).
					Strs("other_ips", otherIPs).
					Msg("Successfully mapped interface")
			} else {
				logger.Debug().Str("adapter", adapterInfo.name).Msg("No matching PDH instance found for adapter")
			}
		} else {
			logger.Debug().Str("interface", iface.Name).Msg("No IP addresses found for interface")
		}
	}

	logger.Debug().Msg("Final Result")
	logger.Debug().Int("interface_count", len(interfaces)).Msg("Found valid interfaces")

	return interfaces, nil
}

func newNetworkCollector(config map[string]interface{}, baseLogger *logger.Logger) (osNetworkCollector, error) {
	// Initialize PDH logger
	pdh.InitializePDHLogger(baseLogger)

	// Create module logger for host probes
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.host")

	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	moduleLogger.Debug().Msg("Initializing network collector")

	interfaces, err := getNetworkInterfaces(moduleLogger)
	if err != nil {
		query.Close()
		return nil, fmt.Errorf("failed to get network interfaces: %v", err)
	}

	collector := &windowsNetworkCollector{
		query:      query,
		paths:      make(map[string]pathInfo),
		interfaces: interfaces,
		logger:     moduleLogger,
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
	w.logger.Debug().Msg("Initializing Network probe counters")

	for metricName, def := range networkCounterPaths {
		if def.instance == "*" {
			for pdhName, interfaceInfo := range w.interfaces {
				path := pdh.BuildCounterPath(def.path, pdhName)
				w.paths[fmt.Sprintf("%s|%s", metricName, pdhName)] = pathInfo{
					path:     path,
					instance: pdhName,
				}

				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (instance %s): %v", metricName, pdhName, err)
				}
				w.logger.Debug().
					Str("metric", metricName).
					Str("adapter", interfaceInfo.adapterName).
					Str("connection", interfaceInfo.connectionName).
					Str("ipv4", interfaceInfo.ipv4).
					Str("ipv6", interfaceInfo.ipv6).
					Str("other_ips", strings.Join(interfaceInfo.otherIPs, ", ")).
					Msg("Added counter")
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
			w.logger.Debug().Str("metric", name).Err(err).Msg("Error getting counter value")
			continue
		}

		interfaceInfo := w.interfaces[pathInfo.instance]

		metricTags := append([]tags.Tag{}, baseTags...)
		metricTags = append(metricTags, []tags.Tag{
			{
				Key:     "adapter",
				Value:   interfaceInfo.adapterName,
				Private: false,
			},
			{
				Key:     "interface",
				Value:   pathInfo.instance, // Use PDH instance name (e.g., "Network_3")
				Private: false,
			},
			{
				Key:     "connection_name",
				Value:   interfaceInfo.connectionName, // Keep original connection name as additional info
				Private: false,
			},
		}...)

		if interfaceInfo.ipv4 != "" {
			metricTags = append(metricTags, tags.Tag{
				Key:     "ip",
				Value:   interfaceInfo.ipv4,
				Private: false,
			})
		} else if interfaceInfo.ipv6 != "" {
			metricTags = append(metricTags, tags.Tag{
				Key:     "ip",
				Value:   interfaceInfo.ipv6,
				Private: false,
			})
		}

		// Add other IPs with index
		for i, ip := range interfaceInfo.otherIPs {
			metricTags = append(metricTags, tags.Tag{
				Key:     fmt.Sprintf("ip_%d", i+1),
				Value:   ip,
				Private: false,
			})
		}

		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      strings.Split(name, "|")[0],
			Timestamp: timestamp,
			Value:     float32(value),
			Tags:      metricTags,
		})
	}

	return dataPoints, nil
}

func (w *windowsNetworkCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}
