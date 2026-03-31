package citrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetMachinesBySite retrieves all machines for a specific site
func (c *deliveryControllerClient) GetMachinesBySite(ctx context.Context, siteName string) ([]string, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving machines for site")

	siteID, actualSiteName, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	return c.getMachinesBySiteID(ctx, siteID, actualSiteName)
}

// getMachinesBySiteID is a helper method that retrieves machines by site ID
func (c *deliveryControllerClient) getMachinesBySiteID(ctx context.Context, siteID, siteName string) ([]string, error) {
	c.logger.Debug().
		Str("site_id", siteID).
		Str("site_name", siteName).
		Msg("Retrieving machines for site ID")

	// Collect all machines with pagination support
	var allMachines []string
	continuationToken := ""
	pageCount := 0

	for {
		// CVAD REST API endpoint for machines with pagination
		endpoint := "/cvad/manage/Machines"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}

		body, err := c.makeRequestWithSiteID(ctx, "GET", endpoint, nil, siteID)
		if err != nil {
			return nil, fmt.Errorf("failed to get machines: %w", err)
		}

		// Parse machine response with ContinuationToken
		var response struct {
			Items []struct {
				Id          string `json:"Id"`
				Name        string `json:"Name"`
				DNSName     string `json:"DnsName"`
				MachineName string `json:"MachineName"`
			} `json:"Items"`
			ContinuationToken string `json:"ContinuationToken,omitempty"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse machines response: %w", err)
		}

		pageCount++
		c.logger.Debug().
			Int("page", pageCount).
			Int("items_in_page", len(response.Items)).
			Str("continuation_token", response.ContinuationToken).
			Msg("Processing machines page")

		// Extract machine names (prefer DNSName, fallback to MachineName or Name)
		for _, m := range response.Items {
			machineName := ""
			if m.DNSName != "" {
				machineName = m.DNSName
			} else if m.MachineName != "" {
				machineName = m.MachineName
			} else if m.Name != "" {
				machineName = m.Name
			}

			if machineName != "" {
				allMachines = append(allMachines, machineName)
			}
		}

		// Check if there are more pages
		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}

	c.logger.Debug().
		Str("site", siteName).
		Str("site_id", siteID).
		Int("machine_count", len(allMachines)).
		Int("pages_processed", pageCount).
		Msg("Retrieved all machines for site")

	return allMachines, nil
}

// GetMachinesDetailedBySite retrieves detailed machine info for a specific site
func (c *deliveryControllerClient) GetMachinesDetailedBySite(ctx context.Context, siteName string) ([]DDCMachine, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving detailed machines for site")

	siteID, _, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	// Get machines with pagination support
	var allMachines []DDCMachine
	continuationToken := ""

	for {
		endpoint := "/cvad/manage/Machines"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}

		body, err := c.makeRequestWithSiteID(ctx, "GET", endpoint, nil, siteID)
		if err != nil {
			return nil, fmt.Errorf("failed to get machines: %w", err)
		}

		var response DDCMachinesResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse machines response: %w", err)
		}

		// Add all machines (server-side filtering via Citrix-InstanceId header should handle site filtering)
		allMachines = append(allMachines, response.Items...)

		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}

	c.logger.Debug().
		Str("site", siteName).
		Int("machine_count", len(allMachines)).
		Msg("Retrieved detailed machines for site")

	return allMachines, nil
}

// GetDeliveryGroupsBySite retrieves all delivery groups for a specific site
func (c *deliveryControllerClient) GetDeliveryGroupsBySite(ctx context.Context, siteName string) ([]DDCDeliveryGroup, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving delivery groups for site")

	siteID, _, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	// Get delivery groups with pagination
	var allGroups []DDCDeliveryGroup
	continuationToken := ""

	for {
		endpoint := "/cvad/manage/DeliveryGroups"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}

		body, err := c.makeRequestWithSiteID(ctx, "GET", endpoint, nil, siteID)
		if err != nil {
			return nil, fmt.Errorf("failed to get delivery groups: %w", err)
		}

		var response DDCDeliveryGroupsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse delivery groups response: %w", err)
		}

		// Add all groups (server-side filtering via Citrix-InstanceId header should handle site filtering)
		allGroups = append(allGroups, response.Items...)

		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}

	c.logger.Debug().
		Str("site", siteName).
		Int("group_count", len(allGroups)).
		Msg("Retrieved delivery groups for site")

	return allGroups, nil
}

// GetControllersBySite retrieves all controllers for a specific site
func (c *deliveryControllerClient) GetControllersBySite(ctx context.Context, siteName string) ([]DDCController, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving controllers for site")

	siteID, _, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	// Get controllers with pagination support
	var siteControllers []DDCController
	continuationToken := ""

	for {
		endpoint := "/cvad/manage/Controllers"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}

		body, err := c.makeRequestWithSiteID(ctx, "GET", endpoint, nil, siteID)
		if err != nil {
			return nil, fmt.Errorf("failed to get controllers: %w", err)
		}

		var response DDCControllersResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse controllers response: %w", err)
		}

		// Add all controllers (server-side filtering via Citrix-InstanceId header should handle site filtering)
		siteControllers = append(siteControllers, response.Items...)

		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}

	c.logger.Debug().
		Str("site", siteName).
		Int("controller_count", len(siteControllers)).
		Msg("Retrieved controllers for site")

	return siteControllers, nil
}

// GetSessionsBySite retrieves active sessions for a specific site
func (c *deliveryControllerClient) GetSessionsBySite(ctx context.Context, siteName string) ([]DDCSession, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving sessions for site")

	siteID, _, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	// Get active sessions (server-side filtering via Citrix-InstanceId header should handle site filtering)
	var allSessions []DDCSession
	continuationToken := ""

	for {
		endpoint := "/cvad/manage/Sessions"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}

		body, err := c.makeRequestWithSiteID(ctx, "GET", endpoint, nil, siteID)
		if err != nil {
			return nil, fmt.Errorf("failed to get sessions: %w", err)
		}

		var response DDCSessionsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse sessions response: %w", err)
		}

		// Add all sessions (server-side filtering via Citrix-InstanceId header should handle site filtering)
		allSessions = append(allSessions, response.Items...)

		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}

	c.logger.Debug().
		Str("site", siteName).
		Int("session_count", len(allSessions)).
		Msg("Retrieved sessions for site")

	return allSessions, nil
}

// GetSiteDetails retrieves detailed information about a specific site
func (c *deliveryControllerClient) GetSiteDetails(ctx context.Context, siteName string) (*DDCSiteDetails, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving site details")

	siteID, actualSiteName, err := c.getSiteInfo(ctx, siteName)
	if err != nil {
		return nil, err
	}

	siteInfo := &Site{
		Id:   siteID,
		Name: actualSiteName,
	}
	siteName = actualSiteName

	// Get detailed information
	machines, err := c.GetMachinesDetailedBySite(ctx, siteName)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get machines for site details")
		machines = []DDCMachine{}
	}

	deliveryGroups, err := c.GetDeliveryGroupsBySite(ctx, siteName)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get delivery groups for site details")
		deliveryGroups = []DDCDeliveryGroup{}
	}

	controllers, err := c.GetControllersBySite(ctx, siteName)
	if err != nil {
		c.logger.Debug().Err(err).Msg("Controllers endpoint not available for site details - using empty count")
		controllers = []DDCController{}
	}

	sessions, err := c.GetSessionsBySite(ctx, siteName)
	if err != nil {
		c.logger.Debug().Err(err).Msg("Sessions endpoint not available for site details - using empty count")
		sessions = []DDCSession{}
	}

	// Count registered machines
	registeredCount := 0
	for _, machine := range machines {
		if machine.RegistrationState == "Registered" {
			registeredCount++
		}
	}

	// Build site details
	details := &DDCSiteDetails{
		Site:               *siteInfo,
		TotalMachines:      len(machines),
		RegisteredMachines: registeredCount,
		ActiveSessions:     len(sessions),
		DeliveryGroups:     make([]string, len(deliveryGroups)),
		Controllers:        make([]string, len(controllers)),
	}

	for i, dg := range deliveryGroups {
		details.DeliveryGroups[i] = dg.Name
	}

	for i, ctrl := range controllers {
		details.Controllers[i] = ctrl.DNSName
	}

	c.logger.Debug().
		Str("site", siteName).
		Int("machines", details.TotalMachines).
		Int("registered", details.RegisteredMachines).
		Int("sessions", details.ActiveSessions).
		Msg("Retrieved site details")

	return details, nil
}

// GetMe retrieves current user information from Delivery Controller
func (c *deliveryControllerClient) GetMe(ctx context.Context) (*DDCMeResponse, error) {
	c.logger.Debug().Msg("Retrieving current user information from Delivery Controller")

	// CVAD REST API endpoint for current user (lowercase 'me')
	endpoint := "/cvad/manage/me"

	body, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get current user info: %w", err)
	}

	// Parse me response
	var meResp DDCMeResponse
	if err := json.Unmarshal(body, &meResp); err != nil {
		return nil, fmt.Errorf("failed to parse me response: %w", err)
	}

	// Extract first site info for logging
	var siteId, siteName string
	if len(meResp.Customers) > 0 && len(meResp.Customers[0].Sites) > 0 {
		siteId = meResp.Customers[0].Sites[0].Id
		siteName = meResp.Customers[0].Sites[0].Name
	}

	c.logger.Debug().
		Str("user_id", meResp.UserId).
		Str("display_name", meResp.DisplayName).
		Str("site_id", siteId).
		Str("site_name", siteName).
		Msg("Retrieved current user information")

	return &meResp, nil
}

// TestConnectivity tests the connection to the Delivery Controller
func (c *deliveryControllerClient) TestConnectivity(ctx context.Context) error {
	c.logger.Debug().Msg("Testing Delivery Controller connectivity")

	// Try to get a token
	if err := c.getToken(ctx); err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}

	// Try the /me endpoint as connectivity test
	_, err := c.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("API test failed: %w", err)
	}

	c.logger.Debug().Msg("Delivery Controller connectivity test successful")
	return nil
}
