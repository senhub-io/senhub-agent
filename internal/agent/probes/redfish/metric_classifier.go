// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"strings"
	
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/metrics"
)

// RedfishMetricClassifier implements MetricClassifier for Redfish hardware metrics
type RedfishMetricClassifier struct {
	// Classification rules organized by metric patterns
	classificationRules map[string]metrics.MetricClassification
	logger              *logger.ModuleLogger // Module-specific logger
}

// NewRedfishMetricClassifier creates a new Redfish metric classifier
func NewRedfishMetricClassifier(baseLogger *logger.Logger) *RedfishMetricClassifier {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.redfish.classifier")
	
	classifier := &RedfishMetricClassifier{
		classificationRules: make(map[string]metrics.MetricClassification),
		logger:              moduleLogger,
	}
	
	// Initialize classification rules
	classifier.initializeClassificationRules()
	
	classifier.logger.Info().
		Int("rules_count", len(classifier.classificationRules)).
		Msg("Redfish metric classifier initialized")
	
	return classifier
}

// ClassifyMetric assigns classification to a Redfish metric
func (c *RedfishMetricClassifier) ClassifyMetric(metricName string, value float64, metricTags []tags.Tag) metrics.MetricClassification {
	metricLower := strings.ToLower(metricName)
	
	// Try exact match first
	if classification, exists := c.classificationRules[metricLower]; exists {
		c.logger.Debug().
			Str("metric", metricName).
			Str("match_type", "exact").
			Str("pattern", metricLower).
			Msg("Metric classification found via exact match")
		return c.enhanceClassification(classification, metricName, value, metricTags)
	}
	
	// Try pattern matching for dynamic metrics
	for pattern, classification := range c.classificationRules {
		if c.matchesPattern(metricLower, pattern) {
			c.logger.Debug().
				Str("metric", metricName).
				Str("match_type", "pattern").
				Str("pattern", pattern).
				Msg("Metric classification found via pattern match")
			return c.enhanceClassification(classification, metricName, value, metricTags)
		}
	}
	
	// Fallback classification based on metric name analysis
	c.logger.Debug().
		Str("metric", metricName).
		Str("match_type", "name_analysis").
		Msg("Using fallback classification based on name analysis")
	return c.classifyByNameAnalysis(metricName, value, metricTags)
}

// GetSupportedCategories returns the categories this classifier supports
func (c *RedfishMetricClassifier) GetSupportedCategories() []metrics.MetricCategory {
	return []metrics.MetricCategory{
		metrics.CategoryHealth,
		metrics.CategoryPerformance,
		metrics.CategoryCapacity,
		metrics.CategoryQuality,
	}
}

// GetCategoryMetrics returns all metrics for a given category
func (c *RedfishMetricClassifier) GetCategoryMetrics(category metrics.MetricCategory) []string {
	var metricNames []string
	
	for metricName, classification := range c.classificationRules {
		if classification.Category == category {
			metricNames = append(metricNames, metricName)
		}
	}
	
	return metricNames
}

