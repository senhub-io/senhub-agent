package citrix

import (
	"context"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// CollectFailureMetrics collects all failure-related metrics
func (mc *MetricsCollector) CollectFailureMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting failure metrics")

	var metrics []datapoint.DataPoint

	// Get connection failures from last hour
	oneHourAgo := timestamp.Add(-1 * time.Hour)

	// 1. Connection failures (total and by category)
	connectionFailures, err := mc.client.GetConnectionFailureLogs(ctx, oneHourAgo)

	// Always add total connection failures metric
	connectionFailureMetric := datapoint.DataPoint{
		Name:      MetricFailuresTotal,
		Value:     0, // Default to 0 if error
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "connection_failures"},
		},
	}

	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connection failures")
		// Keep metric with 0 value
	} else {
		// Update with actual count
		connectionFailureMetric.Value = float32(len(connectionFailures))
	}
	metrics = append(metrics, connectionFailureMetric)

	// Always add detailed failure category metrics (even if empty/zero)
	categories, err := mc.client.GetConnectionFailureCategories(ctx)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get failure categories")
		// Create zero metrics without categories
		categoryMetrics := mc.createZeroFailureCategoryMetrics(timestamp)
		metrics = append(metrics, categoryMetrics...)
	} else {
		// Create detailed metrics based on actual data
		var failuresToProcess []ConnectionFailureLog
		if connectionFailures != nil {
			failuresToProcess = connectionFailures
		}
		categoryMetrics := mc.calculateFailuresByCategory(failuresToProcess, categories, timestamp)
		metrics = append(metrics, categoryMetrics...)
	}

	// 2. Black hole machines (machines with 4+ connection failures)
	// Black hole machines metrics removed - concept unclear and not actionable

	mc.logger.Debug().Int("metrics_count", len(metrics)).Msg("Failure metrics collected")
	return metrics, nil
}

// calculateFailuresByCategory creates detailed metrics for connection failures by category
// Uses dynamic mapping from environment + static local conversion for consistent categorization
func (mc *MetricsCollector) calculateFailuresByCategory(failures []ConnectionFailureLog, categories []ConnectionFailureCategory, timestamp time.Time) []datapoint.DataPoint {
	// Step 1: Dynamic mapping from environment API (ConnectionFailureEnumValue → Category)
	enumToCategory := make(map[int]int)
	for _, cat := range categories {
		enumToCategory[cat.ConnectionFailureEnumValue] = cat.Category
	}

	// Step 2: Static local conversion (Category → Failure Type) - based on observed patterns
	// This mapping is derived from Citrix documentation and environment analysis
	categoryToType := map[int]string{
		0: "configuration", // Configuration issues (SessionSharingDisabled, etc.)
		1: "client",        // Client/network connection issues
		2: "machine",       // Machine/VDA failures (locked, not ready, etc.)
		3: "capacity",      // Capacity/resource unavailable
		4: "license",       // License server issues
		5: "other",         // Other/unknown issues
	}

	// Step 3: Count failures by type
	typeCounts := make(map[string]int)
	unknownCategories := make(map[int]int)
	unmappedEnums := make(map[int]int)

	for _, failure := range failures {
		if category, exists := enumToCategory[failure.ConnectionFailureEnumValue]; exists {
			if failureType, typeExists := categoryToType[category]; typeExists {
				typeCounts[failureType]++
			} else {
				// Unknown category, track for debugging
				unknownCategories[category]++
				typeCounts["other"]++
			}
		} else {
			// Unknown enum value, track for debugging
			unmappedEnums[failure.ConnectionFailureEnumValue]++
			typeCounts["other"]++
		}
	}

	// Log unknown values for debugging
	if len(unknownCategories) > 0 {
		mc.logger.Debug().
			Interface("unknown_categories", unknownCategories).
			Msg("Found unknown category codes in environment")
	}
	if len(unmappedEnums) > 0 {
		mc.logger.Debug().
			Interface("unmapped_enum_values", unmappedEnums).
			Msg("Found ConnectionFailureEnumValue codes not in environment mapping")
	}

	var metrics []datapoint.DataPoint

	// Step 4: Create metrics based on failure types (consistent across environments)
	failureTypeNames := []string{
		"client_connection_failures", // Client-side issues
		"configuration_errors",       // Configuration problems
		"machine_failures",           // Machine/VDA issues
		"capacity_unavailable",       // Capacity/resource problems
		"licenses_unavailable",       // License issues
		"other_failures",             // Other/unknown issues
	}

	for _, typeName := range failureTypeNames {
		// Map type name back to our counting key
		var countKey string
		switch typeName {
		case "client_connection_failures":
			countKey = "client"
		case "configuration_errors":
			countKey = "configuration"
		case "machine_failures":
			countKey = "machine"
		case "capacity_unavailable":
			countKey = "capacity"
		case "licenses_unavailable":
			countKey = "license"
		case "other_failures":
			countKey = "other"
		}

		count := typeCounts[countKey] // Will be 0 if type not found
		metrics = append(metrics, datapoint.DataPoint{
			Name:      typeName,
			Value:     float32(count),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "connection_failures"},
				{Key: "failure_category", Value: typeName},
			},
		})
	}

	mc.logger.Debug().
		Interface("failure_type_counts", typeCounts).
		Int("total_failures", len(failures)).
		Msg("Connection failures categorized by type (documentation-based)")

	return metrics
}

// createZeroFailureCategoryMetrics creates zero-value metrics for all failure categories
func (mc *MetricsCollector) createZeroFailureCategoryMetrics(timestamp time.Time) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	// Create zero metrics for all failure types (consistent across environments)
	categoryNames := []string{
		"client_connection_failures", // Client-side issues
		"configuration_errors",       // Configuration problems
		"machine_failures",           // Machine/VDA issues
		"capacity_unavailable",       // Capacity/resource problems
		"licenses_unavailable",       // License issues
		"other_failures",             // Other/unknown issues
	}

	for _, name := range categoryNames {
		metrics = append(metrics, datapoint.DataPoint{
			Name:      name,
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "connection_failures"},
				{Key: "failure_category", Value: name},
			},
		})
	}

	mc.logger.Debug().
		Int("zero_metrics_created", len(metrics)).
		Msg("Created zero failure category metrics")

	return metrics
}

// calculateBlackHoleMachines function removed - concept unclear and not operationally actionable

// TODO: Implement application_failures_count when we have ApplicationErrors endpoint
