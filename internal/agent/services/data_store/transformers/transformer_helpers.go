// senhub-agent/internal/agent/services/data_store/transformers/transformer_helpers.go
package transformers

import (
	"fmt"
	"strings"
)

// Template Variable Replacement Helpers

// replaceTemplateVariables replaces {variable} placeholders with tag values
func (dt *DefinitionBasedTransformer) replaceTemplateVariables(template string, tags map[string]string) string {
	result := template

	// Replace common template variables
	templateVars := []string{
		"index", "component", "core", "interface", "device", "mountpoint",
		"volume_name", "volume_id", "drive_name", "drive_id", "pool_id",
		"diskgroup_name", "diskgroup_id", "controller_name", "controller_id",
		"fan_name", "sensor_location", "target", "operation_type",
		"raid_type", "host", "system_name", "slot", "enclosure_id",
		"adapter_id", "memory_controller",
	}

	for _, varName := range templateVars {
		placeholder := fmt.Sprintf("{%s}", varName)
		if strings.Contains(result, placeholder) {
			value := tags[varName]

			// Handle special case mappings
			if value == "" {
				switch varName {
				case "core":
					// Try "index" tag if "core" tag is not found
					value = tags["index"]
				case "index":
					// Try "core" tag if "index" tag is not found
					value = tags["core"]
				case "device":
					// Try "interface" tag if "device" tag is not found
					value = tags["interface"]
				case "interface":
					// Try "device" tag if "interface" tag is not found
					value = tags["device"]
				case "drive_name":
					// Try "drive_id" tag if "drive_name" tag is not found
					value = tags["drive_id"]
				case "drive_id":
					// Try "drive_name" tag if "drive_id" tag is not found
					value = tags["drive_name"]
				}
			}

			if value == "" {
				// Fallback values
				switch varName {
				case "index", "core":
					value = "0"
				case "component":
					value = "Unknown"
				default:
					value = fmt.Sprintf("<%s>", varName)
				}
			}
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}

	return result
}

// replaceTemplateVariablesWithDefinition replaces {variable} placeholders using definition-specific multi_instance_labels and automatic detection
func (dt *DefinitionBasedTransformer) replaceTemplateVariablesWithDefinition(template string, tags map[string]string, def MetricDefinition) string {
	result := template

	dt.moduleLogger.Debug().
		Str("template", template).
		Interface("multi_instance_labels", def.MultiInstanceLabels).
		Interface("tags", tags).
		Msg("Processing template with definition-specific labels")

	// Process multi_instance_labels: the metric's own list first, then
	// the definition-level defaults (e.g. snmp_poll declares
	// {instance} once for the whole file) — #317.
	labels := append(append([]string{}, def.MultiInstanceLabels...), dt.definition.MultiInstanceLabels...)
	for _, labelName := range labels {
		placeholder := fmt.Sprintf("{%s}", labelName)
		if strings.Contains(result, placeholder) {
			value := tags[labelName]

			if value != "" {
				result = strings.ReplaceAll(result, placeholder, value)
				dt.moduleLogger.Debug().
					Str("placeholder", placeholder).
					Str("value", value).
					Msg("Replaced template variable from multi_instance_labels")
			} else {
				dt.moduleLogger.Debug().
					Str("placeholder", placeholder).
					Str("label", labelName).
					Msg("Multi-instance label not found in tags")
			}
		}
	}

	// If we still have unresolved placeholders, try automatic index detection
	if strings.Contains(result, "{") {
		result = dt.replaceTemplateVariablesWithAutoDetection(result, tags)
	}

	// Then process any remaining placeholders with fallback logic
	result = dt.replaceTemplateVariables(result, tags)

	return result
}

// replaceTemplateVariablesWithAutoDetection automatically detects index tags and replaces placeholders
func (dt *DefinitionBasedTransformer) replaceTemplateVariablesWithAutoDetection(template string, tags map[string]string) string {
	result := template

	// Detect all index tags automatically
	indexTags := dt.detectIndexTags(tags)

	dt.moduleLogger.Debug().
		Interface("detected_index_tags", indexTags).
		Str("template", template).
		Msg("Auto-detected index tags for template replacement")

	// Replace all detected index tags in the template
	for tagKey, tagValue := range indexTags {
		placeholder := fmt.Sprintf("{%s}", tagKey)
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, tagValue)
			dt.moduleLogger.Debug().
				Str("placeholder", placeholder).
				Str("value", tagValue).
				Msg("Auto-replaced template variable")
		}
	}

	// Special handling for generic {instance} placeholder
	if strings.Contains(result, "{instance}") {
		// Try to find the best index tag for {instance}
		instanceValue := dt.findBestInstanceTag(indexTags)
		if instanceValue != "" {
			result = strings.ReplaceAll(result, "{instance}", instanceValue)
			dt.moduleLogger.Debug().
				Str("instance_value", instanceValue).
				Msg("Replaced generic {instance} placeholder")
		}
	}

	return result
}

// String Utility Helpers

