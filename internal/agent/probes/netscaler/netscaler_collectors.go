// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// collectSystemStats gathers system-level metrics
func (p *netscalerProbe) collectSystemStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// system is a singleton resource, use FindStat instead of FindAllStats
	sys, err := p.client.FindStat("system", "")
	if err != nil {
		return nil, err
	}

	if sys == nil {
		return nil, fmt.Errorf("no system stats returned")
	}

	var datapoints []datapoint.DataPoint

	// CPU metrics
	// IMPORTANT: cpuusagepcnt is OBSOLETE and returns 4294967295 (bug in Netscaler API)
	// Official Citrix recommendation: use pktcpuusagepcnt (packet engine CPU) instead
	// See: https://github.com/citrix/citrix-adc-metrics-exporter/issues/44
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.cpu.usage.percent",
		Value:     float32(getFloat(sys, "pktcpuusagepcnt")), // Packet engine CPU (the real load balancer work)
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.cpu.mgmt.usage.percent",
		Value:     float32(getFloat(sys, "mgmtcpuusagepcnt")), // Management plane CPU (config, API, web UI)
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Memory metrics
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.memory.usage.percent",
		Value:     float32(getFloat(sys, "memusagepcnt")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Network throughput
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.rx.mbits_per_sec",
		Value:     float32(getFloat(sys, "rxmbitsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.tx.mbits_per_sec",
		Value:     float32(getFloat(sys, "txmbitsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// HTTP requests/responses
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.http.requests.rate",
		Value:     float32(getFloat(sys, "httprequestsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.http.responses.rate",
		Value:     float32(getFloat(sys, "httpresponsesrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// TCP connections
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.tcp.client.connections.current",
		Value:     float32(getFloat(sys, "tcpcurclientconn")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.tcp.server.connections.current",
		Value:     float32(getFloat(sys, "tcpcurserverconn")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Packets per second (PPS) - RX
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.rx.packets_per_sec",
		Value:     float32(getFloat(sys, "rxpacketsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Packets per second (PPS) - TX
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.tx.packets_per_sec",
		Value:     float32(getFloat(sys, "txpacketsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Total packets received (counter for rate calculation)
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.rx.packets.total",
		Value:     float32(getFloat(sys, "totalpktsrecvd")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Total packets sent (counter for rate calculation)
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.system.network.tx.packets.total",
		Value:     float32(getFloat(sys, "totalpktssent")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Disk metrics (disk0 = /flash, disk1 = /var)
	// Disk 0 (/flash partition)
	disk0Tags := append(baseTags, tags.Tag{Key: "partition", Value: "/flash"})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.percent_used",
		Value:     float32(getFloat(sys, "disk0perusage")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.used_kb",
		Value:     float32(getFloat(sys, "disk0used")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.available_kb",
		Value:     float32(getFloat(sys, "disk0avail")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})

	// Disk 1 (/var partition)
	disk1Tags := append(baseTags, tags.Tag{Key: "partition", Value: "/var"})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.percent_used",
		Value:     float32(getFloat(sys, "disk1perusage")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.used_kb",
		Value:     float32(getFloat(sys, "disk1used")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.available_kb",
		Value:     float32(getFloat(sys, "disk1avail")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectNSStats gathers Netscaler-specific global metrics
func (p *netscalerProbe) collectNSStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// ns is a singleton resource, use FindStat instead of FindAllStats
	ns, err := p.client.FindStat("ns", "")
	if err != nil {
		return nil, err
	}

	if ns == nil {
		return nil, fmt.Errorf("no NS stats returned")
	}

	var datapoints []datapoint.DataPoint

	// Total throughput
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.ns.throughput.total.mbits_per_sec",
		Value:     float32(getFloat(ns, "totalthroughputrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// HTTP throughput
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.ns.http.throughput.mbits_per_sec",
		Value:     float32(getFloat(ns, "httpthroughputrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectLBVServerStats gathers load balancer virtual server metrics
func (p *netscalerProbe) collectLBVServerStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	stats, err := p.client.FindAllStats("lbvserver")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, vserver := range stats {
		vserverName := getString(vserver, "name")
		if vserverName == "" {
			continue
		}

		// Create tags with vserver name
		vserverTags := append(baseTags, tags.Tag{Key: "vserver", Value: vserverName})

		// Enrich with config data from cache
		if config := p.cache.getVServerConfig(vserverName); config != nil {
			if vserverType := getString(config, "servicetype"); vserverType != "" {
				vserverTags = append(vserverTags, tags.Tag{Key: "vserver_type", Value: vserverType})
			}
			if port := getString(config, "port"); port != "" {
				vserverTags = append(vserverTags, tags.Tag{Key: "vserver_port", Value: port})
			}
			if ip := getString(config, "ipv46"); ip != "" {
				vserverTags = append(vserverTags, tags.Tag{Key: "vserver_ip", Value: ip})
			}
		}

		// Add bound servicegroups
		if servicegroups := p.cache.getServiceGroupsForVServer(vserverName); len(servicegroups) > 0 {
			// If multiple SGs, use the first one (most common case is 1:1)
			vserverTags = append(vserverTags, tags.Tag{Key: "servicegroup", Value: servicegroups[0]})
		}

		// State - use official Citrix ADC NITRO API numeric codes
		// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
		// UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OFS=4, TROFS=5, TROFS_DOWN=8
		state := getString(vserver, "state")
		stateValue := parseNetscalerState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.state",
			Value:     stateValue,
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Requests rate
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.requests.rate",
			Value:     float32(getFloat(vserver, "requestsrate")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Connections
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.connections.current",
			Value:     float32(getFloat(vserver, "curclntconnections")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Throughput
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.throughput.rx.bytes_per_sec",
			Value:     float32(getFloat(vserver, "requestbytesrate")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.throughput.tx.bytes_per_sec",
			Value:     float32(getFloat(vserver, "responsebytesrate")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Spillover count (saturation indicator)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.spillovers.total",
			Value:     float32(getFloat(vserver, "totspillovers")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Established connections (vs current for capacity planning)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.connections.established",
			Value:     float32(getFloat(vserver, "establishedconn")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})

		// Total hits (request distribution)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.lbvserver.hits.total",
			Value:     float32(getFloat(vserver, "tothits")),
			Tags:      vserverTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectServiceStats gathers backend service metrics
func (p *netscalerProbe) collectServiceStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	stats, err := p.client.FindAllStats("service")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, service := range stats {
		serviceName := getString(service, "name")
		if serviceName == "" {
			continue
		}

		// Create tags with service name
		serviceTags := append(baseTags, tags.Tag{Key: "service", Value: serviceName})

		// Enrich with config data from cache
		if config := p.cache.getServiceConfig(serviceName); config != nil {
			if backendHost := getString(config, "ipaddress"); backendHost != "" {
				serviceTags = append(serviceTags, tags.Tag{Key: "backend_host", Value: backendHost})
			}
			if backendPort := getString(config, "port"); backendPort != "" {
				serviceTags = append(serviceTags, tags.Tag{Key: "backend_port", Value: backendPort})
			}
		}

		// State - use official Citrix ADC NITRO API numeric codes
		// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
		// UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OFS=4, TROFS=5, TROFS_DOWN=8
		state := getString(service, "state")
		stateValue := parseNetscalerState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.service.state",
			Value:     stateValue,
			Tags:      serviceTags,
			Timestamp: timestamp,
		})

		// Throughput
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.service.throughput.bytes_per_sec",
			Value:     float32(getFloat(service, "throughputrate")),
			Tags:      serviceTags,
			Timestamp: timestamp,
		})

		// Active transactions
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.service.transactions.active",
			Value:     float32(getFloat(service, "activetransactions")),
			Tags:      serviceTags,
			Timestamp: timestamp,
		})

		// Surge queue length (backend saturation indicator)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.service.surge_queue_length",
			Value:     float32(getFloat(service, "surgecount")),
			Tags:      serviceTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectSSLStats gathers SSL/TLS metrics
func (p *netscalerProbe) collectSSLStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// ssl is a singleton resource, use FindStat instead of FindAllStats
	ssl, err := p.client.FindStat("ssl", "")
	if err != nil {
		return nil, err
	}

	if ssl == nil {
		return nil, fmt.Errorf("no SSL stats returned")
	}

	var datapoints []datapoint.DataPoint

	// SSL transactions rate
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.ssl.transactions.rate",
		Value:     float32(getFloat(ssl, "ssltransactionsrate")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// SSL sessions
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.ssl.sessions.total",
		Value:     float32(getFloat(ssl, "sslsessiontot")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectServiceGroupStats gathers service group metrics
func (p *netscalerProbe) collectServiceGroupStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	stats, err := p.client.FindAllStats("servicegroup")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, sg := range stats {
		sgName := getString(sg, "servicegroupname")
		if sgName == "" {
			continue
		}

		// Create tags with service group name
		sgTags := append(baseTags, tags.Tag{Key: "servicegroup", Value: sgName})

		// Add bound vServers
		if vservers := p.cache.getVServersForServiceGroup(sgName); len(vservers) > 0 {
			// If multiple vServers, use the first one (most common case is 1:1)
			sgTags = append(sgTags, tags.Tag{Key: "vserver", Value: vservers[0]})
		}

		// State - use official Citrix ADC NITRO API numeric codes
		// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
		// UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OFS=4, TROFS=5, TROFS_DOWN=8
		state := getString(sg, "state")
		stateValue := parseNetscalerState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.servicegroup.state",
			Value:     stateValue,
			Tags:      sgTags,
			Timestamp: timestamp,
		})

		// Requests rate
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.servicegroup.requests.rate",
			Value:     float32(getFloat(sg, "requestsrate")),
			Tags:      sgTags,
			Timestamp: timestamp,
		})

		// Active members
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.servicegroup.members.active",
			Value:     float32(getFloat(sg, "activemembers")),
			Tags:      sgTags,
			Timestamp: timestamp,
		})

		// Inactive members
		totalMembers := getFloat(sg, "totalmembers")
		activeMembers := getFloat(sg, "activemembers")
		inactiveMembers := totalMembers - activeMembers
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.servicegroup.members.inactive",
			Value:     float32(inactiveMembers),
			Tags:      sgTags,
			Timestamp: timestamp,
		})

		// Surge queue length (backend saturation indicator)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.servicegroup.surge_queue_length",
			Value:     float32(getFloat(sg, "surgecount")),
			Tags:      sgTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectSSLCertificateStats gathers SSL certificate expiration metrics
func (p *netscalerProbe) collectSSLCertificateStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// Get all SSL certificates from cache
	certkeys := p.cache.getAllSSLCertKeys()
	if len(certkeys) == 0 {
		p.logger.Debug().Msg("No SSL certificates found in cache")
		return nil, nil
	}

	var datapoints []datapoint.DataPoint

	for certname, cert := range certkeys {
		// Create tags with certificate name
		certTags := append(baseTags, tags.Tag{Key: "certname", Value: certname})

		// Days to expiration
		daysToExpiration := getFloat(cert, "daystoexpiration")
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ssl.certificate.days_to_expiration",
			Value:     float32(daysToExpiration),
			Tags:      certTags,
			Timestamp: timestamp,
		})

		// Certificate status (1 = valid, 0 = expired or about to expire)
		certStatus := float32(1)
		if daysToExpiration < 0 {
			certStatus = 0 // Expired
		}
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ssl.certificate.status",
			Value:     certStatus,
			Tags:      certTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectHAStats gathers High Availability (HA) metrics
func (p *netscalerProbe) collectHAStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	p.logger.Debug().
		Int("probe_node_id", p.nodeID).
		Str("probe_hostname", p.hostname).
		Msg("Starting HA stats collection")

	// Fetch HA configuration to get all nodes in the cluster
	// /config/hanode returns both nodes with their IDs and hostnames
	p.logger.Debug().Msg("Calling FindAllResources('hanode')")
	haNodeConfigs, err := p.client.FindAllResources("hanode")
	if err != nil {
		p.logger.Warn().
			Err(err).
			Msg("Failed to fetch HA node config - this is expected if HA is not configured")
		return nil, fmt.Errorf("failed to fetch HA node config: %w", err)
	}

	p.logger.Debug().
		Int("config_nodes_count", len(haNodeConfigs)).
		Int("probe_node_id", p.nodeID).
		Interface("hanode_configs", haNodeConfigs).
		Msg("Fetched HA node configurations")

	// Fetch HA stats (this returns stats for the local node only)
	// /stat/hanode returns stats for the connected node
	stats, err := p.client.FindAllStats("hanode")
	if err != nil {
		return nil, err
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no HA stats returned")
	}

	// Stats are for the local node only
	localNodeStats := stats[0]

	var datapoints []datapoint.DataPoint

	// Iterate through ALL HA nodes from config (typically 2: node 0 and node 1)
	for _, nodeConfig := range haNodeConfigs {
		// Extract node identification from config
		// nodeConfig has: id (0 or 1), ipaddress, name (hostname)
		nodeID := int(getFloat(nodeConfig, "id"))
		nodeIDStr := fmt.Sprintf("%d", nodeID)
		nodeIP := getString(nodeConfig, "ipaddress")
		nodeHostname := getString(nodeConfig, "name")

		// Determine if this is the local node (the one we're connected to)
		isLocalNode := (p.nodeID == nodeID)

		p.logger.Debug().
			Int("node_id", nodeID).
			Str("node_ip", nodeIP).
			Str("node_hostname", nodeHostname).
			Bool("is_local", isLocalNode).
			Msg("Processing HA node")

		// Create node-specific tags (deep copy to avoid modifying baseTags)
		nodeTags := append([]tags.Tag{}, baseTags...)
		nodeTags = append(nodeTags, tags.Tag{Key: "ha_node_id", Value: nodeIDStr})
		if nodeHostname != "" {
			nodeTags = append(nodeTags, tags.Tag{Key: "ha_node_name", Value: nodeHostname})
		}
		if nodeIP != "" {
			nodeTags = append(nodeTags, tags.Tag{Key: "ha_node_ip", Value: nodeIP})
		}
		if p.hostname != "" {
			nodeTags = append(nodeTags, tags.Tag{Key: "connected_to", Value: p.hostname})
		}
		nodeTags = append(nodeTags, tags.Tag{Key: "is_local_node", Value: fmt.Sprintf("%t", isLocalNode)})

		// For the local node, use real stats from /stat/hanode
		// For the remote node, use config data (state from hanode config)
		var masterState string
		var hacurstate string
		var syncFailures float64

		if isLocalNode {
			// Use actual stats for local node
			masterState = getString(localNodeStats, "hacurmasterstate")
			hacurstate = getString(localNodeStats, "hacurstate")
			syncFailures = getFloat(localNodeStats, "haerrsyncfailure")
		} else {
			// For remote node, use config data
			masterState = getString(nodeConfig, "masterstate")
			hacurstate = getString(nodeConfig, "state")
			// Sync failures not available for remote node
			syncFailures = 0
		}

		// HA State using hacurmasterstate (PRIMARY/SECONDARY from NITRO API)
		// Normalize to: 2=PRIMARY, 1=SECONDARY, 0=UNKNOWN
		// Source: https://developer-docs.netscaler.com/en-us/adc-nitro-api/current-release/statistics/ha/hanode/
		// masterState already defined above based on isLocalNode
		stateValue := float32(0) // UNKNOWN
		if masterState == "Primary" || masterState == "PRIMARY" {
			stateValue = 2
		} else if masterState == "Secondary" || masterState == "SECONDARY" {
			stateValue = 1
		}

		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ha.state",
			Value:     stateValue,
			Tags:      nodeTags,
			Timestamp: timestamp,
		})

		// HA node operational state (UP/DOWN)
		// hacurstate: UP, DOWN, DISABLED, etc.
		// hacurstate already defined above based on isLocalNode
		nodeState := float32(0) // DOWN
		if hacurstate == "UP" {
			nodeState = 1
		}

		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ha.node.state",
			Value:     nodeState,
			Tags:      nodeTags,
			Timestamp: timestamp,
		})

		// HA Sync status (1 = success, 0 = failed)
		// Use correct field name from NITRO API: haerrsyncfailure (not hasyncfailures)
		// syncFailures already defined above based on isLocalNode
		syncStatus := float32(1)
		if syncFailures > 0 {
			syncStatus = 0
		}

		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ha.sync_status",
			Value:     syncStatus,
			Tags:      nodeTags,
			Timestamp: timestamp,
		})

		// HA Sync failures count
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.ha.sync_failures",
			Value:     float32(syncFailures),
			Tags:      nodeTags,
			Timestamp: timestamp,
		})
	}

	p.logger.Debug().Int("ha_nodes", len(haNodeConfigs)).Msg("Collected HA stats for all nodes")

	return datapoints, nil
}

// collectDiskStats gathers disk usage metrics
func (p *netscalerProbe) collectDiskStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// Disk stats are part of system resource (disk0* = /flash, disk1* = /var)
	sys, err := p.client.FindStat("system", "")
	if err != nil {
		return nil, err
	}

	if sys == nil {
		return nil, fmt.Errorf("no system stats returned")
	}

	var datapoints []datapoint.DataPoint

	// Disk 0 (/flash partition)
	disk0Tags := append(baseTags, tags.Tag{Key: "partition", Value: "/flash"})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.percent_used",
		Value:     float32(getFloat(sys, "disk0perusage")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.used_kb",
		Value:     float32(getFloat(sys, "disk0used")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.available_kb",
		Value:     float32(getFloat(sys, "disk0avail")),
		Tags:      disk0Tags,
		Timestamp: timestamp,
	})

	// Disk 1 (/var partition)
	disk1Tags := append(baseTags, tags.Tag{Key: "partition", Value: "/var"})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.percent_used",
		Value:     float32(getFloat(sys, "disk1perusage")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.used_kb",
		Value:     float32(getFloat(sys, "disk1used")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.disk.available_kb",
		Value:     float32(getFloat(sys, "disk1avail")),
		Tags:      disk1Tags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectInterfaceStats gathers network interface metrics
func (p *netscalerProbe) collectInterfaceStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// Interface returns stats for all network interfaces (note: capital I)
	stats, err := p.client.FindAllStats("Interface")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, iface := range stats {
		interfaceName := getString(iface, "id")
		if interfaceName == "" {
			continue
		}

		// Create tags with interface name (keep original name with /)
		ifaceTags := append(baseTags, tags.Tag{Key: "interface", Value: interfaceName})

		// Interface state - binary ENABLED/DISABLED state
		// Source: Citrix ADC NITRO API - interface state field
		// ENABLED/UP=1, DISABLED/DOWN=0
		state := getString(iface, "state")
		stateValue := parseNetscalerBinaryState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.state",
			Value:     stateValue,
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// RX bytes total
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.rx.bytes.total",
			Value:     float32(getFloat(iface, "totrxbytes")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// TX bytes total
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.tx.bytes.total",
			Value:     float32(getFloat(iface, "tottxbytes")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// RX bytes rate (Mbps)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.rx.mbits_per_sec",
			Value:     float32(getFloat(iface, "rxbytesrate")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// TX bytes rate (Mbps)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.tx.mbits_per_sec",
			Value:     float32(getFloat(iface, "txbytesrate")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// RX errors total
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.rx.errors.total",
			Value:     float32(getFloat(iface, "errifrcvnobuf")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// TX errors total
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.tx.errors.total",
			Value:     float32(getFloat(iface, "errxmitbadpacket")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// RX drops total
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.rx.drops.total",
			Value:     float32(getFloat(iface, "errdroppedtxpkts")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// TX drops total (using same field as it's bidirectional)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.interface.tx.drops.total",
			Value:     float32(getFloat(iface, "errdroppedtxpkts")),
			Tags:      ifaceTags,
			Timestamp: timestamp,
		})

		// Link speed (if available)
		linkSpeed := getFloat(iface, "linkspeed")
		if linkSpeed > 0 {
			datapoints = append(datapoints, datapoint.DataPoint{
				Name:      "netscaler.interface.link_speed_mbps",
				Value:     float32(linkSpeed),
				Tags:      ifaceTags,
				Timestamp: timestamp,
			})
		}
	}

	return datapoints, nil
}

// collectContentSwitchingStats gathers Content Switching vServer metrics
func (p *netscalerProbe) collectContentSwitchingStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// csvserver returns Content Switching virtual server stats
	stats, err := p.client.FindAllStats("csvserver")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, csvserver := range stats {
		csvserverName := getString(csvserver, "name")
		if csvserverName == "" {
			continue
		}

		// Create tags with csvserver name
		csvserverTags := append(baseTags, tags.Tag{Key: "csvserver", Value: csvserverName})

		// State - use official Citrix ADC NITRO API numeric codes
		// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
		// UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OFS=4, TROFS=5, TROFS_DOWN=8
		state := getString(csvserver, "state")
		stateValue := parseNetscalerState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.vserver.state",
			Value:     stateValue,
			Tags:      csvserverTags,
			Timestamp: timestamp,
		})

		// Total hits
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.vserver.hits.total",
			Value:     float32(getFloat(csvserver, "tothits")),
			Tags:      csvserverTags,
			Timestamp: timestamp,
		})

		// Requests rate
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.vserver.requests.rate",
			Value:     float32(getFloat(csvserver, "requestsrate")),
			Tags:      csvserverTags,
			Timestamp: timestamp,
		})

		// Current connections
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.vserver.connections.current",
			Value:     float32(getFloat(csvserver, "curclntconnections")),
			Tags:      csvserverTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectContentSwitchingPolicyStats gathers Content Switching policy metrics
func (p *netscalerProbe) collectContentSwitchingPolicyStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// cspolicy returns Content Switching policy stats
	stats, err := p.client.FindAllStats("cspolicy")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, policy := range stats {
		policyName := getString(policy, "policyname")
		if policyName == "" {
			continue
		}

		// Create tags with policy name
		policyTags := append(baseTags, tags.Tag{Key: "cspolicy", Value: policyName})

		// Policy hits
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.policy.hits.total",
			Value:     float32(getFloat(policy, "policyht")),
			Tags:      policyTags,
			Timestamp: timestamp,
		})

		// Undefine hits (rules not matched)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.cs.policy.undefine_hits.total",
			Value:     float32(getFloat(policy, "undefht")),
			Tags:      policyTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectGSLBVServerStats gathers GSLB virtual server metrics
func (p *netscalerProbe) collectGSLBVServerStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// gslbvserver returns GSLB virtual server stats
	stats, err := p.client.FindAllStats("gslbvserver")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, gslbvserver := range stats {
		vserverName := getString(gslbvserver, "name")
		if vserverName == "" {
			continue
		}

		// Create tags with GSLB vserver name
		gslbTags := append(baseTags, tags.Tag{Key: "gslbvserver", Value: vserverName})

		// State - use official Citrix ADC NITRO API numeric codes
		// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
		// UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OFS=4, TROFS=5, TROFS_DOWN=8
		state := getString(gslbvserver, "state")
		stateValue := parseNetscalerState(state)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.vserver.state",
			Value:     stateValue,
			Tags:      gslbTags,
			Timestamp: timestamp,
		})

		// Total hits
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.vserver.hits.total",
			Value:     float32(getFloat(gslbvserver, "tothits")),
			Tags:      gslbTags,
			Timestamp: timestamp,
		})

		// Requests rate
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.vserver.requests.rate",
			Value:     float32(getFloat(gslbvserver, "requestsrate")),
			Tags:      gslbTags,
			Timestamp: timestamp,
		})

		// Persistence records
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.vserver.persistence.records",
			Value:     float32(getFloat(gslbvserver, "totpersistentrecords")),
			Tags:      gslbTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectGSLBSiteStats gathers GSLB site metrics
func (p *netscalerProbe) collectGSLBSiteStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// gslbsite returns GSLB site stats
	stats, err := p.client.FindAllStats("gslbsite")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, site := range stats {
		siteName := getString(site, "sitename")
		if siteName == "" {
			continue
		}

		// Create tags with site name
		siteTags := append(baseTags, tags.Tag{Key: "gslbsite", Value: siteName})

		// Site state (UP=1, DOWN=0)
		state := getString(site, "sitestate")
		stateValue := float32(0)
		if state == "UP" || state == "ACTIVE" {
			stateValue = 1
		}
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.site.state",
			Value:     stateValue,
			Tags:      siteTags,
			Timestamp: timestamp,
		})

		// Network round-trip time (latency in microseconds)
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.site.network_rtt_microseconds",
			Value:     float32(getFloat(site, "nwrtt")),
			Tags:      siteTags,
			Timestamp: timestamp,
		})

		// Current connections
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.site.connections.current",
			Value:     float32(getFloat(site, "curclntconnections")),
			Tags:      siteTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectGSLBServiceStats gathers GSLB service metrics
func (p *netscalerProbe) collectGSLBServiceStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// gslbservice returns GSLB service stats
	stats, err := p.client.FindAllStats("gslbservice")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, service := range stats {
		serviceName := getString(service, "servicename")
		if serviceName == "" {
			continue
		}

		// Create tags with service name
		serviceTags := append(baseTags, tags.Tag{Key: "gslbservice", Value: serviceName})

		// State (UP=1, DOWN=0)
		state := getString(service, "state")
		stateValue := float32(0)
		if state == "UP" {
			stateValue = 1
		}
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.service.state",
			Value:     stateValue,
			Tags:      serviceTags,
			Timestamp: timestamp,
		})

		// Total hits
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.service.hits.total",
			Value:     float32(getFloat(service, "tothits")),
			Tags:      serviceTags,
			Timestamp: timestamp,
		})

		// Current connections
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.gslb.service.connections.current",
			Value:     float32(getFloat(service, "curclntconnections")),
			Tags:      serviceTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectCacheStats gathers integrated cache metrics
