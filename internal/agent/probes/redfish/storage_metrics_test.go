package redfish

import (
	"context"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"os"
)

func TestStorageMetricsCollection(t *testing.T) {
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*logger.Logger)(&testLogger)
	testTimestamp := time.Now()

	// Create test context
	ctx := context.Background()

	t.Run("CollectPoolMetrics", func(t *testing.T) {
		// Create mock client
		mockClient := NewMockRedfishClient()

		// Mock response for system info
		mockClient.AddMockResponse("Systems/1", `{
			"Name": "TestSystem", 
			"Id": "1"
		}`)

		// Mock response for a storage pool
		poolResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A",
			"Id": "A",
			"Name": "Pool A",
			"Status": {"Health": "OK", "State": "Enabled"},
			"CapacityBytes": 1099511627776,
			"RemainingCapacityPercent": 85,
			"Description": "Main storage pool",
			"SupportedRAIDTypes": ["RAID0", "RAID1", "RAID5"],
			"MaxBlockSizeBytes": 512,
			"Capacity": {
				"Data": {
					"AllocatedBytes": 1099511627776,
					"ConsumedBytes": 164926744166,
					"VolumesAllocatedBytes": 164926744166,
					"UnusedBytes": 934584883610
				},
				"IsThinProvisioned": true
			},
			"IOStatistics": {
				"ReadHitIORequests": 100000,
				"ReadIOKiBytes": 204800,
				"WriteHitIORequests": 50000,
				"WriteIOKiBytes": 102400
			}
		}`
		mockClient.AddMockResponse("Storage/controller_a/StoragePools/A", poolResponse)

		// Mock response for controller info (required for manufacturer/model tags)
		controllerResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a",
			"Id": "controller_a",
			"Name": "Controller A",
			"StorageControllers": [{
				"Manufacturer": "Dell",
				"Model": "PowerVault",
				"SerialNumber": "ABC123"
			}]
		}`
		mockClient.AddMockResponse("Storage/controller_a", controllerResponse)

		// Create collector
		collector, err := NewStorageCollector("https://redfish.example.com", "admin", "password", loggerPtr, false)
		assert.NoError(t, err)
		storageCollector := collector.(*StorageCollector)

		// Replace client with mock
		storageCollector.GenericCollector.client = mockClient
		storageCollector.GenericCollector.systems = []string{"Systems/1"}

		// Add a pool to the collector
		storageCollector.storagePools = []string{"Storage/controller_a/StoragePools/A"}

		// Set controller path for the test
		storageCollector.storageControllers = []string{"Storage/controller_a"}

		// Collect pool metrics
		metrics, err := storageCollector.collectPoolMetrics(ctx, testTimestamp)
		assert.NoError(t, err)
		assert.NotEmpty(t, metrics)

		// Verify metrics
		metricNames := make(map[string]bool)
		for _, metric := range metrics {
			metricNames[metric.Name] = true

			// Verify host tag is set
			hasHost := false
			for _, tag := range metric.Tags {
				if tag.Key == "host" && tag.Value == "TestSystem" {
					hasHost = true
					break
				}
			}
			assert.True(t, hasHost, "Host tag should be set on all metrics")

			// Verify pool-specific tags are set
			hasPool := false
			hasController := false
			hasDescription := false
			for _, tag := range metric.Tags {
				if tag.Key == "pool_name" && tag.Value == "Pool A" {
					hasPool = true
				}
				if tag.Key == "controller" && tag.Value == "A" {
					hasController = true
				}
				if tag.Key == "description" && tag.Value == "Main storage pool" {
					hasDescription = true
				}
			}
			assert.True(t, hasPool, "pool_name tag should be set on pool metrics")
			assert.True(t, hasController, "controller tag should be set on pool metrics")
			assert.True(t, hasDescription, "description tag should be set on pool metrics")

			// Check timestamp is correctly set
			assert.Equal(t, testTimestamp, metric.Timestamp)
		}

		// Check all expected metrics are present
		assert.True(t, metricNames["hardware.storage.pool.health"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.total"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.allocated"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.allocated_percent"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.used"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.used_percent"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.free_percent"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.free"])
		assert.True(t, metricNames["hardware.storage.pool.capacity.volumes"])
		assert.True(t, metricNames["hardware.storage.pool.io.reads"])
		assert.True(t, metricNames["hardware.storage.pool.io.writes"])
		assert.True(t, metricNames["hardware.storage.pool.io.read.bytes"])
		assert.True(t, metricNames["hardware.storage.pool.io.write.bytes"])
	})

	t.Run("CollectDriveMetrics", func(t *testing.T) {
		// Create mock client
		mockClient := NewMockRedfishClient()

		// Mock response for system info
		mockClient.AddMockResponse("Systems/1", `{
			"Name": "TestSystem", 
			"Id": "1"
		}`)

		// Mock response for controller
		controllerResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a",
			"Id": "controller_a",
			"Name": "Storage Controller A",
			"StorageControllers": [{
				"Manufacturer": "Dell",
				"Model": "PowerVault",
				"SerialNumber": "ABC123"
			}]
		}`
		mockClient.AddMockResponse("Storage/controller_a", controllerResponse)

		// Mock response for drives collection
		drivesResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Drives",
			"Members": [
				{"@odata.id": "/redfish/v1/Storage/controller_a/Drives/0"},
				{"@odata.id": "/redfish/v1/Storage/controller_a/Drives/1"}
			]
		}`
		mockClient.AddMockResponse("Storage/controller_a/Drives", drivesResponse)

		// Mock response for first drive (normal)
		drive0Response := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Drives/0",
			"Id": "0",
			"Name": "Drive 0",
			"Model": "SSD Model X",
			"Manufacturer": "Storage Corp",
			"SerialNumber": "ABCD1234",
			"MediaType": "SSD",
			"Protocol": "SAS",
			"CapacityBytes": 1000000000000,
			"FailurePredicted": false,
			"HotspareType": "None",
			"PhysicalLocation": {
				"PartLocation": {
					"LocationType": "Bay",
					"ServiceLabel": "Bay 0",
					"LocationOrdinalValue": 0
				}
			},
			"Status": {
				"Health": "OK",
				"State": "Enabled"
			}
		}`
		mockClient.AddMockResponse("Storage/controller_a/Drives/0", drive0Response)

		// Mock response for second drive (with operations)
		drive1Response := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Drives/1",
			"Id": "1",
			"Name": "Drive 1",
			"Model": "SSD Model X",
			"Manufacturer": "Storage Corp",
			"SerialNumber": "EFGH5678",
			"MediaType": "SSD",
			"Protocol": "SAS",
			"CapacityBytes": 1000000000000,
			"FailurePredicted": true,
			"HotspareType": "Global",
			"PhysicalLocation": {
				"PartLocation": {
					"LocationType": "Bay",
					"ServiceLabel": "Bay 1",
					"LocationOrdinalValue": 1
				}
			},
			"Status": {
				"Health": "Warning",
				"State": "Enabled"
			},
			"Operations": [
				{
					"OperationName": "Rebuild",
					"PercentageComplete": 45,
					"AssociatedTask": {
						"@odata.id": "/redfish/v1/TaskService/Tasks/1"
					}
				}
			]
		}`
		mockClient.AddMockResponse("Storage/controller_a/Drives/1", drive1Response)

		// Create collector
		collector, err := NewStorageCollector("https://redfish.example.com", "admin", "password", loggerPtr, false)
		assert.NoError(t, err)
		storageCollector := collector.(*StorageCollector)

		// Replace client with mock
		storageCollector.GenericCollector.client = mockClient
		storageCollector.GenericCollector.systems = []string{"Systems/1"}

		// Set controller for the test
		storageCollector.storageControllers = []string{"Storage/controller_a"}

		// Collect drive metrics
		metrics, err := storageCollector.collectDriveMetrics(ctx, testTimestamp)
		assert.NoError(t, err)
		assert.NotEmpty(t, metrics)

		// Separate metrics by drive ID for easier testing
		drive0Metrics := make(map[string]data_store.DataPoint)
		drive1Metrics := make(map[string]data_store.DataPoint)

		for _, metric := range metrics {
			var driveID string
			for _, tag := range metric.Tags {
				if tag.Key == "drive_id" {
					driveID = tag.Value
					break
				}
			}

			if driveID == "0" {
				drive0Metrics[metric.Name] = metric
			} else if driveID == "1" {
				drive1Metrics[metric.Name] = metric
			}
		}

		// Check Drive 0 metrics (regular drive, no operations)
		assert.Contains(t, drive0Metrics, "hardware.storage.drive.health")
		assert.Contains(t, drive0Metrics, "hardware.storage.drive.capacity.total")
		assert.Contains(t, drive0Metrics, "hardware.storage.drive.failure_predicted")
		assert.Contains(t, drive0Metrics, "hardware.storage.drive.has_operations")
		assert.Equal(t, float32(0.0), drive0Metrics["hardware.storage.drive.has_operations"].Value, "Drive 0 should have no operations")
		assert.Equal(t, float32(0.0), drive0Metrics["hardware.storage.drive.failure_predicted"].Value, "Drive 0 should not have failure predicted")
		assert.Equal(t, float32(0.0), drive0Metrics["hardware.storage.drive.health"].Value, "Drive 0 should have OK health")

		// Check Drive 1 metrics (drive with operations)
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.health")
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.capacity.total")
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.failure_predicted")
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.has_operations")
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.operation.progress")
		assert.Contains(t, drive1Metrics, "hardware.storage.drive.hotspare")
		assert.Equal(t, float32(1.0), drive1Metrics["hardware.storage.drive.has_operations"].Value, "Drive 1 should have operations")
		assert.Equal(t, float32(1.0), drive1Metrics["hardware.storage.drive.failure_predicted"].Value, "Drive 1 should have failure predicted")
		assert.Equal(t, float32(1.0), drive1Metrics["hardware.storage.drive.health"].Value, "Drive 1 should have Warning health")
		assert.Equal(t, float32(45.0), drive1Metrics["hardware.storage.drive.operation.progress"].Value, "Drive 1 operation should be 45% complete")
		assert.Equal(t, float32(1.0), drive1Metrics["hardware.storage.drive.hotspare"].Value, "Drive 1 should be a hotspare")
	})

	t.Run("CollectVolumeMetrics", func(t *testing.T) {
		// Create mock client
		mockClient := NewMockRedfishClient()

		// Mock response for system info
		mockClient.AddMockResponse("Systems/1", `{
			"Name": "TestSystem", 
			"Id": "1"
		}`)

		// Mock response for controller
		controllerResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a",
			"Id": "controller_a",
			"Name": "Storage Controller A",
			"StorageControllers": [{
				"Manufacturer": "Dell",
				"Model": "PowerVault",
				"SerialNumber": "ABC123"
			}]
		}`
		mockClient.AddMockResponse("Storage/controller_a", controllerResponse)

		// Mock response for volumes collection
		volumesResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Volumes",
			"Members": [
				{"@odata.id": "/redfish/v1/Storage/controller_a/Volumes/1"}
			]
		}`
		mockClient.AddMockResponse("Storage/controller_a/Volumes", volumesResponse)

		// Mock response for volume
		volumeResponse := `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Volumes/1",
			"Id": "1",
			"Name": "Volume 1",
			"CapacityBytes": 500000000000,
			"RAIDType": "RAID5",
			"Status": {
				"Health": "OK",
				"State": "Enabled"
			},
			"VolumeType": "StripedWithParity",
			"Encrypted": true,
			"BlockSizeBytes": 512,
			"AccessCapabilities": ["Read", "Write"],
			"EncryptionTypes": ["NativeDriveEncryption"],
			"StripSizeBytes": 262144,
			"RemainingCapacityPercent": 75,
			"Capacity": {
				"Data": {
					"AllocatedBytes": 500000000000,
					"ConsumedBytes": 125000000000
				}
			},
			"IOStatistics": {
				"ReadHitIORequests": 50000,
				"ReadIOKiBytes": 102400,
				"WriteHitIORequests": 30000,
				"WriteIOKiBytes": 61440,
				"ReadIORequestTime": "10.5",
				"WriteIORequestTime": "5.3"
			},
			"CapacitySources": [
				{
					"ProvidingPools": {
						"Members": [
							{"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A"}
						]
					}
				}
			]
		}`
		mockClient.AddMockResponse("Storage/controller_a/Volumes/1", volumeResponse)

		// Create collector
		collector, err := NewStorageCollector("https://redfish.example.com", "admin", "password", loggerPtr, false)
		assert.NoError(t, err)
		storageCollector := collector.(*StorageCollector)

		// Replace client with mock
		storageCollector.GenericCollector.client = mockClient
		storageCollector.GenericCollector.systems = []string{"Systems/1"}

		// Set controller for the test
		storageCollector.storageControllers = []string{"Storage/controller_a"}

		// Collect volume metrics
		metrics, err := storageCollector.collectVolumeConsumptionMetrics(ctx, testTimestamp)
		assert.NoError(t, err)
		assert.NotEmpty(t, metrics)

		// Verify metrics
		metricNames := make(map[string]bool)
		for _, metric := range metrics {
			metricNames[metric.Name] = true

			// Verify host tag is set
			hasHost := false
			hasVolumeID := false
			hasVolumeName := false
			hasPoolID := false
			hasRAIDType := false

			for _, tag := range metric.Tags {
				if tag.Key == "host" && tag.Value == "TestSystem" {
					hasHost = true
				}
				if tag.Key == "volume_id" && tag.Value == "1" {
					hasVolumeID = true
				}
				if tag.Key == "volume_name" && tag.Value == "Volume 1" {
					hasVolumeName = true
				}
				if tag.Key == "pool_name" && tag.Value == "A" {
					hasPoolID = true
				}
				if tag.Key == "raid_type" && tag.Value == "RAID5" {
					hasRAIDType = true
				}
			}

			assert.True(t, hasHost, "Host tag should be set on all metrics")
			assert.True(t, hasVolumeID, "volume_id tag should be set on all metrics")
			assert.True(t, hasVolumeName, "volume_name tag should be set on all metrics")
			assert.True(t, hasPoolID, "pool_name tag should be set on all metrics")
			assert.True(t, hasRAIDType, "raid_type tag should be set on all metrics")

			// Check timestamp is correctly set
			assert.Equal(t, testTimestamp, metric.Timestamp)
		}

		// Check all expected metrics are present
		assert.True(t, metricNames["hardware.storage.volume.health"])
		assert.True(t, metricNames["hardware.storage.volume.encrypted"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.total"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.allocated"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.allocated_percent"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.used"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.used_percent"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.free_percent"])
		assert.True(t, metricNames["hardware.storage.volume.capacity.free"])
		assert.True(t, metricNames["hardware.storage.volume.io.total_ops"])
		assert.True(t, metricNames["hardware.storage.volume.io.reads"])
		assert.True(t, metricNames["hardware.storage.volume.io.writes"])
		assert.True(t, metricNames["hardware.storage.volume.io.total_bytes"])
		assert.True(t, metricNames["hardware.storage.volume.io.read.bytes"])
		assert.True(t, metricNames["hardware.storage.volume.io.write.bytes"])
		assert.True(t, metricNames["hardware.storage.volume.io.read.latency"])
		assert.True(t, metricNames["hardware.storage.volume.io.write.latency"])
	})

	t.Run("Complete CollectMetrics Flow", func(t *testing.T) {
		// Create mock client
		mockClient := NewMockRedfishClient()

		// Mock response for system info
		mockClient.AddMockResponse("Systems/1", `{
			"Name": "TestSystem", 
			"Id": "1"
		}`)

		// Mock response for controller
		mockClient.AddMockResponse("Storage/controller_a", `{
			"@odata.id": "/redfish/v1/Storage/controller_a",
			"Id": "controller_a",
			"Name": "Storage Controller A",
			"Status": {"Health": "OK", "State": "Enabled"},
			"StorageControllers": [{
				"Manufacturer": "Dell",
				"Model": "PowerVault",
				"SerialNumber": "ABC123"
			}]
		}`)

		// Mock response for volumes collection
		mockClient.AddMockResponse("Storage/controller_a/Volumes", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Volumes",
			"Members": [
				{"@odata.id": "/redfish/v1/Storage/controller_a/Volumes/1"}
			]
		}`)

		// Mock response for volume
		mockClient.AddMockResponse("Storage/controller_a/Volumes/1", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Volumes/1",
			"Id": "1",
			"Name": "Volume 1",
			"CapacityBytes": 500000000000,
			"Status": {"Health": "OK"}
		}`)

		// Mock response for pools collection
		mockClient.AddMockResponse("Storage/controller_a/StoragePools", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools",
			"Members": [
				{"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A"}
			]
		}`)

		// Mock response for pool
		mockClient.AddMockResponse("Storage/controller_a/StoragePools/A", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A",
			"Id": "A",
			"Name": "Pool A",
			"Status": {"Health": "OK"},
			"CapacityBytes": 1000000000000
		}`)

		// Mock response for drives collection
		mockClient.AddMockResponse("Storage/controller_a/Drives", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Drives",
			"Members": [
				{"@odata.id": "/redfish/v1/Storage/controller_a/Drives/0"}
			]
		}`)

		// Mock response for drive
		mockClient.AddMockResponse("Storage/controller_a/Drives/0", `{
			"@odata.id": "/redfish/v1/Storage/controller_a/Drives/0",
			"Id": "0",
			"Name": "Drive 0",
			"Status": {"Health": "OK"},
			"CapacityBytes": 1000000000000
		}`)

		// Mock responses for event metrics
		mockClient.AddMockResponse("Managers", `{
			"@odata.id": "/redfish/v1/Managers",
			"Members": [
				{"@odata.id": "/redfish/v1/Managers/1"}
			]
		}`)

		mockClient.AddMockResponse("Managers/1", `{
			"@odata.id": "/redfish/v1/Managers/1",
			"Id": "1",
			"Name": "Storage Controller Manager",
			"Model": "PowerVault ME5024"
		}`)

		mockClient.AddMockResponse("Managers/1/LogServices", `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices",
			"Members": [
				{"@odata.id": "/redfish/v1/Managers/1/LogServices/Log"}
			]
		}`)

		mockClient.AddMockResponse("Managers/1/LogServices/Log", `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices/Log",
			"Id": "Log",
			"Name": "System Logs"
		}`)

		mockClient.AddMockResponse("Managers/1/LogServices/Log/Entries", `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries",
			"Members": [
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/1",
					"Id": "1",
					"Severity": "Critical",
					"Created": "2024-05-18T10:00:00Z",
					"Message": "Disk failure detected"
				},
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/2",
					"Id": "2",
					"Severity": "Warning",
					"Created": "2024-05-18T11:00:00Z",
					"Message": "Temperature threshold exceeded"
				}
			],
			"Members@odata.count": 2
		}`)

		mockClient.AddMockResponse("EventService", `{
			"@odata.id": "/redfish/v1/EventService",
			"Id": "EventService",
			"Name": "Event Service",
			"Status": {
				"Health": "OK",
				"State": "Enabled"
			}
		}`)

		mockClient.AddMockResponse("EventService/Subscriptions", `{
			"@odata.id": "/redfish/v1/EventService/Subscriptions",
			"Members": [
				{"@odata.id": "/redfish/v1/EventService/Subscriptions/1"}
			]
		}`)

		// Create collector
		collector, err := NewStorageCollector("https://redfish.example.com", "admin", "password", loggerPtr, false)
		assert.NoError(t, err)
		storageCollector := collector.(*StorageCollector)

		// Replace client with mock
		storageCollector.GenericCollector.client = mockClient
		storageCollector.GenericCollector.systems = []string{"Systems/1"}

		// Set resources for the test
		storageCollector.storageControllers = []string{"Storage/controller_a"}
		storageCollector.storageVolumes = []string{"Storage/controller_a/Volumes/1"}
		storageCollector.storagePools = []string{"Storage/controller_a/StoragePools/A"}

		// Call CollectMetrics
		metrics, err := storageCollector.CollectMetrics(ctx, CollectionStorage, testTimestamp)
		assert.NoError(t, err)
		assert.NotEmpty(t, metrics)

		// Check that we have metrics from each component type
		hasPoolMetric := false
		hasVolumeMetric := false
		hasControllerMetric := false
		hasDriveMetric := false
		hasEventMetric := false

		for _, metric := range metrics {
			if metric.Name == "hardware.storage.pool.health" {
				hasPoolMetric = true
			}
			if metric.Name == "hardware.storage.volume.health" {
				hasVolumeMetric = true
			}
			if metric.Name == "hardware.storage.controller.health" {
				hasControllerMetric = true
			}
			if metric.Name == "hardware.storage.drive.health" {
				hasDriveMetric = true
			}
			if metric.Name == "hardware.logs.entries.total" {
				hasEventMetric = true
			}
		}

		assert.True(t, hasPoolMetric, "Should have pool metrics")
		assert.True(t, hasVolumeMetric, "Should have volume metrics")
		assert.True(t, hasControllerMetric, "Should have controller metrics")
		assert.True(t, hasDriveMetric, "Should have drive metrics")
		assert.True(t, hasEventMetric, "Should have event metrics")
	})
}

// Test for mapHealthState function
func TestMapHealthState(t *testing.T) {
	tests := []struct {
		health string
		want   int
	}{
		{"OK", 0},
		{"ok", 0},
		{"Warning", 1},
		{"warning", 1},
		{"Critical", 2},
		{"critical", 2},
		{"Unknown", 3},
		{"unknown", 3},
		{"", 3},
		{"SomeOtherState", 3},
	}

	for _, tt := range tests {
		t.Run(tt.health, func(t *testing.T) {
			got := mapHealthState(tt.health)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test for helper functions
func TestHelperFunctions(t *testing.T) {
	t.Run("boolToFloat", func(t *testing.T) {
		assert.Equal(t, 1.0, boolToFloat(true))
		assert.Equal(t, 0.0, boolToFloat(false))
	})
}
