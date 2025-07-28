package citrix

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetSites retrieves all sites from the Delivery Controller
func (c *deliveryControllerClient) GetSites(ctx context.Context) ([]Site, error) {
	c.logger.Debug().Msg("Retrieving sites from Delivery Controller")
	
	// CVAD REST API endpoint for sites
	endpoint := "/cvad/manage/Sites"
	
	body, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get sites: %w", err)
	}
	
	// Parse response which contains Items array
	var response struct {
		Items []Site `json:"Items"`
	}
	
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse sites response: %w", err)
	}
	
	c.logger.Info().
		Int("site_count", len(response.Items)).
		Msg("Retrieved sites from Delivery Controller")
	
	return response.Items, nil
}

// GetMachinesBySite retrieves all machines for a specific site
func (c *deliveryControllerClient) GetMachinesBySite(ctx context.Context, siteName string) ([]string, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving machines for site")
	
	// First, we need to get the site ID
	sites, err := c.GetSites(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sites: %w", err)
	}
	
	var siteID string
	for _, site := range sites {
		if site.Name == siteName {
			siteID = site.Id
			break
		}
	}
	
	if siteID == "" {
		return nil, fmt.Errorf("site %s not found", siteName)
	}
	
	// CVAD REST API endpoint for machines
	// We'll use a filter to get machines for this site
	endpoint := fmt.Sprintf("/cvad/manage/Machines?filter=Site/Id eq '%s'", siteID)
	
	body, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get machines: %w", err)
	}
	
	// Parse machine response
	var response struct {
		Items []struct {
			Id          string `json:"Id"`
			Name        string `json:"Name"`
			DNSName     string `json:"DNSName"`
			MachineName string `json:"MachineName"`
		} `json:"Items"`
	}
	
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse machines response: %w", err)
	}
	
	// Extract machine names (prefer DNSName, fallback to MachineName or Name)
	machines := make([]string, 0, len(response.Items))
	for _, m := range response.Items {
		if m.DNSName != "" {
			machines = append(machines, m.DNSName)
		} else if m.MachineName != "" {
			machines = append(machines, m.MachineName)
		} else if m.Name != "" {
			machines = append(machines, m.Name)
		}
	}
	
	c.logger.Info().
		Str("site", siteName).
		Int("machine_count", len(machines)).
		Msg("Retrieved machines for site")
	
	return machines, nil
}

// TestConnectivity tests the connection to the Delivery Controller
func (c *deliveryControllerClient) TestConnectivity(ctx context.Context) error {
	c.logger.Debug().Msg("Testing Delivery Controller connectivity")
	
	// Try to get a token
	if err := c.getToken(ctx); err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}
	
	// Try a simple API call
	endpoint := "/cvad/manage/Sites"
	_, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("API test failed: %w", err)
	}
	
	c.logger.Info().Msg("Delivery Controller connectivity test successful")
	return nil
}