func (p *netscalerProbe) collectCacheStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// cache is a singleton resource
	cache, err := p.client.FindStat("cache", "")
	if err != nil {
		return nil, err
	}

	if cache == nil {
		return nil, fmt.Errorf("no cache stats returned")
	}

	var datapoints []datapoint.DataPoint

	// Cache hit ratio
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.cache.hit_ratio_percent",
		Value:     float32(getFloat(cache, "cachecqahitpercent")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Objects in cache
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.cache.objects.count",
		Value:     float32(getFloat(cache, "cachecurobjs")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Cache memory used (KB)
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.cache.memory.used_kb",
		Value:     float32(getFloat(cache, "cachecurmemused")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Cache hits total
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.cache.hits.total",
		Value:     float32(getFloat(cache, "cachetotrequestswhits")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Cache misses total
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.cache.misses.total",
		Value:     float32(getFloat(cache, "cachetotrequestsmiss")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectCompressionStats gathers compression metrics
func (p *netscalerProbe) collectCompressionStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// cmp is a singleton resource for compression
	cmp, err := p.client.FindStat("cmp", "")
	if err != nil {
		return nil, err
	}

	if cmp == nil {
		return nil, fmt.Errorf("no compression stats returned")
	}

	var datapoints []datapoint.DataPoint

	// Compression ratio
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.compression.ratio",
		Value:     float32(getFloat(cmp, "compratio")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Compressed bytes total
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.compression.bytes.compressed.total",
		Value:     float32(getFloat(cmp, "comptotdatacompressed")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Original bytes total (before compression)
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.compression.bytes.original.total",
		Value:     float32(getFloat(cmp, "comptotuncompresseddata")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Bandwidth savings (bytes saved)
	origBytes := getFloat(cmp, "comptotuncompresseddata")
	compBytes := getFloat(cmp, "comptotdatacompressed")
	bandwidthSavings := origBytes - compBytes
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.compression.bandwidth_savings.bytes",
		Value:     float32(bandwidthSavings),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectAAAStats gathers AAA (Authentication, Authorization, Accounting) metrics
func (p *netscalerProbe) collectAAAStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// aaauser returns AAA user stats
	stats, err := p.client.FindAllStats("aaauser")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	// Total AAA sessions active
	totalSessions := float32(len(stats))

	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.aaa.sessions.active.total",
		Value:     totalSessions,
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}

// collectAuthenticationVServerStats gathers authentication vServer metrics
func (p *netscalerProbe) collectAuthenticationVServerStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// authenticationvserver returns authentication vServer stats
	stats, err := p.client.FindAllStats("authenticationvserver")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, authvs := range stats {
		vserverName := getString(authvs, "name")
		if vserverName == "" {
			continue
		}

		// Create tags with vserver name
		authTags := append(baseTags, tags.Tag{Key: "authvserver", Value: vserverName})

		// State (UP=1, DOWN=0)
		state := getString(authvs, "state")
		stateValue := float32(0)
		if state == "UP" {
			stateValue = 1
		}
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.aaa.vserver.state",
			Value:     stateValue,
			Tags:      authTags,
			Timestamp: timestamp,
		})

		// Total authentication successes
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.aaa.vserver.auth.successes.total",
			Value:     float32(getFloat(authvs, "totauthsuccesscount")),
			Tags:      authTags,
			Timestamp: timestamp,
		})

		// Total authentication failures
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.aaa.vserver.auth.failures.total",
			Value:     float32(getFloat(authvs, "totauthfailurecount")),
			Tags:      authTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectVPNStats gathers Citrix Gateway/VPN metrics
func (p *netscalerProbe) collectVPNStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// vpnvserver returns VPN virtual server stats
	stats, err := p.client.FindAllStats("vpnvserver")
	if err != nil {
		return nil, err
	}

	var datapoints []datapoint.DataPoint

	for _, vpnvs := range stats {
		vserverName := getString(vpnvs, "name")
		if vserverName == "" {
			continue
		}

		// Create tags with VPN vserver name
		vpnTags := append(baseTags, tags.Tag{Key: "vpnvserver", Value: vserverName})

		// State (UP=1, DOWN=0)
		state := getString(vpnvs, "state")
		stateValue := float32(0)
		if state == "UP" {
			stateValue = 1
		}
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.vpn.vserver.state",
			Value:     stateValue,
			Tags:      vpnTags,
			Timestamp: timestamp,
		})

		// Total hits
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.vpn.vserver.hits.total",
			Value:     float32(getFloat(vpnvs, "tothits")),
			Tags:      vpnTags,
			Timestamp: timestamp,
		})

		// ICA sessions active
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.vpn.vserver.ica.sessions.active",
			Value:     float32(getFloat(vpnvs, "totalica")),
			Tags:      vpnTags,
			Timestamp: timestamp,
		})

		// VPN connections established
		datapoints = append(datapoints, datapoint.DataPoint{
			Name:      "netscaler.vpn.vserver.connections.established",
			Value:     float32(getFloat(vpnvs, "establishedconn")),
			Tags:      vpnTags,
			Timestamp: timestamp,
		})
	}

	return datapoints, nil
}

