package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"strings"
	"time"
)

// collectNetworkMetrics gathers network metrics from a Redfish system
func (c *GenericCollector) collectNetworkMetrics(ctx context.Context, timestamp time.Time) ([]data_store.DataPoint, error) {
	var datapoints []data_store.DataPoint

	for _, systemID := range c.systems {
		// Normalize system ID by removing any redundant "Systems/" prefix
		// This fixes paths like "Systems/Systems/something" that can occur in some implementations
		normalizedSystemID := strings.TrimPrefix(systemID, "Systems/")

		// Get system details for hostname - will be added to network interface tags
		var hostname string
		sysResp, err := c.client.Get(ctx, systemID)
		if err == nil && sysResp.Name != "" {
			hostname = sysResp.Name
		}

		// For storage systems like PowerVault ME5024, we may need to skip network adapters
		// as they don't expose network interfaces through Redfish
		if c.vendorType == VendorStorage {
			// Check if this is a storage system that doesn't support network endpoints
			testPath := fmt.Sprintf("Systems/%s/NetworkInterfaces", normalizedSystemID)
			_, testErr := c.client.Get(ctx, testPath)
			if testErr != nil && strings.Contains(testErr.Error(), "400") &&
				strings.Contains(testErr.Error(), "Invalid URL") {
				// This appears to be a storage system that doesn't support network endpoints
				c.logger.Debug().
					Str("system_id", systemID).
					Msg("Storage system does not support network interfaces endpoints, skipping")
				continue
			}
		}

		// Network interfaces path
		networkPath := fmt.Sprintf("Systems/%s/NetworkInterfaces", normalizedSystemID)

		// Fetch network collection
		networkResp, err := c.client.Get(ctx, networkPath)
		if err != nil {
			c.logger.Debug().
				Err(err).
				Str("path", networkPath).
				Msg("Failed to get network interfaces collection")

			// Try alternate path - EthernetInterfaces is also common
			alternatePath := fmt.Sprintf("Systems/%s/EthernetInterfaces", normalizedSystemID)
			networkResp, err = c.client.Get(ctx, alternatePath)
			if err != nil {
				c.logger.Debug().
					Err(err).
					Str("path", alternatePath).
					Msg("Failed to get ethernet interfaces collection")

				// Try another common pattern - adapters directly under system
				adaptersPath := fmt.Sprintf("Systems/%s/Adapters", normalizedSystemID)
				networkResp, err = c.client.Get(ctx, adaptersPath)
				if err != nil {
					c.logger.Debug().
						Err(err).
						Str("path", adaptersPath).
						Msg("Failed to get adapters collection")
					continue
				}
			}
		}

		// Parse the collection
		var networkCollection struct {
			Members []struct {
				OdataID string `json:"@odata.id"`
			} `json:"Members"`
		}

		if err := json.Unmarshal(networkResp.Raw, &networkCollection); err != nil {
			c.logger.Warn().
				Err(err).
				Str("path", networkPath).
				Msg("Failed to parse network collection")
			continue
		}

		// Process each network interface
		for _, member := range networkCollection.Members {
			if member.OdataID == "" {
				continue
			}

			// Fetch network interface
			networkDetailResp, err := c.client.Get(ctx, member.OdataID)
			if err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to get network interface details")
				continue
			}

			// Parse network interface details
			var network struct {
				ID            string  `json:"Id"`
				Name          string  `json:"Name"`
				Description   string  `json:"Description"`
				Status        *Status `json:"Status"`
				SpeedMbps     int     `json:"SpeedMbps"`
				MACAddress    string  `json:"MACAddress"`
				LinkStatus    string  `json:"LinkStatus"`
				InterfaceType string  `json:"InterfaceType"`
			}

			if err := json.Unmarshal(networkDetailResp.Raw, &network); err != nil {
				c.logger.Warn().
					Err(err).
					Str("path", member.OdataID).
					Msg("Failed to parse network interface details")
				continue
			}

			// Network interface tags - suivant les conventions de REDFISH-TAGS.md
			networkTags := []tags.Tag{
				{Key: "system_id", Value: systemID},
				{Key: "adapter_id", Value: network.ID},
				{Key: "adapter_name", Value: network.Name},
				{Key: "mac_address", Value: network.MACAddress},
				{Key: "interface_type", Value: network.InterfaceType},
			}

			// Add host tag if available
			if hostname != "" {
				networkTags = append(networkTags, tags.Tag{Key: "host", Value: hostname})
			}

			// Network health state
			if network.Status != nil && network.Status.Health != "" {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.network.health",
					Value:     float32(mapHealthState(network.Status.Health)),
					Timestamp: timestamp,
					Tags:      networkTags,
				})
			}

			// Network speed
			if network.SpeedMbps > 0 {
				datapoints = append(datapoints, data_store.DataPoint{
					Name:      "hardware.network.speed_mbps",
					Value:     float32(network.SpeedMbps),
					Timestamp: timestamp,
					Tags:      networkTags,
				})
			}

			// Link status
			linkUp := 0.0
			if strings.ToLower(network.LinkStatus) == "up" || strings.ToLower(network.LinkStatus) == "linked" {
				linkUp = 1.0
			}

			datapoints = append(datapoints, data_store.DataPoint{
				Name:      "hardware.network.link_up",
				Value:     float32(linkUp),
				Timestamp: timestamp,
				Tags:      networkTags,
			})
		}
	}

	return datapoints, nil
}
