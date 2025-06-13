package redfish

import (
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/tags"
	"strings"
)

// addCollectionTag adds the collection tag to a tag slice for proper categorization
func addCollectionTag(baseTags []tags.Tag, collection string) []tags.Tag {
	return append(baseTags, tags.Tag{Key: "collection", Value: collection})
}

// containsIgnoreCase checks if a string contains a substring, ignoring case
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// mapHealthState converts health state string to integer for monitoring systems
func mapHealthState(health string) int {
	switch strings.ToLower(health) {
	case "ok":
		return 0 // OK
	case "warning":
		return 1 // Warning
	case "critical":
		return 2 // Critical
	default:
		return 3 // Unknown
	}
}

// mapPowerState converts power state string to integer for monitoring systems
func mapPowerState(state string) int {
	switch strings.ToLower(state) {
	case "on":
		return 1 // On
	case "off":
		return 0 // Off
	case "powering on":
		return 2 // Powering On
	case "powering off":
		return 3 // Powering Off
	default:
		return 4 // Unknown
	}
}

// createBaseSystemTags creates the base tags for system-level metrics
func (c *GenericCollector) createBaseSystemTags(resp *RedfishResponse, hostname string) []tags.Tag {
	systemTags := []tags.Tag{
		{Key: "endpoint", Value: hostname},
	}

	if resp.ID != "" {
		systemTags = append(systemTags, tags.Tag{Key: "system_id", Value: resp.ID})
	}
	if resp.Name != "" {
		systemTags = append(systemTags, tags.Tag{Key: "system_name", Value: resp.Name})
	}
	if resp.Manufacturer != "" {
		systemTags = append(systemTags, tags.Tag{Key: "manufacturer", Value: resp.Manufacturer})
	}
	if resp.Model != "" {
		systemTags = append(systemTags, tags.Tag{Key: "model", Value: resp.Model})
	}
	if resp.SerialNumber != "" {
		systemTags = append(systemTags, tags.Tag{Key: "serial_number", Value: resp.SerialNumber})
	}
	if resp.UUID != "" {
		systemTags = append(systemTags, tags.Tag{Key: "system_uuid", Value: resp.UUID})
	}
	if resp.SystemType != "" {
		systemTags = append(systemTags, tags.Tag{Key: "system_type", Value: resp.SystemType})
	}
	if resp.BiosVersion != "" {
		systemTags = append(systemTags, tags.Tag{Key: "bios_version", Value: resp.BiosVersion})
	}
	if resp.Status != nil && resp.Status.State != "" {
		systemTags = append(systemTags, tags.Tag{Key: "state", Value: resp.Status.State})
	}
	if resp.AssetTag != "" {
		systemTags = append(systemTags, tags.Tag{Key: "asset_tag", Value: resp.AssetTag})
	}

	return systemTags
}

// createChassisBaseTags creates common chassis tags from a chassis response
// This helper function extracts the duplicate chassis tag creation logic
func (c *GenericCollector) createChassisBaseTags(chassisResp *RedfishResponse) []tags.Tag {
	chassisTags := []tags.Tag{
		{Key: "chassis_id", Value: chassisResp.ID},
		{Key: "chassis_name", Value: chassisResp.Name},
	}

	// Add additional chassis tags according to REDFISH-TAGS.md
	// Extract ChassisType from Raw data if available
	var chassisRawData map[string]interface{}
	if err := json.Unmarshal(chassisResp.Raw, &chassisRawData); err == nil {
		if chassisType, ok := chassisRawData["ChassisType"].(string); ok && chassisType != "" {
			chassisTags = append(chassisTags, tags.Tag{Key: "chassis_type", Value: chassisType})
		}
	}
	if chassisResp.Manufacturer != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "manufacturer", Value: chassisResp.Manufacturer})
	}
	if chassisResp.Model != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "model", Value: chassisResp.Model})
	}
	if chassisResp.SerialNumber != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "serial_number", Value: chassisResp.SerialNumber})
	}
	if chassisResp.PartNumber != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "part_number", Value: chassisResp.PartNumber})
	}
	if chassisResp.AssetTag != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "asset_tag", Value: chassisResp.AssetTag})
	}
	if chassisResp.SKU != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "sku", Value: chassisResp.SKU})
	}
	if chassisResp.Status != nil && chassisResp.Status.State != "" {
		chassisTags = append(chassisTags, tags.Tag{Key: "state", Value: chassisResp.Status.State})
	}

	// Get physical dimensions if available
	var chassisPhysical struct {
		HeightMm float32 `json:"HeightMm"`
		WidthMm float32 `json:"WidthMm"`
		DepthMm float32 `json:"DepthMm"`
		WeightKg float32 `json:"WeightKg"`
		LocationIndicatorActive bool `json:"LocationIndicatorActive"`
	}
	rawJSON, _ := json.Marshal(chassisResp)
	if err := json.Unmarshal(rawJSON, &chassisPhysical); err == nil {
		if chassisPhysical.HeightMm > 0 {
			chassisTags = append(chassisTags, tags.Tag{Key: "height_mm", Value: fmt.Sprintf("%g", chassisPhysical.HeightMm)})
		}
		if chassisPhysical.WidthMm > 0 {
			chassisTags = append(chassisTags, tags.Tag{Key: "width_mm", Value: fmt.Sprintf("%g", chassisPhysical.WidthMm)})
		}
		if chassisPhysical.DepthMm > 0 {
			chassisTags = append(chassisTags, tags.Tag{Key: "depth_mm", Value: fmt.Sprintf("%g", chassisPhysical.DepthMm)})
		}
		if chassisPhysical.WeightKg > 0 {
			chassisTags = append(chassisTags, tags.Tag{Key: "weight_kg", Value: fmt.Sprintf("%g", chassisPhysical.WeightKg)})
		}
		chassisTags = append(chassisTags, tags.Tag{Key: "location_indicator", Value: fmt.Sprintf("%t", chassisPhysical.LocationIndicatorActive)})
	}

	return chassisTags
}