// collectApplicationFirewallStats gathers WAF/Application Firewall metrics
func (p *netscalerProbe) collectApplicationFirewallStats(timestamp time.Time, baseTags []tags.Tag) ([]datapoint.DataPoint, error) {
	// appfw is a singleton resource for application firewall
	appfw, err := p.client.FindStat("appfw", "")
	if err != nil {
		return nil, err
	}

	if appfw == nil {
		return nil, fmt.Errorf("no application firewall stats returned")
	}

	var datapoints []datapoint.DataPoint

	// Total violations detected
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.violations.total",
		Value:     float32(getFloat(appfw, "appfwtotalviolations")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Requests blocked
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.requests.blocked.total",
		Value:     float32(getFloat(appfw, "appfwreqsblocked")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Responses blocked
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.responses.blocked.total",
		Value:     float32(getFloat(appfw, "appfwrespblocked")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// SQL injection violations
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.violations.sqli.total",
		Value:     float32(getFloat(appfw, "appfwviolsqlinjection")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// XSS (Cross-Site Scripting) violations
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.violations.xss.total",
		Value:     float32(getFloat(appfw, "appfwviolxss")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	// Buffer overflow violations
	datapoints = append(datapoints, datapoint.DataPoint{
		Name:      "netscaler.appfw.violations.buffer_overflow.total",
		Value:     float32(getFloat(appfw, "appfwviolbufferoverflow")),
		Tags:      baseTags,
		Timestamp: timestamp,
	})

	return datapoints, nil
}
