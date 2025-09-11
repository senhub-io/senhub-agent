package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// CollectInfrastructureMetrics collects all infrastructure-related metrics
func (mc *MetricsCollector) CollectInfrastructureMetrics(ctx context.Context, timestamp time.Time, inventoryService *InventoryService) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting infrastructure metrics")

	var metrics []datapoint.DataPoint

	// Get all machines data (no time filter for current state metrics)
	machines, err := mc.client.GetMachines(ctx, time.Time{})
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get machines for infrastructure metrics")
		return nil, err
	}

	mc.logger.Debug().Int("machines_count", len(machines)).Msg("Retrieved machines for infrastructure metrics")

	// Calculate metrics directly inline for better performance
	var (
		totalMachines     = len(machines)
		registeredCount   = 0
		unregisteredCount = 0
		faultyCount       = 0
		maintenanceCount  = 0
	)

	// Detailed fault state tracking for multi-session machines
	var (
		multiSessionFaultyCount = 0
		faultStateCounts        = make(map[int]int)
	)

	// Single pass through machines for all calculations
	for _, machine := range machines {
		switch machine.RegistrationState {
		case RegistrationStateRegistered:
			registeredCount++
		case RegistrationStateUnregistered:
			unregisteredCount++
			mc.logger.Info().
				Str("machine_name", machine.MachineName).
				Int("machine_role", machine.MachineRole).
				Str("machine_id", machine.MachineId).
				Str("dns_name", machine.DnsName).
				Int("registration_state", machine.RegistrationState).
				Int("fault_state", machine.FaultState).
				Str("os_type", machine.OSType).
				Int("lifecycle_state", machine.LifecycleState).
				Msg("🔍 Found unregistered machine")
		case RegistrationStateAgentError:
			unregisteredCount++ // Count AgentError as unregistered for metrics
			mc.logger.Info().
				Str("machine_name", machine.MachineName).
				Int("machine_role", machine.MachineRole).
				Str("machine_id", machine.MachineId).
				Str("dns_name", machine.DnsName).
				Int("registration_state", machine.RegistrationState).
				Int("fault_state", machine.FaultState).
				Str("os_type", machine.OSType).
				Int("lifecycle_state", machine.LifecycleState).
				Msg("🔍 Found machine with agent error (counted as unregistered)")
		}

		// Track faulty machines (FaultState != None)
		if machine.FaultState != FaultStateNone {
			faultyCount++

			// Track detailed fault states for multi-session machines
			// Since MachineRole doesn't distinguish in this environment, use machines with valid names and DesktopGroupId
			if machine.MachineName != "" && machine.DesktopGroupId != "" {
				multiSessionFaultyCount++
				faultStateCounts[machine.FaultState]++
			}
		}

		// Also track unregistered multi-session machines
		// All unregistered multi-session machines are considered faulty regardless of FaultState
		// Include both Unregistered (0) and AgentError (2) states
		// Exclude phantom machines (no name) and infrastructure machines (no DesktopGroupId)
		if (machine.RegistrationState == RegistrationStateUnregistered || machine.RegistrationState == RegistrationStateAgentError) &&
			machine.MachineName != "" && machine.DesktopGroupId != "" {
			// If not already counted as faulty (FaultState != None), add to multi-session counts
			if machine.FaultState == FaultStateNone {
				multiSessionFaultyCount++
				faultStateCounts[FaultStateUnregistered]++ // Treat as unregistered fault
				mc.logger.Info().
					Str("machine_name", machine.MachineName).
					Int("machine_role", machine.MachineRole).
					Int("registration_state", machine.RegistrationState).
					Int("fault_state", machine.FaultState).
					Str("desktop_group_id", machine.DesktopGroupId).
					Msg("✅ Added unregistered machine in desktop group to multi-session faulty count")
			} else {
				// Machine already counted in the faulty section above, but ensure it's in unregistered category
				mc.logger.Info().
					Str("machine_name", machine.MachineName).
					Int("machine_role", machine.MachineRole).
					Int("registration_state", machine.RegistrationState).
					Int("fault_state", machine.FaultState).
					Str("desktop_group_id", machine.DesktopGroupId).
					Msg("Unregistered machine in desktop group already counted as faulty")
			}
		}

		// Count as maintenance only machines with RegistrationState = 2 (AgentError)
		// This filters to only problematic machines rather than all maintenance mode machines
		if machine.InMaintenanceMode && machine.RegistrationState == RegistrationStateAgentError {
			maintenanceCount++
			mc.logger.Debug().
				Str("machine_name", machine.MachineName).
				Int("registration_state", machine.RegistrationState).
				Int("fault_state", machine.FaultState).
				Msg("Machine counted as maintenance (RegistrationState=2/AgentError)")
		}
	}

	// Create all metrics with proper units
	metrics = []datapoint.DataPoint{
		{
			Name:      "machines_total",
			Value:     float32(totalMachines),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_registered",
			Value:     float32(registeredCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "unregistered_VDA_count",
			Value:     float32(unregisteredCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_faulty",
			Value:     float32(faultyCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
		{
			Name:      "machines_maintenance",
			Value:     float32(maintenanceCount),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		},
	}

	// Add detailed fault state metrics for multi-session machines (matching Citrix Director)
	detailedFaultMetrics := mc.createDetailedFaultStateMetrics(faultStateCounts, multiSessionFaultyCount, timestamp)
	metrics = append(metrics, detailedFaultMetrics...)

	// Add inventory-related infrastructure metrics if inventory service is available
	if inventoryService != nil {
		inventoryMetrics := mc.createInventoryInfrastructureMetrics(timestamp, inventoryService)
		metrics = append(metrics, inventoryMetrics...)
	}

	mc.logger.Info().
		Int("total", totalMachines).
		Int("registered", registeredCount).
		Int("unregistered", unregisteredCount).
		Int("faulty", faultyCount).
		Int("maintenance", maintenanceCount).
		Int("multi_session_faulty", multiSessionFaultyCount).
		Msg("✅ Infrastructure metrics calculated")

	return metrics, nil
}

// createDetailedFaultStateMetrics creates detailed fault state metrics matching Citrix Director
func (mc *MetricsCollector) createDetailedFaultStateMetrics(faultStateCounts map[int]int, totalFaultyCount int, timestamp time.Time) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	// Total multi-session faulty machines
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "machines_total",
		Value:     float32(totalFaultyCount),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "multi_session_faults"},
		},
	})

	// Detailed fault state metrics (matching Director categories)
	faultStateNames := map[int]string{
		FaultStateFailedToStart: "boot_failure",  // "Échec du démarrage"
		FaultStateStuckOnBoot:   "stuck_at_boot", // "Bloquées au démarrage"
		FaultStateUnregistered:  "unregistered",  // "Non enregistrées"
		FaultStateMaxCapacity:   "max_capacity",  // "Charge maximale"
		FaultStateVMNotFound:    "vm_not_found",  // "Machine virtuelle introuvable"
		FaultStateUnknown:       "unknown",       // "Inconnue"
	}

	for faultState, name := range faultStateNames {
		count := faultStateCounts[faultState] // Will be 0 if not found
		metrics = append(metrics, datapoint.DataPoint{
			Name:      name,
			Value:     float32(count),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "multi_session_faults"},
				{Key: "fault_state", Value: name},
			},
		})
	}

	mc.logger.Debug().
		Interface("fault_state_counts", faultStateCounts).
		Int("total_faulty", totalFaultyCount).
		Msg("Multi-session fault state metrics created")

	return metrics
}

