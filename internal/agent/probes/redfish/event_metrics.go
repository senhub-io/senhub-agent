// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"encoding/json"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"strings"
	"time"
)

// collectEventMetrics gathers metrics related to system events and logs
func (c *StorageCollector) collectEventMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	// Get system name to use as the host tag
	var hostName string
	if len(c.systems) > 0 {
		sysResp, err := c.client.Get(ctx, c.systems[0])
		if err == nil && sysResp.Name != "" {
			hostName = sysResp.Name
		}
	}
	if hostName == "" {
		rootResp, err := c.client.Get(ctx, "")
		if err == nil && rootResp.UUID != "" {
			hostName = rootResp.UUID
		}
	}

	// Get managers to find log services
	managersPath := "Managers"
	managersResp, err := c.client.Get(ctx, managersPath)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to retrieve managers for event metrics")
		return datapoints, nil
	}

	// Process each manager to get logs
	for _, managerRef := range managersResp.Members {
		managerPath, ok := managerRef["@odata.id"]
		if !ok {
			continue
		}

		// Try to get manager details
		managerPath = strings.TrimPrefix(managerPath, "/redfish/v1/")
		managerResp, err := c.client.Get(ctx, managerPath)
		if err != nil {
			c.logger.Debug().Err(err).Str("path", managerPath).Msg("Unable to get manager details")
			continue
		}

		// Create base tags for this manager
		managerID := managerResp.ID
		managerTags := []tags.Tag{
			{Key: "manager_id", Value: managerID},
		}
		if managerResp.Name != "" {
			managerTags = append(managerTags, tags.Tag{Key: "manager_name", Value: managerResp.Name})
		}
		if managerResp.Model != "" {
			managerTags = append(managerTags, tags.Tag{Key: "model", Value: managerResp.Model})
		}
		if hostName != "" {
			managerTags = append(managerTags, tags.Tag{Key: "host", Value: hostName})
		}

		// Check for LogServices
		logServicesPath := managerPath + "/LogServices"
		logServicesResp, err := c.client.Get(ctx, logServicesPath)
		if err != nil {
			c.logger.Debug().Err(err).Str("path", logServicesPath).Msg("Unable to get log services")
			continue
		}

		// Process each log service
		for _, logServiceRef := range logServicesResp.Members {
			logServicePath, ok := logServiceRef["@odata.id"]
			if !ok {
				continue
			}

			// Get log service details
			logServicePath = strings.TrimPrefix(logServicePath, "/redfish/v1/")
			logServiceResp, err := c.client.Get(ctx, logServicePath)
			if err != nil {
				c.logger.Debug().Err(err).Str("path", logServicePath).Msg("Unable to get log service details")
				continue
			}

			// Create log service specific tags
			logServiceTags := append([]tags.Tag{}, managerTags...)
			logServiceID := logServiceResp.ID
			logServiceTags = append(logServiceTags, tags.Tag{Key: "log_service_id", Value: logServiceID})
			if logServiceResp.Name != "" {
				logServiceTags = append(logServiceTags, tags.Tag{Key: "log_service_name", Value: logServiceResp.Name})
			}

			// Get log entries
			entriesPath := logServicePath + "/Entries"
			entriesResp, err := c.client.Get(ctx, entriesPath)
			if err != nil {
				c.logger.Debug().Err(err).Str("path", entriesPath).Msg("Unable to get log entries")
				continue
			}

			// Process log entry metrics
			logEntryMetrics, err := processLogEntries(entriesResp, logServiceTags, timestamp)
			if err != nil {
				c.logger.Warn().Err(err).Msg("Failed to process log entries")
				continue
			}
			
			datapoints = append(datapoints, logEntryMetrics...)
		}
	}

	// Also check for dedicated EventService
	eventServicePath := "EventService"
	eventServiceResp, err := c.client.Get(ctx, eventServicePath)
	if err == nil {
		// Create EventService tags
		eventServiceTags := []tags.Tag{}
		if hostName != "" {
			eventServiceTags = append(eventServiceTags, tags.Tag{Key: "host", Value: hostName})
		}
		
		// Check status of event service
		if eventServiceResp.Status != nil && eventServiceResp.Status.Health != "" {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.eventservice.health",
				Timestamp: timestamp,
				Value:     float32(mapHealthState(eventServiceResp.Status.Health)),
				Tags:      eventServiceTags,
			})
		}
		
		// Check for subscriptions
		subscriptionsPath := eventServicePath + "/Subscriptions"
		subscriptionsResp, err := c.client.Get(ctx, subscriptionsPath)
		if err == nil && len(subscriptionsResp.Members) > 0 {
			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.eventservice.subscriptions",
				Timestamp: timestamp,
				Value:     float32(len(subscriptionsResp.Members)),
				Tags:      eventServiceTags,
			})
		}
	}

	return datapoints, nil
}

// processLogEntries analyzes log entries and generates metrics
func processLogEntries(entriesResp *RedfishResponse, baseTags []tags.Tag, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint
	
	// Basic count of total entries
	totalEntries := len(entriesResp.Members)
	if entriesResp.MembersCount > 0 {
		// Use MembersCount if available (more accurate for large logs)
		totalEntries = entriesResp.MembersCount
	}
	
	// Add metric for total entries
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.total",
		Timestamp: timestamp,
		Value:     float32(totalEntries),
		Tags:      baseTags,
	})
	
	// Parse entries and count by severity
	criticalCount := 0
	warningCount := 0
	infoCount := 0
	
	// Initialize current time for time-based metrics
	now := time.Now()
	last24hCount := 0
	last7dCount := 0
	
	// Only process entries if we have the details
	entriesArray := make([]map[string]interface{}, 0)
	entriesRaw, err := json.Marshal(entriesResp.Members)
	if err != nil {
		return datapoints, err
	}
	
	if err := json.Unmarshal(entriesRaw, &entriesArray); err != nil {
		return datapoints, err
	}
	
	// Process each entry to count by severity and time range
	for _, entry := range entriesArray {
		// Check severity
		if severity, ok := entry["Severity"].(string); ok {
			switch strings.ToLower(severity) {
			case "critical":
				criticalCount++
			case "warning":
				warningCount++
			case "ok", "informational", "information":
				infoCount++
			}
		}
		
		// Check entry time for recent events
		if created, ok := entry["Created"].(string); ok {
			if entryTime, err := time.Parse(time.RFC3339, created); err == nil {
				// Count events from last 24 hours
				if now.Sub(entryTime) <= 24*time.Hour {
					last24hCount++
				}
				
				// Count events from last 7 days
				if now.Sub(entryTime) <= 7*24*time.Hour {
					last7dCount++
				}
			}
		}
	}
	
	// Add metrics by severity
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.critical",
		Timestamp: timestamp,
		Value:     float32(criticalCount),
		Tags:      baseTags,
	})
	
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.warning",
		Timestamp: timestamp,
		Value:     float32(warningCount),
		Tags:      baseTags,
	})
	
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.info",
		Timestamp: timestamp,
		Value:     float32(infoCount),
		Tags:      baseTags,
	})
	
	// Add metrics for time-based counts
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.last_24h",
		Timestamp: timestamp,
		Value:     float32(last24hCount),
		Tags:      baseTags,
	})
	
	datapoints = append(datapoints, data_store.DataPoint{
		Name:      "hardware.logs.entries.last_7d",
		Timestamp: timestamp,
		Value:     float32(last7dCount),
		Tags:      baseTags,
	})
	
	return datapoints, nil
}