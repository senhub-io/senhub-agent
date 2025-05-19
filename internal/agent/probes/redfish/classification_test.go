package redfish

import (
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClassificationTags(t *testing.T) {
	t.Run("HealthMetricsClassification", func(t *testing.T) {
		// Create test metric for a drive health status
		dp := &datapoint.DataPoint{
			Name:      "hardware.storage.drive.health",
			Value:     2.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "drive_id", Value: "0:1"},
				{Key: "drive_name", Value: "Drive 1"},
				{Key: "media_type", Value: "HDD"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify original tags are preserved
		assert.Equal(t, "0:1", getTagValue(dp.Tags, "drive_id"))
		assert.Equal(t, "Drive 1", getTagValue(dp.Tags, "drive_name"))
		assert.Equal(t, "HDD", getTagValue(dp.Tags, "media_type"))

		// Verify classification tags were added correctly
		assert.Equal(t, CategoryHealth, getTagValue(dp.Tags, TagKeyCategory))
		assert.Equal(t, ComponentDrive, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionStorage, getTagValue(dp.Tags, TagKeySection))
		assert.Equal(t, SectionOverview, getTagValue(dp.Tags, TagKeyDashboard))
	})

	t.Run("CapacityMetricsClassification", func(t *testing.T) {
		// Create test metric for volume capacity
		dp := &datapoint.DataPoint{
			Name:      "hardware.storage.volume.capacity.used_percent",
			Value:     85.5,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "volume_id", Value: "volume-1"},
				{Key: "volume_name", Value: "DataVolume"},
				{Key: "raid_type", Value: "RAID5"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify original tags are preserved
		assert.Equal(t, "volume-1", getTagValue(dp.Tags, "volume_id"))
		assert.Equal(t, "DataVolume", getTagValue(dp.Tags, "volume_name"))
		assert.Equal(t, "RAID5", getTagValue(dp.Tags, "raid_type"))

		// Verify classification tags were added correctly
		assert.Equal(t, CategoryCapacity, getTagValue(dp.Tags, TagKeyCategory))
		assert.Equal(t, ComponentVolume, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionStorage, getTagValue(dp.Tags, TagKeySection))
		assert.Equal(t, SectionOverview, getTagValue(dp.Tags, TagKeyDashboard))
	})

	t.Run("PerformanceMetricsClassification", func(t *testing.T) {
		// Create test metric for IO performance
		dp := &datapoint.DataPoint{
			Name:      "hardware.storage.volume.io.read.latency",
			Value:     12.3,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "volume_id", Value: "volume-1"},
				{Key: "volume_name", Value: "DataVolume"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, CategoryPerformance, getTagValue(dp.Tags, TagKeyCategory))
		assert.Equal(t, ComponentVolume, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionStorage, getTagValue(dp.Tags, TagKeySection))
		assert.Equal(t, SectionOverview, getTagValue(dp.Tags, TagKeyDashboard))
	})

	t.Run("OperationsMetricsClassification", func(t *testing.T) {
		// Create test metric for disk operations
		dp := &datapoint.DataPoint{
			Name:      "hardware.storage.drive.operation.progress",
			Value:     45.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "drive_id", Value: "0:2"},
				{Key: "drive_name", Value: "Drive 2"},
				{Key: "operation_name", Value: "Rebuild"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, CategoryOperations, getTagValue(dp.Tags, TagKeyCategory))
		assert.Equal(t, ComponentDrive, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionStorage, getTagValue(dp.Tags, TagKeySection))
		// Operations should not be in the overview dashboard
		assert.Empty(t, getTagValue(dp.Tags, TagKeyDashboard))
	})

	t.Run("EventsMetricsClassification", func(t *testing.T) {
		// Create test metric for system logs
		dp := &datapoint.DataPoint{
			Name:      "hardware.logs.entries.critical",
			Value:     3.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "host", Value: "server-1"},
				{Key: "log_service_id", Value: "Log1"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, CategoryEvents, getTagValue(dp.Tags, TagKeyCategory))
		assert.Equal(t, ComponentLogs, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionEvents, getTagValue(dp.Tags, TagKeySection))
		assert.Equal(t, SectionOverview, getTagValue(dp.Tags, TagKeyDashboard))
	})

	t.Run("ThermalMetricsClassification", func(t *testing.T) {
		// Create test metric for thermal data
		dp := &datapoint.DataPoint{
			Name:      "hardware.thermal.temperature",
			Value:     45.6,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "sensor_name", Value: "CPU1"},
				{Key: "physical_context", Value: "CPU"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, ComponentThermal, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionHardware, getTagValue(dp.Tags, TagKeySection))
	})

	t.Run("PowerMetricsClassification", func(t *testing.T) {
		// Create test metric for power data
		dp := &datapoint.DataPoint{
			Name:      "hardware.power.consumption",
			Value:     350.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "psu_name", Value: "PSU1"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, ComponentPower, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionHardware, getTagValue(dp.Tags, TagKeySection))
	})

	t.Run("NetworkMetricsClassification", func(t *testing.T) {
		// Create test metric for network data
		dp := &datapoint.DataPoint{
			Name:      "hardware.network.interface.status",
			Value:     2.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "interface_id", Value: "eth0"},
			},
		}

		// Add classification tags
		AddClassificationTags(dp)

		// Verify classification tags were added correctly
		assert.Equal(t, ComponentNetwork, getTagValue(dp.Tags, TagKeyComponent))
		assert.Equal(t, SectionHardware, getTagValue(dp.Tags, TagKeySection))
	})
}

// Helper function to get tag value by key
func getTagValue(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}