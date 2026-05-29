// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"senhub-agent.go/probesdk/tags"
	"strings"
)

// TagEnhancer provides utilities to improve and organize Redfish metric tags
type TagEnhancer struct{}

// NewTagEnhancer creates a new tag enhancer instance
func NewTagEnhancer() *TagEnhancer {
	return &TagEnhancer{}
}

// EnhanceMetricTags improves metric tags by adding collection and simplifying redundant tags
func (te *TagEnhancer) EnhanceMetricTags(metricName string, originalTags []tags.Tag) []tags.Tag {
	enhancedTags := make([]tags.Tag, 0, len(originalTags)+1)

	// Add collection tag based on metric name pattern
	collection := te.detectCollection(metricName)
	if collection != "" {
		enhancedTags = append(enhancedTags, tags.Tag{Key: "collection", Value: collection})
	}

	// Process and clean existing tags
	seenKeys := make(map[string]bool)

	for _, tag := range originalTags {
		// Skip redundant or confusing tags
		if te.shouldSkipTag(tag) {
			continue
		}

		// Simplify tag values
		simplifiedTag := te.simplifyTag(tag)

		// Handle duplicate keys - prefer the better value
		if seenKeys[simplifiedTag.Key] {
			// If we already have this key, check if the new value is better
			if te.isBetterTagValue(simplifiedTag.Key, simplifiedTag.Value, enhancedTags) {
				// Replace the existing tag with the better one
				te.replaceTagInSlice(enhancedTags, simplifiedTag)
			}
			continue
		}

		enhancedTags = append(enhancedTags, simplifiedTag)
		seenKeys[simplifiedTag.Key] = true
	}

	return enhancedTags
}

// detectCollection determines the appropriate collection based on metric name
func (te *TagEnhancer) detectCollection(metricName string) string {
	metricLower := strings.ToLower(metricName)

	switch {
	case strings.Contains(metricLower, "temperature") || strings.Contains(metricLower, "thermal"):
		return "thermal"
	case strings.Contains(metricLower, "fan") || strings.Contains(metricLower, "cooling"):
		return "thermal"
	case strings.Contains(metricLower, "power") || strings.Contains(metricLower, "psu"):
		return "power"
	case strings.Contains(metricLower, "drive") || strings.Contains(metricLower, "disk"):
		return "storage"
	case strings.Contains(metricLower, "pool") || strings.Contains(metricLower, "volume"):
		return "storage"
	case strings.Contains(metricLower, "controller") && strings.Contains(metricLower, "storage"):
		return "storage"
	case strings.Contains(metricLower, "memory") || strings.Contains(metricLower, "ram"):
		return "memory"
	case strings.Contains(metricLower, "cpu") || strings.Contains(metricLower, "processor"):
		return "processor"
	case strings.Contains(metricLower, "network") || strings.Contains(metricLower, "ethernet"):
		return "networkadapter"
	case strings.Contains(metricLower, "system") || strings.Contains(metricLower, "health"):
		return "system"
	default:
		return "system" // Default collection for general metrics
	}
}

// shouldSkipTag determines if a tag should be excluded from the enhanced tags
func (te *TagEnhancer) shouldSkipTag(tag tags.Tag) bool {
	// Always skip serial numbers (they clutter the interface and aren't useful for filtering)
	if tag.Key == "serial_number" {
		return true
	}

	// Skip empty values
	if tag.Value == "" {
		return true
	}

	// Skip internal/debugging tags
	if strings.HasPrefix(tag.Key, "_") || strings.HasPrefix(tag.Key, "debug_") {
		return true
	}

	// Skip ID-based tags in favor of name-based tags, but preserve important ones
	if strings.HasSuffix(tag.Key, "_id") {
		// Keep important IDs that are useful for filtering
		importantIDs := []string{"pool_id", "controller_id", "drive_id", "volume_id"}
		for _, importantID := range importantIDs {
			if tag.Key == importantID {
				return false // Don't skip these important IDs
			}
		}
		return true // Skip other *_id tags
	}

	// Skip technical/internal tags that aren't useful for user filtering
	technicalTags := []string{
		"location_ordinal", // Technical location identifier
		"system_id",        // Internal system ID (prefer system_name)
		"uuid",             // Technical UUID (not user-friendly)
	}
	for _, techTag := range technicalTags {
		if tag.Key == techTag {
			return true
		}
	}

	// Thermal metrics disabled - sensor_name filtering removed for consistency

	return false
}

