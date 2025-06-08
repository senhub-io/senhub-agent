// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"strings"
	"senhub-agent.go/internal/agent/tags"
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
	
	// Skip ID-based tags in favor of name-based tags
	if strings.HasSuffix(tag.Key, "_id") {
		return true
	}
	
	// Skip technical/internal tags that aren't useful for user filtering
	technicalTags := []string{
		"location_ordinal",    // Technical location identifier
		"system_id",          // Internal system ID (prefer system_name)
		"uuid",               // Technical UUID (not user-friendly)
		"metric_category",    // Internal classification metadata
		"metric_severity",    // Internal classification metadata  
		"metric_unit",        // Internal classification metadata
	}
	for _, techTag := range technicalTags {
		if tag.Key == techTag {
			return true
		}
	}
	
	// Optionally skip sensor_name if they're too complex for filtering
	// This can be enabled if sensor names are not useful for end-user filtering
	if tag.Key == "sensor_name" && te.isSensorNameTooComplex(tag.Value) {
		return true
	}
	
	return false
}

// simplifyTag improves tag readability and consistency
func (te *TagEnhancer) simplifyTag(tag tags.Tag) tags.Tag {
	simplifiedTag := tag
	
	// Handle sensor_name - keep the simplified name for better readability but don't expose the raw complex name
	if tag.Key == "sensor_name" {
		simplifiedTag.Value = te.simplifySensorName(tag.Value)
	}
	
	// Simplify controller names and unify the key
	if tag.Key == "controller_name" || tag.Key == "controller_id" {
		simplifiedTag.Value = te.simplifyControllerName(tag.Value)
		simplifiedTag.Key = "controller"
	}
	
	// Simplify drive names
	if tag.Key == "drive_name" {
		simplifiedTag.Value = te.simplifyDriveName(tag.Value)
	}
	
	// Handle pool names - check both pool_name and description for pool information
	if tag.Key == "pool_name" || tag.Key == "description" {
		// If it's a pool-related description, extract pool name
		if tag.Key == "description" && te.isPoolDescription(tag.Value) {
			simplifiedTag.Key = "pool_name"
			simplifiedTag.Value = te.extractPoolNameFromDescription(tag.Value)
		} else if tag.Key == "pool_name" {
			simplifiedTag.Value = te.simplifyPoolName(tag.Value)
		}
	}
	
	// Normalize manufacturer names for consistency
	if tag.Key == "manufacturer" || tag.Key == "drive_manufacturer" {
		simplifiedTag.Value = te.normalizeManufacturer(tag.Value)
		// Unify manufacturer tag names
		simplifiedTag.Key = "manufacturer"
	}
	
	return simplifiedTag
}

// simplifySensorName extracts meaningful information from complex sensor names
func (te *TagEnhancer) simplifySensorName(sensorName string) string {
	// Examples: "sensor_temp_ctrl_A.4" -> "Controller A Sensor 4"
	//          "sensor_temp_iom_0.A.1" -> "IOM A Sensor 1"
	//          "sensor_temp_psu_0.0.0" -> "PSU 0 Sensor 0"
	
	if strings.HasPrefix(sensorName, "sensor_temp_") {
		parts := strings.Split(strings.TrimPrefix(sensorName, "sensor_temp_"), ".")
		if len(parts) >= 2 {
			component := parts[0]
			sensorNum := parts[len(parts)-1]
			
			// Beautify component names
			switch {
			case strings.HasPrefix(component, "ctrl_"):
				controller := strings.TrimPrefix(component, "ctrl_")
				return "Controller " + strings.ToUpper(controller) + " Sensor " + sensorNum
			case strings.HasPrefix(component, "iom_"):
				iomParts := strings.Split(strings.TrimPrefix(component, "iom_"), "_")
				if len(iomParts) >= 2 {
					return "IOM " + strings.ToUpper(iomParts[1]) + " Sensor " + sensorNum
				}
				return "IOM Sensor " + sensorNum
			case strings.HasPrefix(component, "psu_"):
				psuParts := strings.Split(strings.TrimPrefix(component, "psu_"), "_")
				if len(psuParts) >= 2 {
					return "PSU " + psuParts[1] + " Sensor " + sensorNum
				}
				return "PSU Sensor " + sensorNum
			}
		}
	}
	
	// Fallback: return original if no pattern matches
	return sensorName
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

// simplifyPoolName creates cleaner pool identifiers
func (te *TagEnhancer) simplifyPoolName(poolName string) string {
	// Examples: "dgA01" -> "Pool A", "A" -> "Pool A"
	if len(poolName) == 1 && poolName >= "A" && poolName <= "Z" {
		return "Pool " + poolName
	}
	if strings.HasPrefix(strings.ToLower(poolName), "dg") && len(poolName) > 2 {
		// Extract letter after "dg"
		letter := string(poolName[2])
		if letter >= "A" && letter <= "Z" || letter >= "a" && letter <= "z" {
			return "Pool " + strings.ToUpper(letter)
		}
	}
	return poolName
}

// GetRecommendedCollections returns the list of standard Redfish collections
func (te *TagEnhancer) GetRecommendedCollections() []string {
	return []string{
		"system",        // General system health and info
		"thermal",       // Temperatures, fans, cooling
		"power",         // Power supplies, consumption
		"processor",     // CPU hardware
		"memory",        // RAM hardware
		"storage",       // RAID controllers, pools, volumes
		"drives",        // Individual drives
		"networkadapter", // Network cards
	}
}

// GetCollectionDescription returns a human-readable description of each collection
func (te *TagEnhancer) GetCollectionDescription(collection string) string {
	descriptions := map[string]string{
		"system":        "General system health, power state, hardware info",
		"thermal":       "Temperature sensors, fans, cooling systems",
		"power":         "Power supplies, power consumption, PSU health",
		"processor":     "CPU hardware, processor health and summary",
		"memory":        "RAM hardware, memory capacity and health",
		"storage":       "RAID controllers, storage pools, volumes",
		"drives":        "Individual drive health, capacity, operations",
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
	return strings.Title(strings.ToLower(description))
}

// isSensorNameTooComplex determines if a sensor name is too complex for user filtering
func (te *TagEnhancer) isSensorNameTooComplex(sensorName string) bool {
	// Skip very long sensor names
	if len(sensorName) > 50 {
		return true
	}
	
	// Skip sensor names with multiple dots or complex IDs
	dotCount := strings.Count(sensorName, ".")
	if dotCount > 2 {
		return true
	}
	
	// Skip sensor names that look like internal IDs rather than user-friendly names
	if strings.Contains(sensorName, "_temp_") || strings.Contains(sensorName, "_sensor_") {
		return true
	}
	
	// Keep sensor names that are already user-friendly
	return false
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
		return strings.Title(strings.ToLower(manufacturer))
	}
}