// initializeClassificationRules sets up the comprehensive classification rules for Redfish metrics
func (c *RedfishMetricClassifier) initializeClassificationRules() {
	// Health - System Health
	c.addRule("redfish.system.health", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "Overall system health status",
		DisplayName: "System Health",
		Group:       "System",
		SortOrder:   1,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	c.addRule("redfish.system.power_state", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategoryAvailability,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "System power state (1=on, 0=off)",
		DisplayName: "Power State",
		Group:       "System",
		SortOrder:   2,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = powered off
		},
	})
	
	// Health - Thermal
	c.addRule("redfish.thermal.temperature.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityHigh,
		Unit:        metrics.UnitCelsius,
		Description: "Component temperature reading",
		DisplayName: "Temperature",
		Group:       "Thermal",
		SortOrder:   10,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{75}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{85}[0]},
		},
	})
	
	c.addRule("redfish.thermal.fan.speed.*", metrics.MetricClassification{
		Category:    metrics.CategoryPerformance,
		Subcategory: metrics.SubcategoryProcessingSpeed,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitRPM,
		Description: "Fan rotational speed",
		DisplayName: "Fan Speed",
		Group:       "Thermal",
		SortOrder:   11,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Min: &[]float64{500}[0]}, // Below 500 RPM critical
		},
	})
	
	c.addRule("redfish.thermal.fan.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityHigh,
		Unit:        metrics.UnitBoolean,
		Description: "Fan health status",
		DisplayName: "Fan Health",
		Group:       "Thermal",
		SortOrder:   12,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	// Health - Power
	c.addRule("redfish.power.supply.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "Power supply health status",
		DisplayName: "PSU Health",
		Group:       "Power",
		SortOrder:   20,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	c.addRule("redfish.power.consumption.*", metrics.MetricClassification{
		Category:    metrics.CategoryCapacity,
		Subcategory: metrics.SubcategoryStorage, // Using storage as general resource
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitWatts,
		Description: "Power consumption in watts",
		DisplayName: "Power Consumption",
		Group:       "Power",
		SortOrder:   21,
	})
	
	c.addRule("redfish.power.voltage.*", metrics.MetricClassification{
		Category:    metrics.CategoryQuality,
		Subcategory: metrics.SubcategoryStability,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitVolts,
		Description: "Voltage reading",
		DisplayName: "Voltage",
		Group:       "Power",
		SortOrder:   22,
	})
	
	// Capacity - CPU
	c.addRule("redfish.processor.utilization.*", metrics.MetricClassification{
		Category:    metrics.CategoryCapacity,
		Subcategory: metrics.SubcategoryCPU,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitPercent,
		Description: "CPU utilization percentage",
		DisplayName: "CPU Utilization",
		Group:       "Processor",
		SortOrder:   30,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{80}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{90}[0]},
		},
	})
	
	c.addRule("redfish.processor.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "Processor health status",
		DisplayName: "CPU Health",
		Group:       "Processor",
		SortOrder:   31,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	// Capacity - Memory
	c.addRule("redfish.memory.utilization.*", metrics.MetricClassification{
		Category:    metrics.CategoryCapacity,
		Subcategory: metrics.SubcategoryMemory,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitPercent,
		Description: "Memory utilization percentage",
		DisplayName: "Memory Utilization",
		Group:       "Memory",
		SortOrder:   40,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{85}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{95}[0]},
		},
	})
	
	c.addRule("redfish.memory.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "Memory module health status",
		DisplayName: "Memory Health",
		Group:       "Memory",
		SortOrder:   41,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	// Capacity - Storage
	c.addRule("redfish.storage.drive.capacity_used.*", metrics.MetricClassification{
		Category:    metrics.CategoryCapacity,
		Subcategory: metrics.SubcategoryStorage,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitPercent,
		Description: "Drive capacity utilization percentage",
		DisplayName: "Drive Capacity Used",
		Group:       "Storage",
		SortOrder:   50,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{85}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{95}[0]},
		},
	})
	
	c.addRule("redfish.storage.drive.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityCritical,
		Unit:        metrics.UnitBoolean,
		Description: "Drive health status",
		DisplayName: "Drive Health",
		Group:       "Storage",
		SortOrder:   51,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
	
	c.addRule("redfish.storage.pool.capacity_used.*", metrics.MetricClassification{
		Category:    metrics.CategoryCapacity,
		Subcategory: metrics.SubcategoryStorage,
		Severity:    metrics.SeverityMedium,
		Unit:        metrics.UnitPercent,
		Description: "Storage pool capacity utilization percentage",
		DisplayName: "Pool Capacity Used",
		Group:       "Storage",
		SortOrder:   52,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{85}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{95}[0]},
		},
	})
	
	// Quality - Error Rates
	c.addRule("redfish.storage.drive.read_errors.*", metrics.MetricClassification{
		Category:    metrics.CategoryQuality,
		Subcategory: metrics.SubcategoryErrorRates,
		Severity:    metrics.SeverityHigh,
		Unit:        metrics.UnitCount,
		Description: "Drive read error count",
		DisplayName: "Drive Read Errors",
		Group:       "Storage",
		SortOrder:   60,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{10}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{50}[0]},
		},
	})
	
	c.addRule("redfish.storage.drive.write_errors.*", metrics.MetricClassification{
		Category:    metrics.CategoryQuality,
		Subcategory: metrics.SubcategoryErrorRates,
		Severity:    metrics.SeverityHigh,
		Unit:        metrics.UnitCount,
		Description: "Drive write error count",
		DisplayName: "Drive Write Errors",
		Group:       "Storage",
		SortOrder:   61,
		Thresholds: &metrics.MetricThresholds{
			Warning:  &metrics.ThresholdRange{Max: &[]float64{10}[0]},
			Critical: &metrics.ThresholdRange{Max: &[]float64{50}[0]},
		},
	})
	
	// Performance - Storage
	c.addRule("redfish.storage.drive.read_iops.*", metrics.MetricClassification{
		Category:    metrics.CategoryPerformance,
		Subcategory: metrics.SubcategoryThroughput,
		Severity:    metrics.SeverityLow,
		Unit:        metrics.UnitOpsPerSecond,
		Description: "Drive read IOPS",
		DisplayName: "Drive Read IOPS",
		Group:       "Storage",
		SortOrder:   70,
	})
	
	c.addRule("redfish.storage.drive.write_iops.*", metrics.MetricClassification{
		Category:    metrics.CategoryPerformance,
		Subcategory: metrics.SubcategoryThroughput,
		Severity:    metrics.SeverityLow,
		Unit:        metrics.UnitOpsPerSecond,
		Description: "Drive write IOPS",
		DisplayName: "Drive Write IOPS",
		Group:       "Storage",
		SortOrder:   71,
	})
	
	// Network Adapter metrics
	c.addRule("redfish.networkadapter.health.*", metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategoryConnectivity,
		Severity:    metrics.SeverityHigh,
		Unit:        metrics.UnitBoolean,
		Description: "Network adapter health status",
		DisplayName: "Network Health",
		Group:       "Network",
		SortOrder:   80,
		Thresholds: &metrics.MetricThresholds{
			Critical: &metrics.ThresholdRange{Max: &[]float64{0}[0]}, // 0 = unhealthy
		},
	})
}