// simplifyTag improves tag readability and consistency
func (te *TagEnhancer) simplifyTag(tag tags.Tag) tags.Tag {
	simplifiedTag := tag

	// Thermal metrics disabled - sensor_name handling removed for consistency
	// if tag.Key == "sensor_name" {
	//     // Sensor processing disabled for consistency across strategies
	// }

	// Simplify controller names and unify the key
	if tag.Key == "controller_name" || tag.Key == "controller_id" {
		simplifiedTag.Value = te.simplifyControllerName(tag.Value)
		simplifiedTag.Key = "controller"
	}

	// Simplify drive names
	if tag.Key == "drive_name" {
		simplifiedTag.Value = te.simplifyDriveName(tag.Value)
	}

	// Handle pool names - keep only pool_name, skip pool tag
	if tag.Key == "pool_name" {
		// Keep pool_name tag as-is, no transformation
		simplifiedTag = tag
	} else if tag.Key == "pool" {
		// Skip pool tag entirely - we only want pool_name
		// This will be filtered out by the shouldSkipTag logic
		simplifiedTag = tags.Tag{Key: "_skip_pool", Value: ""}
	} else if tag.Key == "description" && te.isPoolDescription(tag.Value) {
		// Convert pool description to pool_name tag
		simplifiedTag.Key = "pool_name"
		simplifiedTag.Value = te.extractPoolNameFromDescription(tag.Value)
	}

	// Normalize manufacturer names for consistency
	if tag.Key == "manufacturer" || tag.Key == "drive_manufacturer" {
		simplifiedTag.Value = te.normalizeManufacturer(tag.Value)
		// Unify manufacturer tag names
		simplifiedTag.Key = "manufacturer"
	}

	// Clean problematic characters from all tag values for URL compatibility
	simplifiedTag.Value = te.cleanTagValueForURL(simplifiedTag.Value)

	return simplifiedTag
}

// simplifyControllerName standardizes controller identifiers
func (te *TagEnhancer) simplifyControllerName(controllerName string) string {
	// Examples: "controller_a" -> "A", "Controller A" -> "A"
	controllerName = strings.ToLower(controllerName)
	controllerName = strings.TrimPrefix(controllerName, "controller")
	controllerName = strings.TrimPrefix(controllerName, "_")
	controllerName = strings.TrimSpace(controllerName)
	return strings.ToUpper(controllerName)
}

// simplifyDriveName creates more readable drive identifiers
func (te *TagEnhancer) simplifyDriveName(driveName string) string {
	// Examples: "0.11" -> "Drive 11", "Drive 0.5" -> "Drive 5"
	if strings.Contains(driveName, ".") {
		parts := strings.Split(driveName, ".")
		if len(parts) == 2 {
			return "Drive " + parts[1]
		}
	}
	return driveName
}

// GetRecommendedCollections returns the list of standard Redfish collections
func (te *TagEnhancer) GetRecommendedCollections() []string {
	return []string{
		"system",         // General system health and info
		"thermal",        // Temperatures, fans, cooling
		"power",          // Power supplies, consumption
		"processor",      // CPU hardware
		"memory",         // RAM hardware
		"storage",        // RAID controllers, pools, volumes
		"drives",         // Individual drives
		"networkadapter", // Network cards
	}
}

