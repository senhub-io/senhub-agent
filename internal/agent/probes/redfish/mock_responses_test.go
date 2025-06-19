package redfish

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// This file contains test data and helpers for mock responses used in Redfish tests



// Mock response for system info
const testSystemResponse = `{
	"@odata.id": "/redfish/v1/Systems/1",
	"Id": "1",
	"Name": "TestSystem",
	"Model": "PowerVault ME5024",
	"Manufacturer": "Dell Inc.",
	"SerialNumber": "TEST123456",
	"Status": {"Health": "OK", "State": "Enabled"}
}`

// Mock response for storage service
const testStorageResponse = `{
	"@odata.id": "/redfish/v1/Storage",
	"Members": [
		{"@odata.id": "/redfish/v1/Storage/controller_a"}
	]
}`

// Mock response for controller
const testControllerResponse = `{
	"@odata.id": "/redfish/v1/Storage/controller_a",
	"Id": "controller_a",
	"Name": "Storage Controller A",
	"Status": {"Health": "OK", "State": "Enabled"},
	"StorageControllers": [
		{
			"Manufacturer": "Dell Inc.",
			"Model": "PowerVault ME5024 Controller",
			"SerialNumber": "CTRLSN123",
			"FirmwareVersion": "5.01.02"
		}
	],
	"Redundancy": [
		{
			"MaxNumSupported": 2,
			"MemberId": "0",
			"MinNumNeeded": 1,
			"Mode": "Failover",
			"Name": "Controller Redundancy Group 1",
			"RedundancySet": [
				{"@odata.id": "/redfish/v1/Storage/controller_a"}
			],
			"Status": {
				"Health": "OK",
				"State": "Enabled"
			}
		}
	]
}`

// Mock response for volumes
const testVolumesResponse = `{
	"@odata.id": "/redfish/v1/Storage/controller_a/Volumes",
	"Members": [
		{"@odata.id": "/redfish/v1/Storage/controller_a/Volumes/1"}
	]
}`

// Mock response for volume with detailed metrics
const testVolumeResponse = `{
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
	"WriteCachePolicy": "WriteBack",
	"RemainingCapacityPercent": 75,
	"Capacity": {
		"Data": {
			"AllocatedBytes": 500000000000,
			"ConsumedBytes": 125000000000
		},
		"IsThinProvisioned": true
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
	],
	"Oem": {
		"Dell": {
			"IOStats": {
				"ReadOps": 50000,
				"ReadBytes": 104857600,
				"WriteOps": 30000,
				"WriteBytes": 62914560,
				"ReadLatency": 10.5,
				"WriteLatency": 5.3
			}
		}
	}
}`

// Mock response for pools
const testPoolsResponse = `{
	"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools",
	"Members": [
		{"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A"}
	]
}`

// Mock response for pool with detailed metrics
const testPoolResponse = `{
	"@odata.id": "/redfish/v1/Storage/controller_a/StoragePools/A",
	"Id": "A",
	"Name": "Pool A",
	"Status": {"Health": "OK", "State": "Enabled"},
	"CapacityBytes": 1099511627776,
	"RemainingCapacityPercent": 85,
	"RemainingCapacityBytes": 934584883610,
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
	},
	"Oem": {
		"Dell": {
			"VolumesBytes": 164926744166,
			"FreeBytes": 934584883610
		}
	}
}`

// Mock response for drives
const testDrivesResponse = `{
	"@odata.id": "/redfish/v1/Storage/controller_a/Drives",
	"Members": [
		{"@odata.id": "/redfish/v1/Storage/controller_a/Drives/0"},
		{"@odata.id": "/redfish/v1/Storage/controller_a/Drives/1"}
	]
}`

// Mock response for normal drive
const testDrive0Response = `{
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

// Mock response for drive with operations
const testDrive1Response = `{
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

// validateMockRedfishClient parses all mock responses to ensure they're valid JSON
func validateMockRedfishClient(t *testing.T, mockClient *MockRedfishClient) {
	for path, response := range mockClient.responseMocks {
		var jsonObj interface{}
		err := json.Unmarshal([]byte(response), &jsonObj)
		assert.NoError(t, err, "Mock response for path %s should be valid JSON", path)
	}
}

// TestMockResponses verifies that all our test responses are valid
func TestMockResponses(t *testing.T) {
	// Create a mock client and add all responses
	mockClient := NewMockRedfishClient()

	// Add all mock responses
	mockClient.AddMockResponse("Systems/1", testSystemResponse)
	mockClient.AddMockResponse("Storage", testStorageResponse)
	mockClient.AddMockResponse("Storage/controller_a", testControllerResponse)
	mockClient.AddMockResponse("Storage/controller_a/Volumes", testVolumesResponse)
	mockClient.AddMockResponse("Storage/controller_a/Volumes/1", testVolumeResponse)
	mockClient.AddMockResponse("Storage/controller_a/StoragePools", testPoolsResponse)
	mockClient.AddMockResponse("Storage/controller_a/StoragePools/A", testPoolResponse)
	mockClient.AddMockResponse("Storage/controller_a/Drives", testDrivesResponse)
	mockClient.AddMockResponse("Storage/controller_a/Drives/0", testDrive0Response)
	mockClient.AddMockResponse("Storage/controller_a/Drives/1", testDrive1Response)

	// Validate all responses
	validateMockRedfishClient(t, mockClient)

	// Test that we can retrieve responses
	mockClient.On("Connect", mock.Anything).Return(nil)

	// Test getting a response
	ctx := context.Background()
	resp, err := mockClient.Get(ctx, "Storage/controller_a")
	assert.NoError(t, err)
	assert.Equal(t, "controller_a", resp.ID)
	assert.Equal(t, "Storage Controller A", resp.Name)
}
