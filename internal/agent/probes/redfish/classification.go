// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"strings"
)

// Classification structure for Redfish metrics
const (
	// Categories
	CategoryHealth      = "health"
	CategoryCapacity    = "capacity"
	CategoryPerformance = "performance"
	CategoryOperations  = "operations"
	CategoryEvents      = "events"

	// Components
	ComponentController   = "controller"
	ComponentDrive        = "drive"
	ComponentPool         = "pool"
	ComponentVolume       = "volume"
	ComponentLogs         = "logs"
	ComponentEventService = "eventservice"
	ComponentSystem       = "system"
	ComponentThermal      = "thermal"
	ComponentPower        = "power"
	ComponentNetwork      = "network"

	// Sections (Main UI groups)
	SectionOverview = "overview"
	SectionStorage  = "storage"
	SectionHardware = "hardware"
	SectionEvents   = "events"

	// Tag Keys
	TagKeyCategory  = "category"
	TagKeyComponent = "component"
	TagKeySection   = "section"
	TagKeyDashboard = "dashboard"
)

// AddClassificationTags adds category, component, section, and dashboard tags to a datapoint
// to enable UI-friendly grouping capabilities
func AddClassificationTags(dp *datapoint.DataPoint) {
	metricName := dp.Name

	// Handle category classification
	switch {
	case strings.Contains(metricName, ".health") || 
		 strings.Contains(metricName, ".failure_predicted") ||
		 strings.Contains(metricName, ".redundancy"):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyCategory, Value: CategoryHealth})

	case strings.Contains(metricName, ".capacity"):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyCategory, Value: CategoryCapacity})

	case strings.Contains(metricName, ".io."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyCategory, Value: CategoryPerformance})

	case strings.Contains(metricName, ".operation"):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyCategory, Value: CategoryOperations})

	case strings.Contains(metricName, ".logs.") || strings.Contains(metricName, ".eventservice."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyCategory, Value: CategoryEvents})
	}

	// Handle component classification
	switch {
	case strings.Contains(metricName, ".controller."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentController})

	case strings.Contains(metricName, ".drive."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentDrive})

	case strings.Contains(metricName, ".pool."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentPool})

	case strings.Contains(metricName, ".volume."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentVolume})

	case strings.Contains(metricName, ".logs."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentLogs})

	case strings.Contains(metricName, ".eventservice."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentEventService})

	case strings.Contains(metricName, ".system.") || strings.HasPrefix(metricName, "hardware.system."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentSystem})

	case strings.Contains(metricName, ".thermal.") || 
		 strings.Contains(metricName, ".temperature.") || 
		 strings.Contains(metricName, ".fan."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentThermal})

	case strings.Contains(metricName, ".power.") || strings.Contains(metricName, ".psu."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentPower})

	case strings.Contains(metricName, ".network.") || strings.Contains(metricName, ".interface."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyComponent, Value: ComponentNetwork})
	}

	// Handle section classification (top-level UI groups)
	switch {
	case strings.HasPrefix(metricName, "hardware.storage."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeySection, Value: SectionStorage})

	case strings.HasPrefix(metricName, "hardware.logs.") || 
		 strings.HasPrefix(metricName, "hardware.eventservice."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeySection, Value: SectionEvents})

	case strings.Contains(metricName, ".thermal.") || 
		 strings.Contains(metricName, ".fan.") || 
		 strings.Contains(metricName, ".power.") || 
		 strings.Contains(metricName, ".psu.") ||
		 strings.Contains(metricName, ".system."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeySection, Value: SectionHardware})

	case strings.Contains(metricName, ".network."):
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeySection, Value: SectionHardware})
	}

	// Add overview dashboard tag for critical health and status metrics
	if (strings.Contains(metricName, ".health") ||
		strings.Contains(metricName, ".failure_predicted") ||
		strings.Contains(metricName, "logs.entries.critical")) {
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyDashboard, Value: SectionOverview})
	}

	// Tag overview metrics for capacity
	if strings.Contains(metricName, ".capacity.used_percent") || 
	   strings.Contains(metricName, ".capacity.free_percent") {
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyDashboard, Value: SectionOverview})
	}

	// Tag important performance metrics for overview
	if strings.Contains(metricName, ".io.total_ops") || 
	   strings.Contains(metricName, ".io.read.latency") || 
	   strings.Contains(metricName, ".io.write.latency") {
		dp.Tags = append(dp.Tags, tags.Tag{Key: TagKeyDashboard, Value: SectionOverview})
	}
}

// AddClassificationTagsToDataPoints adds UI classification tags to an array of datapoints
func AddClassificationTagsToDataPoints(datapoints []data_store.DataPoint) {
	for i := range datapoints {
		dp := &datapoints[i]
		AddClassificationTags(dp)
	}
}