// generateFallbackDisplayName creates a fallback display name when no definition matches
func (dt *DefinitionBasedTransformer) generateFallbackDisplayName(metricName string, tags map[string]string) string {
	// Try fallback patterns from templates config
	if len(dt.templatesConfig.FallbackPatterns) > 0 {
		// Use generic fallback pattern
		if pattern, exists := dt.templatesConfig.FallbackPatterns["generic"]; exists {
			return strings.ReplaceAll(pattern, "{metric_name}", dt.makeReadable(metricName))
		}
	}

	// Ultimate fallback: make the metric name readable
	return dt.makeReadable(metricName)
}

// makeReadable converts technical metric names to readable format
func (dt *DefinitionBasedTransformer) makeReadable(metricName string) string {
	// Remove probe-specific common prefixes that add no value
	readable := metricName

	// Redfish probe: remove "hardware." prefix (100% of metrics have it)
	readable = strings.TrimPrefix(readable, "hardware.")

	// Legacy prefixes for backwards compatibility
	legacyPrefixes := []string{"dell_powervault_me.", "thermal.", "power.", "storage."}
	for _, prefix := range legacyPrefixes {
		readable = strings.TrimPrefix(readable, prefix)
	}

	// Replace dots and underscores with spaces
	readable = strings.ReplaceAll(readable, ".", " ")
	readable = strings.ReplaceAll(readable, "_", " ")

	// Capitalize first letter of each word, with special cases
	words := strings.Fields(readable)
	for i, word := range words {
		if len(word) > 0 {
			// Special cases for technical terms
			if strings.ToLower(word) == "io" {
				words[i] = "IO"
			} else if strings.ToLower(word) == "iowait" {
				words[i] = "IO Wait"
			} else if strings.ToLower(word) == "cpu" {
				words[i] = "CPU"
			} else {
				words[i] = strings.ToUpper(word[:1]) + word[1:]
			}
		}
	}

	return strings.Join(words, " ")
}

// normalizeUnit converts unit aliases to standard units
func (dt *DefinitionBasedTransformer) normalizeUnit(unit string) string {
	// Check if unit needs normalization
	for _, unitDef := range dt.unitsConfig.Units {
		for _, alias := range unitDef.Aliases {
			if alias == unit {
				return unitDef.Standard
			}
		}
	}

	// Return as-is if no normalization needed
	return unit
}

// Index Tag Detection Helpers

// detectIndexTags automatically identifies which tags are likely to be index/identifier tags
func (dt *DefinitionBasedTransformer) detectIndexTags(tags map[string]string) map[string]string {
	indexTags := make(map[string]string)

	for key, value := range tags {
		if dt.isIndexTag(key, value) {
			indexTags[key] = value
		}
	}

	return indexTags
}

// isIndexTag determines if a tag key/value pair represents an index or identifier
func (dt *DefinitionBasedTransformer) isIndexTag(key, value string) bool {
	// Skip system/metadata tags
	systemTags := []string{"host", "os", "platform", "probe_name", "state", "manufacturer", "model", "serial_number"}
	for _, systemTag := range systemTags {
		if key == systemTag {
			return false
		}
	}

	// Skip very long values (probably not indexes)
	if len(value) > 50 {
		return false
	}

	// CPU probe patterns (OS-specific)
	if key == "core" && dt.isNumeric(value) {
		return true // Unix/macOS: core="0", "1", "2"
	}
	if key == "instance" && (dt.isNumeric(value) || value == "_Total") {
		return true // Windows: instance="0", "1", "_Total"
	}

	// Network probe patterns
	if key == "interface" && len(value) <= 20 {
		return true // interface="en0", "Wi-Fi", "Ethernet"
	}
	if key == "adapter" && len(value) <= 30 {
		return true // Windows adapter names
	}

	// Redfish probe patterns
	redfishIndexTags := []string{"controller_id", "drive_id", "system_id", "controller"}
	for _, redfishTag := range redfishIndexTags {
		if key == redfishTag {
			return true
		}
	}

	// Physical positions
	physicalTags := []string{"slot", "channel", "socket", "bay"}
	for _, physicalTag := range physicalTags {
		if key == physicalTag && dt.isNumeric(value) {
			return true
		}
	}

	// Memory-specific tags
	if key == "memory_controller" && dt.isNumeric(value) {
		return true
	}

	// Generic numeric values for potential indexes
	if dt.isNumeric(value) && len(value) <= 3 {
		// Short numeric values are likely indexes
		return true
	}

	// Single letter values (like controller letters A, B, C)
	if len(value) == 1 && dt.isAlpha(value) {
		return true
	}

	return false
}

// findBestInstanceTag finds the most appropriate tag value for a generic {instance} placeholder
func (dt *DefinitionBasedTransformer) findBestInstanceTag(indexTags map[string]string) string {
	// Preference order for {instance} replacement
	preferenceOrder := []string{"core", "instance", "interface", "controller_id", "drive_id", "slot", "channel"}

	for _, preferred := range preferenceOrder {
		if value, exists := indexTags[preferred]; exists {
			return value
		}
	}

	// If no preferred tag found, return the first available index tag
	for _, value := range indexTags {
		return value
	}

	return ""
}

// isNumeric checks if a string contains only digits
func (dt *DefinitionBasedTransformer) isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// isAlpha checks if a string contains only letters
func (dt *DefinitionBasedTransformer) isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, char := range s {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
			return false
		}
	}
	return true
}