// GetCollectionDescription returns a human-readable description of each collection
func (te *TagEnhancer) GetCollectionDescription(collection string) string {
	descriptions := map[string]string{
		"system":         "General system health, power state, hardware info",
		"thermal":        "Temperature sensors, fans, cooling systems",
		"power":          "Power supplies, power consumption, PSU health",
		"processor":      "CPU hardware, processor health and summary",
		"memory":         "RAM hardware, memory capacity and health",
		"storage":        "RAID controllers, storage pools, volumes",
		"drives":         "Individual drive health, capacity, operations",
		"networkadapter": "Network interface cards and connectivity",
	}

	if desc, exists := descriptions[collection]; exists {
		return desc
	}
	return "Redfish hardware metrics"
}

// isPoolDescription checks if a description contains pool information
func (te *TagEnhancer) isPoolDescription(description string) bool {
	descLower := strings.ToLower(description)

	// Common pool indicators in descriptions
	poolIndicators := []string{
		"pool",
		"raid group",
		"volume group",
		"disk group",
		"storage pool",
	}

	for _, indicator := range poolIndicators {
		if strings.Contains(descLower, indicator) {
			return true
		}
	}

	// Check for patterns like "dgA01", "Pool A", etc.
	if strings.HasPrefix(descLower, "dg") || strings.Contains(descLower, "pool") {
		return true
	}

	return false
}

// extractPoolNameFromDescription extracts pool name from description field
func (te *TagEnhancer) extractPoolNameFromDescription(description string) string {
	descLower := strings.ToLower(description)

	// Try to extract pool name from various patterns

	// Pattern: "Pool A" -> "Pool A"
	if strings.Contains(descLower, "pool ") {
		// Extract the part after "pool "
		parts := strings.Fields(description)
		for i, part := range parts {
			if strings.ToLower(part) == "pool" && i+1 < len(parts) {
				return "Pool " + strings.ToUpper(parts[i+1])
			}
		}
	}

	// Pattern: "dgA01" -> "Pool A"
	if strings.HasPrefix(descLower, "dg") && len(description) > 2 {
		letter := string(description[2])
		if letter >= "A" && letter <= "Z" || letter >= "a" && letter <= "z" {
			return "Pool " + strings.ToUpper(letter)
		}
	}

	// Pattern: "RAID Group 1" -> "Pool 1"
	if strings.Contains(descLower, "raid group") {
		parts := strings.Fields(description)
		for i, part := range parts {
			if strings.ToLower(part) == "group" && i+1 < len(parts) {
				return "Pool " + parts[i+1]
			}
		}
	}

	// Fallback: use original description but clean it up
	// Note: Using simple capitalization instead of deprecated strings.Title
	desc := strings.ToLower(description)
	if len(desc) > 0 {
		return strings.ToUpper(string(desc[0])) + desc[1:]
	}
	return desc
}

// isBetterTagValue determines if a new tag value is better than the existing one
func (te *TagEnhancer) isBetterTagValue(key, newValue string, existingTags []tags.Tag) bool {
	// Find the existing tag
	for _, tag := range existingTags {
		if tag.Key == key {
			existingValue := tag.Value

			// For endpoint tags, prefer hostname over full URL for consistency
			if key == "endpoint" {
				// Prefer shorter, cleaner hostname over full URL
				if te.isHostname(newValue) && te.isFullURL(existingValue) {
					return true
				}
				if te.isFullURL(newValue) && te.isHostname(existingValue) {
					return false
				}
			}

			// For manufacturer tags, prefer the normalized value
			if key == "manufacturer" {
				normalizedNew := te.normalizeManufacturer(newValue)
				normalizedExisting := te.normalizeManufacturer(existingValue)
				// If they normalize to the same value, prefer the cleaner one
				if normalizedNew == normalizedExisting {
					return len(newValue) < len(existingValue)
				}
			}

			// For names vs IDs, prefer names
			if strings.Contains(existingValue, "_id") && !strings.Contains(newValue, "_id") {
				return true
			}

			// Prefer shorter, cleaner values
			if len(newValue) < len(existingValue) && len(newValue) > 0 {
				return true
			}

			break
		}
	}

	return false
}