// addRule is a helper to add classification rules
func (c *RedfishMetricClassifier) addRule(pattern string, classification metrics.MetricClassification) {
	c.classificationRules[strings.ToLower(pattern)] = classification
}

// matchesPattern checks if a metric name matches a pattern (supports * wildcards)
func (c *RedfishMetricClassifier) matchesPattern(metricName, pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return metricName == pattern
	}
	
	// Split pattern by wildcards
	parts := strings.Split(pattern, "*")
	
	// Check if the metric name contains all the pattern parts in order
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue // Skip empty parts (from leading/trailing/consecutive wildcards)
		}
		
		// Find the part in the metric name starting from current position
		index := strings.Index(metricName[pos:], part)
		if index == -1 {
			return false // Part not found
		}
		
		// Update position for next search
		pos += index + len(part)
		
		// For the first part, it must be at the beginning if there's no leading wildcard
		if i == 0 && !strings.HasPrefix(pattern, "*") && index != 0 {
			return false
		}
	}
	
	// For the last part, it must be at the end if there's no trailing wildcard
	lastPart := parts[len(parts)-1]
	if lastPart != "" && !strings.HasSuffix(pattern, "*") {
		return strings.HasSuffix(metricName, lastPart)
	}
	
	return true
}

// classifyByNameAnalysis provides fallback classification based on metric name analysis
func (c *RedfishMetricClassifier) classifyByNameAnalysis(metricName string, value float64, metricTags []tags.Tag) metrics.MetricClassification {
	metricLower := strings.ToLower(metricName)
	
	// Health keywords
	if strings.Contains(metricLower, "health") || strings.Contains(metricLower, "status") || strings.Contains(metricLower, "state") {
		return metrics.MetricClassification{
			Category:    metrics.CategoryHealth,
			Subcategory: metrics.SubcategorySystemHealth,
			Severity:    metrics.SeverityHigh,
			Unit:        metrics.UnitBoolean,
			Description: "System component health status",
			DisplayName: c.formatDisplayName(metricName),
			Group:       "System",
			SortOrder:   999,
		}
	}
	
	// Temperature metrics
	if strings.Contains(metricLower, "temperature") || strings.Contains(metricLower, "temp") {
		return metrics.MetricClassification{
			Category:    metrics.CategoryHealth,
			Subcategory: metrics.SubcategorySystemHealth,
			Severity:    metrics.SeverityMedium,
			Unit:        metrics.UnitCelsius,
			Description: "Temperature reading",
			DisplayName: c.formatDisplayName(metricName),
			Group:       "Thermal",
			SortOrder:   999,
		}
	}
	
	// Capacity/utilization metrics
	if strings.Contains(metricLower, "utilization") || strings.Contains(metricLower, "used") || strings.Contains(metricLower, "capacity") {
		return metrics.MetricClassification{
			Category:    metrics.CategoryCapacity,
			Subcategory: metrics.SubcategoryStorage, // Default to storage
			Severity:    metrics.SeverityMedium,
			Unit:        metrics.UnitPercent,
			Description: "Resource utilization",
			DisplayName: c.formatDisplayName(metricName),
			Group:       "Capacity",
			SortOrder:   999,
		}
	}
	
	// Error metrics
	if strings.Contains(metricLower, "error") || strings.Contains(metricLower, "fail") {
		return metrics.MetricClassification{
			Category:    metrics.CategoryQuality,
			Subcategory: metrics.SubcategoryErrorRates,
			Severity:    metrics.SeverityHigh,
			Unit:        metrics.UnitCount,
			Description: "Error count or rate",
			DisplayName: c.formatDisplayName(metricName),
			Group:       "System",
			SortOrder:   999,
		}
	}
	
	// Default classification
	return metrics.MetricClassification{
		Category:    metrics.CategoryHealth,
		Subcategory: metrics.SubcategorySystemHealth,
		Severity:    metrics.SeverityLow,
		Unit:        metrics.UnitNone,
		Description: "Unclassified Redfish metric",
		DisplayName: c.formatDisplayName(metricName),
		Group:       "System",
		SortOrder:   999,
	}
}

