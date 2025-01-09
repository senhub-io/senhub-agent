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
	adapterName    string   // Nom physique de la carte (depuis WMI)
	connectionName string   // Nom de connexion (NetConnectionID de WMI)
	ipv4           string   // Adresse IPv4 principale
	ipv6           string   // Adresse IPv6 principale
	otherIPs       []string // Autres adresses IP
	enabled        bool     // État de la connexion
}

type windowsNetworkCollector struct {
	query       *pdh.Query
	paths       map[string]pathInfo
	interfaces  map[string]interfaceInfo // clé = nom PDH
	mu          sync.Mutex
	initialized bool
}

func getNetworkInterfaces() (map[string]interfaceInfo, error) {
	// 1. Récupérer les adaptateurs physiques via WMI
	var adapters []Win32_NetworkAdapter
	query := "SELECT Name, NetConnectionID, PhysicalAdapter, NetEnabled FROM Win32_NetworkAdapter WHERE PhysicalAdapter=True"
	if err := wmi.Query(query, &adapters); err != nil {
		return nil, fmt.Errorf("failed to get network adapters from WMI: %v", err)
	}

	fmt.Printf("\nFound Network Adapters (WMI):\n")
	for _, adapter := range adapters {
		fmt.Printf("  Adapter: %s\n  Connection Name: %s\n\n",
			adapter.Name, adapter.NetConnectionID)
	}

	// 2. Créer un mapping NetConnectionID -> Name
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
		}
	}

	// 3. Récupérer les interfaces système pour les adresses IP
	netInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get system network interfaces: %v", err)
	}

	// 4. Récupérer les noms d'instance PDH
	pdhInstances, err := pdh.GetInstancesList("Network Interface", false)
	if err != nil {
		return nil, fmt.Errorf("failed to get PDH Network Interface instances: %v", err)
	}

	fmt.Printf("\nPDH Interface instances:\n")
	for _, inst := range pdhInstances {
		fmt.Printf("  %s\n", inst)
	}

	// 5. Construire le mapping final
	interfaces := make(map[string]interfaceInfo)

	for _, iface := range netInterfaces {
		// Chercher l'adaptateur correspondant et vérifier qu'il est actif
		adapterInfo, exists := connectionToAdapter[iface.Name]
		if !exists || !adapterInfo.enabled {
			continue // Pas un adaptateur physique, non trouvé ou non actif
		}
		adapterName := adapterInfo.name

		// Récupérer les adresses IP
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Printf("Error getting addresses for interface %s: %v\n", iface.Name, err)
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
				} else {
					otherIPs = append(otherIPs, ip4.String())
				}
			} else if ip6 := ip.To16(); ip6 != nil {
				if ipv6 == "" {
					ipv6 = ip6.String()
				} else {
					otherIPs = append(otherIPs, ip6.String())
				}
			}
		}

		if ipv4 != "" || ipv6 != "" {
			// Chercher le nom PDH correspondant
			var pdhName string
			for _, inst := range pdhInstances {
				if strings.Contains(inst, adapterName) {
					pdhName = inst
					break
				}
			}

			if pdhName != "" {
				interfaces[pdhName] = interfaceInfo{
					adapterName:    adapterName,
					connectionName: iface.Name,
					ipv4:           ipv4,
					ipv6:           ipv6,
					otherIPs:       otherIPs,
					enabled:        true,
				}
				fmt.Printf("\nMapped interface:\n  PDH: %s\n  Adapter: %s\n  Connection: %s\n  IPv4: %s\n  IPv6: %s\n  Other IPs: %s\n",
					pdhName, adapterName, iface.Name, ipv4, ipv6, strings.Join(otherIPs, ", "))
			}
		}
	}

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

		// Ajouter les autres IPs avec index
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
