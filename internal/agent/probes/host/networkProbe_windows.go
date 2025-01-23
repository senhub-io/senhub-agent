//senhub-agent/internal/agent/probes/host/networkProbe_windows.go
//go:build windows

package host

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

func getNetworkInterfaces() (map[string]interfaceInfo, error) {
	fmt.Printf("\n=== Starting Network Interfaces Detection ===\n")

	// 1. WMI Query
	var adapters []Win32_NetworkAdapter
	query := "SELECT Name, NetConnectionID, PhysicalAdapter, NetEnabled FROM Win32_NetworkAdapter WHERE PhysicalAdapter=True"
	fmt.Printf("\nExecuting WMI query: %s\n", query)

	if err := wmi.Query(query, &adapters); err != nil {
		fmt.Printf("WMI query failed: %v\n", err)
		return nil, fmt.Errorf("failed to get network adapters from WMI: %v", err)
	}
	fmt.Printf("Found %d physical adapters from WMI\n", len(adapters))

	// 2. Debug WMI results
	fmt.Printf("\nNetwork Adapters (WMI):\n")
	for _, adapter := range adapters {
		fmt.Printf("  - Name: %s\n    Connection: %s\n    Enabled: %v\n",
			adapter.Name, adapter.NetConnectionID, adapter.NetEnabled)
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
			fmt.Printf("\nRegistered adapter mapping: %s -> %s (enabled: %v)\n",
				adapter.NetConnectionID, adapter.Name, adapter.NetEnabled)
		}
	}

	// 4. Get system interfaces
	fmt.Printf("\nRetrieving system network interfaces...\n")
	netInterfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("Failed to get system interfaces: %v\n", err)
		return nil, fmt.Errorf("failed to get system network interfaces: %v", err)
	}
	fmt.Printf("Found %d system interfaces\n", len(netInterfaces))

	// 5. Get PDH instances
	fmt.Printf("\nRetrieving PDH interface instances...\n")
	pdhInstances, err := pdh.GetInstancesList("Network Interface", true) // debug enabled
	if err != nil {
		fmt.Printf("Failed to get PDH instances: %v\n", err)
		return nil, fmt.Errorf("failed to get PDH Network Interface instances: %v", err)
	}
	fmt.Printf("\nPDH Interface instances (%d found):\n", len(pdhInstances))
	for _, inst := range pdhInstances {
		fmt.Printf("  - %s\n", inst)
	}

	// 6. Build final mapping
	fmt.Printf("\n=== Building final interface mapping ===\n")
	interfaces := make(map[string]interfaceInfo)

	for _, iface := range netInterfaces {
		fmt.Printf("\nProcessing interface: %s\n", iface.Name)

		// Check for corresponding adapter
		adapterInfo, exists := connectionToAdapter[iface.Name]
		if !exists {
			fmt.Printf("  No matching WMI adapter found for %s, skipping\n", iface.Name)
			continue
		}
		if !adapterInfo.enabled {
			fmt.Printf("  Adapter %s is disabled, skipping\n", iface.Name)
			continue
		}

		// Get IP addresses
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Printf("  Error getting addresses: %v\n", err)
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
					fmt.Printf("  Primary IPv4: %s\n", ipv4)
				} else {
					otherIPs = append(otherIPs, ip4.String())
					fmt.Printf("  Additional IPv4: %s\n", ip4.String())
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ipv6 == "" {
					ipv6 = ip6.String()
					fmt.Printf("  Primary IPv6: %s\n", ipv6)
				} else {
					otherIPs = append(otherIPs, ip6.String())
					fmt.Printf("  Additional IPv6: %s\n", ip6.String())
				}
			}
		}

		if ipv4 != "" || ipv6 != "" {
			// Find matching PDH instance using normalized names
			var pdhName string
			for _, inst := range pdhInstances {
				if strings.Contains(normalizeAdapterName(inst), normalizeAdapterName(adapterInfo.name)) {
					pdhName = inst
					fmt.Printf("  Found matching PDH instance: %s\n", pdhName)
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
				fmt.Printf("\nSuccessfully mapped interface:\n  PDH: %s\n  Adapter: %s\n  Connection: %s\n  IPv4: %s\n  IPv6: %s\n  Other IPs: %v\n",
					pdhName, adapterInfo.name, iface.Name, ipv4, ipv6, otherIPs)
			} else {
				fmt.Printf("  No matching PDH instance found for adapter %s\n", adapterInfo.name)
			}
		} else {
			fmt.Printf("  No IP addresses found for interface %s\n", iface.Name)
		}
	}

	fmt.Printf("\n=== Final Result ===\n")
	fmt.Printf("Found %d valid interfaces\n", len(interfaces))

	return interfaces, nil
}

func newNetworkCollector(config map[string]interface{}, logger *logger.Logger) (osNetworkCollector, error) {
	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	fmt.Printf("Initializing network collector\n")

	interfaces, err := getNetworkInterfaces()
	if err != nil {
		query.Close()
		return nil, fmt.Errorf("failed to get network interfaces: %v", err)
	}

	collector := &windowsNetworkCollector{
		query:      query,
		paths:      make(map[string]pathInfo),
		interfaces: interfaces,
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
	fmt.Printf("Initializing Network probe counters\n")

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
				fmt.Printf("Added counter %s for adapter '%s' (connection: %s) IPv4: %s, IPv6: %s, Other IPs: %s\n",
					metricName, interfaceInfo.adapterName, interfaceInfo.connectionName,
					interfaceInfo.ipv4, interfaceInfo.ipv6, strings.Join(interfaceInfo.otherIPs, ", "))
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
				Value:   interfaceInfo.connectionName,
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
