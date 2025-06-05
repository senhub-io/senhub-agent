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
		
		// Avoid duplicate keys (keep first occurrence)
		if !seenKeys[simplifiedTag.Key] {
			enhancedTags = append(enhancedTags, simplifiedTag)
			seenKeys[simplifiedTag.Key] = true
		}
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
	// Skip very long serial numbers that clutter the interface
	if tag.Key == "serial_number" && len(tag.Value) > 20 {
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
	
	return false
}

// simplifyTag improves tag readability and consistency
func (te *TagEnhancer) simplifyTag(tag tags.Tag) tags.Tag {
	simplifiedTag := tag
	
	// Simplify sensor names - extract meaningful parts
	if tag.Key == "sensor_name" {
		simplifiedTag.Value = te.simplifySensorName(tag.Value)
	}
	
	// Simplify controller names
	if tag.Key == "controller_name" || tag.Key == "controller_id" {
		simplifiedTag.Value = te.simplifyControllerName(tag.Value)
		// Unify controller-related tag keys
		simplifiedTag.Key = "controller"
	}
	
	// Simplify drive names - use location_ordinal if available in context
	if tag.Key == "drive_name" {
		simplifiedTag.Value = te.simplifyDriveName(tag.Value)
	}
	
	// Simplify pool names
	if tag.Key == "pool_name" {
		simplifiedTag.Value = te.simplifyPoolName(tag.Value)
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