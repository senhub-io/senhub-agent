package redfish

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"os"
)

func TestEventMetricsCollection(t *testing.T) {
	// Create a test logger
	testLogger := zerolog.New(os.Stderr)
	loggerPtr := (*zerolog.Logger)(&testLogger)
	testTimestamp := time.Now()

	t.Run("CollectEventMetrics", func(t *testing.T) {
		// Create mock client
		mockClient := NewMockRedfishClient()

		// Mock response for system info
		mockClient.AddMockResponse("Systems/1", `{
			"Name": "TestSystem", 
			"Id": "1"
		}`)

		// Mock response for managers
		managersResponse := `{
			"@odata.id": "/redfish/v1/Managers",
			"Members": [
				{"@odata.id": "/redfish/v1/Managers/1"}
			]
		}`
		mockClient.AddMockResponse("Managers", managersResponse)

		// Mock response for a specific manager
		managerResponse := `{
			"@odata.id": "/redfish/v1/Managers/1",
			"Id": "1",
			"Name": "Storage Controller Manager",
			"Model": "PowerVault ME5024"
		}`
		mockClient.AddMockResponse("Managers/1", managerResponse)

		// Mock response for log services
		logServicesResponse := `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices",
			"Members": [
				{"@odata.id": "/redfish/v1/Managers/1/LogServices/Log"}
			]
		}`
		mockClient.AddMockResponse("Managers/1/LogServices", logServicesResponse)

		// Mock response for a specific log service
		logServiceResponse := `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices/Log",
			"Id": "Log",
			"Name": "System Logs"
		}`
		mockClient.AddMockResponse("Managers/1/LogServices/Log", logServiceResponse)

		// Mock response for log entries
		entriesResponse := `{
			"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries",
			"Members": [
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/1",
					"Id": "1",
					"Severity": "Critical",
					"Created": "` + time.Now().Add(-1*time.Hour).Format(time.RFC3339) + `",
					"Message": "Disk failure detected"
				},
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/2",
					"Id": "2",
					"Severity": "Warning",
					"Created": "` + time.Now().Add(-2*time.Hour).Format(time.RFC3339) + `",
					"Message": "Temperature threshold exceeded"
				},
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/3",
					"Id": "3",
					"Severity": "OK",
					"Created": "` + time.Now().Add(-3*time.Hour).Format(time.RFC3339) + `",
					"Message": "System startup"
				},
				{
					"@odata.id": "/redfish/v1/Managers/1/LogServices/Log/Entries/4",
					"Id": "4",
					"Severity": "Informational",
					"Created": "` + time.Now().Add(-8*24*time.Hour).Format(time.RFC3339) + `",
					"Message": "Old log entry"
				}
			],
			"Members@odata.count": 4
		}`
		mockClient.AddMockResponse("Managers/1/LogServices/Log/Entries", entriesResponse)

		// Mock response for event service
		eventServiceResponse := `{
			"@odata.id": "/redfish/v1/EventService",
			"Id": "EventService",
			"Name": "Event Service",
			"Status": {
				"Health": "OK",
				"State": "Enabled"
			}
		}`
		mockClient.AddMockResponse("EventService", eventServiceResponse)

		// Mock response for event subscriptions
		subscriptionsResponse := `{
			"@odata.id": "/redfish/v1/EventService/Subscriptions",
			"Members": [
				{"@odata.id": "/redfish/v1/EventService/Subscriptions/1"}
			]
		}`
		mockClient.AddMockResponse("EventService/Subscriptions", subscriptionsResponse)

		// Create collector
		collector, err := NewStorageCollector("https://redfish.example.com", "admin", "password", loggerPtr, false)
		assert.NoError(t, err)
		storageCollector := collector.(*StorageCollector)

		// Replace client with mock
		storageCollector.GenericCollector.client = mockClient
		storageCollector.GenericCollector.systems = []string{"Systems/1"}

		// Collect event metrics
		metrics, err := storageCollector.collectEventMetrics(context.Background(), testTimestamp)
		assert.NoError(t, err)
		assert.NotEmpty(t, metrics)

		// Verify metrics - create a map for easier verification
		metricMap := make(map[string]float32)
		for _, metric := range metrics {
			metricMap[metric.Name] = metric.Value
			// Verify all metrics have the correct timestamp
			assert.Equal(t, testTimestamp, metric.Timestamp)
		}

		// Check total entries count
		assert.Contains(t, metricMap, "hardware.logs.entries.total")
		assert.Equal(t, float32(4), metricMap["hardware.logs.entries.total"])

		// Check severity counts
		assert.Contains(t, metricMap, "hardware.logs.entries.critical")
		assert.Equal(t, float32(1), metricMap["hardware.logs.entries.critical"])

		assert.Contains(t, metricMap, "hardware.logs.entries.warning")
		assert.Equal(t, float32(1), metricMap["hardware.logs.entries.warning"])

		assert.Contains(t, metricMap, "hardware.logs.entries.info")
		assert.Equal(t, float32(2), metricMap["hardware.logs.entries.info"])

		// Check timeframe counts
		assert.Contains(t, metricMap, "hardware.logs.entries.last_24h")
		assert.Equal(t, float32(3), metricMap["hardware.logs.entries.last_24h"])

		assert.Contains(t, metricMap, "hardware.logs.entries.last_7d")
		assert.Equal(t, float32(3), metricMap["hardware.logs.entries.last_7d"])

		// Check event service metrics
		assert.Contains(t, metricMap, "hardware.eventservice.health")
		assert.Equal(t, float32(0), metricMap["hardware.eventservice.health"]) // OK = 0

		assert.Contains(t, metricMap, "hardware.eventservice.subscriptions")
		assert.Equal(t, float32(1), metricMap["hardware.eventservice.subscriptions"])
	})
}