// enhanceClassification adds dynamic information to a base classification
func (c *RedfishMetricClassifier) enhanceClassification(base metrics.MetricClassification, metricName string, value float64, metricTags []tags.Tag) metrics.MetricClassification {
	enhanced := base
	
	// Set display name if not already set
	if enhanced.DisplayName == "" {
		enhanced.DisplayName = c.formatDisplayName(metricName)
	}
	
	// Add component information from tags
	for _, tag := range metricTags {
		switch tag.Key {
		case "component", "sensor_name", "drive_name", "fan_name":
			if enhanced.Tags == nil {
				enhanced.Tags = make(map[string]string)
			}
			enhanced.Tags["component"] = tag.Value
		case "location", "slot":
			if enhanced.Tags == nil {
				enhanced.Tags = make(map[string]string)
			}
			enhanced.Tags["location"] = tag.Value
		}
	}
	
	return enhanced
}

// formatDisplayName creates a user-friendly display name from a metric name
func (c *RedfishMetricClassifier) formatDisplayName(metricName string) string {
	// Remove prefix
	displayName := metricName
	if strings.HasPrefix(displayName, "redfish.") {
		displayName = strings.TrimPrefix(displayName, "redfish.")
	}
	
	// Replace dots and underscores with spaces
	displayName = strings.ReplaceAll(displayName, ".", " ")
	displayName = strings.ReplaceAll(displayName, "_", " ")
	
	// Title case
	words := strings.Fields(displayName)
	for i, word := range words {
		words[i] = strings.Title(strings.ToLower(word))
	}
	
	return strings.Join(words, " ")
}