// createInventoryInfrastructureMetrics creates infrastructure metrics from inventory data
func (mc *MetricsCollector) createInventoryInfrastructureMetrics(timestamp time.Time, inventoryService *InventoryService) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	// Get inventory statistics
	inventoryStats := inventoryService.GetInventoryStats()
	siteID, siteName := inventoryService.GetSiteInfo()

	// Base tags for all infrastructure metrics
	baseTags := []tags.Tag{
		{Key: "metric_type", Value: "infrastructure"},
		{Key: "site_name", Value: siteName},
		{Key: "site_id", Value: siteID},
	}

	if cached, ok := inventoryStats["cached"].(bool); ok && cached {
		// Cache age metric
		if cacheAge, ok := inventoryStats["cache_age"].(time.Duration); ok {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      "cache_age_sec",
				Value:     float32(cacheAge.Seconds()),
				Tags:      baseTags,
				Timestamp: timestamp,
			})
		}

		// Update duration metric
		if updateDuration, ok := inventoryStats["update_duration"].(time.Duration); ok {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      "cache_update_sec",
				Value:     float32(updateDuration.Seconds()),
				Tags:      baseTags,
				Timestamp: timestamp,
			})
		}

		// Inventory counts
		if deliveryGroups, ok := inventoryStats["delivery_groups"].(int); ok {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      "delivery_groups",
				Value:     float32(deliveryGroups),
				Tags:      baseTags,
				Timestamp: timestamp,
			})
		}

		if controllers, ok := inventoryStats["controllers"].(int); ok {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      "controllers",
				Value:     float32(controllers),
				Tags:      baseTags,
				Timestamp: timestamp,
			})
		}

		if applications, ok := inventoryStats["applications"].(int); ok {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      "applications",
				Value:     float32(applications),
				Tags:      baseTags,
				Timestamp: timestamp,
			})
		}
	}

	mc.logger.Debug().
		Int("inventory_infrastructure_metrics", len(metrics)).
		Str("site_name", siteName).
		Msg("📦 Created inventory infrastructure metrics")

	return metrics
}
