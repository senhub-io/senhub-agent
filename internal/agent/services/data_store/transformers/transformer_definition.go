// senhub-agent/internal/agent/services/data_store/transformers/transformer_definition.go
package transformers

import (
	"strconv"
	"strings"
)

// DefinitionBasedTransformer Methods - Core transformation logic using YAML definitions

// TransformMetricName implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) TransformMetricName(metricName string, tags map[string]string) string {
	// Find matching metric definition
	var matchingDef *MetricDefinition
	for _, metric := range dt.definition.Metrics {
		if dt.matchesMetric(metric, metricName, tags) {
			matchingDef = &metric
			break
		}
	}

	if matchingDef == nil {
		dt.moduleLogger.Debug().
			Str("metric_name", metricName).
			Interface("tags", tags).
			Msg("No matching definition found, using fallback")
		return dt.generateFallbackDisplayName(metricName, tags)
	}

	// Generate display name using definition
	displayName := dt.generateDisplayName(*matchingDef, tags)

	dt.moduleLogger.Debug().
		Str("metric_name", metricName).
		Str("display_name", displayName).
		Interface("tags", tags).
		Msg("Generated display name from definition")

	return displayName
}

// GetUnit implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) GetUnit(metricName string) string {
	// Find matching metric definition
	for _, metric := range dt.definition.Metrics {
		if metric.Name == metricName {
			return dt.normalizeUnit(metric.Unit)
		}
	}

	// Return empty string if no match found
	return ""
}

// GetOtelMapping returns the OTel mapping declared for a metric, or nil
// when the metric has none. Sink converters use it to derive
// semantically correct display units (rate vs absolute, byte context)
// that the legacy display `unit:` field cannot express.
func (dt *DefinitionBasedTransformer) GetOtelMapping(metricName string) *OtelMapping {
	for _, metric := range dt.definition.Metrics {
		if metric.Name == metricName {
			return metric.Otel
		}
	}
	return nil
}

// GetLookup implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) GetLookup(metricName string) string {
	// Find matching metric definition
	for _, metric := range dt.definition.Metrics {
		if metric.Name == metricName {
			return metric.Lookup
		}
	}

	// Return empty string if no match found
	return ""
}

// ApplyUnitCorrection applies source data corrections for inconsistent vendor APIs
func (dt *DefinitionBasedTransformer) ApplyUnitCorrection(metricName string, value float64, tags map[string]string) (float64, bool) {
	if dt.correctionsConfig == nil {
		return value, false
	}

	for _, correction := range dt.correctionsConfig.Corrections {
		if !correction.Enabled {
			continue
		}

		// Check if metric pattern matches
		if !dt.matchesPattern(correction.MetricPattern, metricName) {
			dt.moduleLogger.Debug().
				Str("pattern", correction.MetricPattern).
				Str("metric", metricName).
				Msg("Pattern did not match, skipping correction")
			continue
		}

		// Check vendor filter conditions
		if !dt.matchesVendorFilter(correction.VendorFilter, tags) {
			dt.moduleLogger.Debug().
				Interface("filter", correction.VendorFilter).
				Interface("tags", tags).
				Msg("Vendor filter did not match, skipping correction")
			continue
		}

		// Check detection rule
		if !dt.matchesDetectionRule(correction.DetectionRule, value) {
			dt.moduleLogger.Debug().
				Str("rule", correction.DetectionRule).
				Float64("value", value).
				Msg("Detection rule did not match, skipping correction")
			continue
		}

		// Apply correction
		correctedValue := value * correction.CorrectionFactor
		dt.moduleLogger.Debug().
			Str("metric", metricName).
			Float64("original", value).
			Float64("corrected", correctedValue).
			Float64("factor", correction.CorrectionFactor).
			Str("reason", correction.Reason).
			Msg("Applied unit correction for inconsistent source data")

		return correctedValue, true
	}

	return value, false
}