// replaceTagInSlice replaces an existing tag in the slice
func (te *TagEnhancer) replaceTagInSlice(tags []tags.Tag, newTag tags.Tag) {
	for i, tag := range tags {
		if tag.Key == newTag.Key {
			tags[i] = newTag
			break
		}
	}
}

// isHostname checks if a value looks like a hostname
func (te *TagEnhancer) isHostname(value string) bool {
	// Simple check: no protocol, no path
	return !strings.Contains(value, "://") && !strings.Contains(value, "/")
}

// isFullURL checks if a value looks like a full URL
func (te *TagEnhancer) isFullURL(value string) bool {
	return strings.Contains(value, "://")
}

// normalizeManufacturer standardizes manufacturer names for consistency
func (te *TagEnhancer) normalizeManufacturer(manufacturer string) string {
	// Convert to lowercase for comparison
	mfr := strings.ToLower(strings.TrimSpace(manufacturer))

	// Normalize common manufacturer variations
	switch {
	case strings.Contains(mfr, "dell"):
		return "Dell"
	case strings.Contains(mfr, "hewlett") || strings.Contains(mfr, "hpe") || mfr == "hp":
		return "HPE"
	case strings.Contains(mfr, "lenovo"):
		return "Lenovo"
	case strings.Contains(mfr, "cisco"):
		return "Cisco"
	case strings.Contains(mfr, "supermicro") || strings.Contains(mfr, "super micro"):
		return "Supermicro"
	case strings.Contains(mfr, "fujitsu"):
		return "Fujitsu"
	case strings.Contains(mfr, "huawei"):
		return "Huawei"
	case strings.Contains(mfr, "intel"):
		return "Intel"
	case strings.Contains(mfr, "amd"):
		return "AMD"
	case strings.Contains(mfr, "nvidia"):
		return "NVIDIA"
	case strings.Contains(mfr, "seagate"):
		return "Seagate"
	case strings.Contains(mfr, "western digital") || strings.Contains(mfr, "wd"):
		return "Western Digital"
	case strings.Contains(mfr, "samsung"):
		return "Samsung"
	case strings.Contains(mfr, "micron"):
		return "Micron"
	case strings.Contains(mfr, "kingston"):
		return "Kingston"
	default:
		// If no match, return the original but properly capitalized
		// Note: Using simple capitalization instead of deprecated strings.Title
		mfr := strings.ToLower(manufacturer)
		if len(mfr) > 0 {
			return strings.ToUpper(string(mfr[0])) + mfr[1:]
		}
		return mfr
	}
}

// cleanTagValueForURL removes or replaces characters that are problematic in URLs
func (te *TagEnhancer) cleanTagValueForURL(value string) string {
	// List of problematic characters to remove from tag values
	// These cause issues with URL encoding/parsing
	problematicChars := []string{
		",", // Comma - often used as separator in URL params
		";", // Semicolon - can be interpreted as parameter separator
		"(", // Parentheses - can cause parsing issues
		")",
		"[", // Brackets - can interfere with array notation
		"]",
		"{", // Braces - can interfere with template syntax
		"}",
		"<", // Angle brackets - can be interpreted as HTML
		">",
		"|",  // Pipe - often used as separator
		"\\", // Backslash - escape character issues
		"\"", // Quotes - can break string parsing
		"'",
		"`", // Backtick - template literal issues
		"#", // Hash - URL fragment identifier
		"&", // Ampersand - URL parameter separator
		"?", // Question mark - URL query start
		"=", // Equals - URL parameter assignment
	}

	cleanValue := value
	for _, char := range problematicChars {
		cleanValue = strings.ReplaceAll(cleanValue, char, "")
	}

	// Replace multiple spaces with single space
	cleanValue = strings.Join(strings.Fields(cleanValue), " ")

	// Trim spaces
	cleanValue = strings.TrimSpace(cleanValue)

	return cleanValue
}