// matchesVendorFilter checks if tags match the vendor filter conditions
func (dt *DefinitionBasedTransformer) matchesVendorFilter(filter map[string]string, tags map[string]string) bool {
	if len(filter) == 0 {
		return true // No filter means match all
	}

	// Convert tags map to string map for easier lookup
	tagMap := make(map[string]string)
	for key, value := range tags {
		tagMap[key] = value
	}

	// All filter conditions must match
	for filterKey, filterValue := range filter {
		tagValue, exists := tagMap[filterKey]
		if !exists {
			return false
		}

		// Check if tag value contains the filter value (partial match)
		if !strings.Contains(strings.ToLower(tagValue), strings.ToLower(filterValue)) {
			return false
		}
	}

	return true
}

// matchesDetectionRule evaluates the detection rule against the metric value
func (dt *DefinitionBasedTransformer) matchesDetectionRule(rule string, value float64) bool {
	switch {
	case rule == "always":
		return true
	case strings.HasPrefix(rule, "value > "):
		threshold := dt.parseThreshold(rule[8:])
		return value > threshold
	case strings.HasPrefix(rule, "value < "):
		threshold := dt.parseThreshold(rule[8:])
		return value < threshold
	case strings.HasPrefix(rule, "value == "):
		threshold := dt.parseThreshold(rule[9:])
		return value == threshold
	case rule == "never":
		return false
	default:
		dt.moduleLogger.Warn().
			Str("rule", rule).
			Msg("Unknown detection rule, defaulting to false")
		return false
	}
}

// parseThreshold converts a string threshold to float64, supporting scientific notation
func (dt *DefinitionBasedTransformer) parseThreshold(threshold string) float64 {
	if val, err := strconv.ParseFloat(threshold, 64); err == nil {
		return val
	}

	dt.moduleLogger.Warn().
		Str("threshold", threshold).
		Msg("Failed to parse threshold, defaulting to 0")
	return 0
}

// matchesMetric checks if a metric definition matches the given metric name and tags
func (dt *DefinitionBasedTransformer) matchesMetric(def MetricDefinition, metricName string, tags map[string]string) bool {
	// Check if metric name matches (with wildcard support)
	if !dt.matchesPattern(def.Name, metricName) {
		return false
	}

	// Check tag filters if defined
	if len(def.TagFilter) > 0 {
		for filterKey, filterValue := range def.TagFilter {
			tagValue, exists := tags[filterKey]
			if !exists {
				return false
			}

			// Handle special null value filter
			if filterValue == "null" || filterValue == "{webapp_url}" {
				// Custom logic for webapp filtering could be added here
				continue
			}

			if tagValue != filterValue {
				return false
			}
		}
	}

	return true
}

// matchesPattern checks if a pattern matches a string (supports {index} wildcards)
func (dt *DefinitionBasedTransformer) matchesPattern(pattern, text string) bool {
	// Simple exact match
	if pattern == text {
		return true
	}

	// Handle wildcards like {index}, {component}, etc. or simple "*" wildcards
	if strings.Contains(pattern, "{") || strings.Contains(pattern, "*") {
		// Use simple pattern matching by comparing parts
		patternParts := strings.Split(pattern, ".")
		textParts := strings.Split(text, ".")

		if len(patternParts) != len(textParts) {
			return false
		}

		for i, patternPart := range patternParts {
			textPart := textParts[i]

			// If it's a template variable, accept any value
			if strings.Contains(patternPart, "{") && strings.Contains(patternPart, "}") {
				continue
			}

			// If it's a wildcard "*", accept any value
			if patternPart == "*" {
				continue
			}

			// Otherwise, must match exactly
			if patternPart != textPart {
				return false
			}
		}

		return true
	}

	return false
}

// generateDisplayName creates a contextualized display name using the definition and tags
func (dt *DefinitionBasedTransformer) generateDisplayName(def MetricDefinition, tags map[string]string) string {
	displayName := def.DisplayName

	// If no display_name is defined, use the channel name
	if displayName == "" {
		displayName = def.Channel
	}

	// If still empty, generate from metric name
	if displayName == "" {
		displayName = dt.makeReadable(def.Name)
	}

	// Replace template variables with tag values using definition-specific labels
	displayName = dt.replaceTemplateVariablesWithDefinition(displayName, tags, def)

	return displayName
}
