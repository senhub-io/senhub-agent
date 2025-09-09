package citrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

// GetMachinesDetailedBySite retrieves detailed machine info for a specific site
func (c *deliveryControllerClient) GetMachinesDetailedBySite(ctx context.Context, siteName string) ([]DDCMachine, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving detailed machines for site")
	
	// First, get the site ID
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
	
	// Get machines with pagination support
	var allMachines []DDCMachine
	continuationToken := ""
	
	for {
		endpoint := "/cvad/manage/Machines"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}
		
		body, err := c.makeRequest(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get machines: %w", err)
		}
		
		var response DDCMachinesResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse machines response: %w", err)
		}
		
		// Filter machines by site
		for _, machine := range response.Items {
			if machine.SiteId == siteID {
				allMachines = append(allMachines, machine)
			}
		}
		
		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}
	
	c.logger.Info().
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
	
	// Get site ID first
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
	
	// Get delivery groups with pagination
	var allGroups []DDCDeliveryGroup
	continuationToken := ""
	
	for {
		endpoint := "/cvad/manage/DeliveryGroups"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}
		
		body, err := c.makeRequest(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get delivery groups: %w", err)
		}
		
		var response DDCDeliveryGroupsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse delivery groups response: %w", err)
		}
		
		// Filter by site
		for _, group := range response.Items {
			if group.SiteId == siteID {
				allGroups = append(allGroups, group)
			}
		}
		
		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}
	
	c.logger.Info().
		Str("site", siteName).
		Int("group_count", len(allGroups)).
		Msg("Retrieved delivery groups for site")
	
	return allGroups, nil
}

// GetApplicationsBySite retrieves all applications for a specific site
func (c *deliveryControllerClient) GetApplicationsBySite(ctx context.Context, siteName string) ([]DDCApplication, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving applications for site")
	
	// Get delivery groups for the site first
	deliveryGroups, err := c.GetDeliveryGroupsBySite(ctx, siteName)
	if err != nil {
		return nil, fmt.Errorf("failed to get delivery groups: %w", err)
	}
	
	// Create a map of delivery group IDs for this site
	siteDeliveryGroups := make(map[string]bool)
	for _, dg := range deliveryGroups {
		siteDeliveryGroups[dg.Id] = true
	}
	
	// Get all applications and filter by delivery group
	var allApps []DDCApplication
	continuationToken := ""
	
	for {
		endpoint := "/cvad/manage/Applications"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}
		
		body, err := c.makeRequest(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get applications: %w", err)
		}
		
		var response DDCApplicationsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse applications response: %w", err)
		}
		
		// Filter applications that belong to delivery groups in this site
		for _, app := range response.Items {
			for _, dgId := range app.DeliveryGroupIds {
				if siteDeliveryGroups[dgId] {
					allApps = append(allApps, app)
					break
				}
			}
		}
		
		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}
	
	c.logger.Info().
		Str("site", siteName).
		Int("app_count", len(allApps)).
		Msg("Retrieved applications for site")
	
	return allApps, nil
}

// GetControllersBySite retrieves all controllers for a specific site
func (c *deliveryControllerClient) GetControllersBySite(ctx context.Context, siteName string) ([]DDCController, error) {
	c.logger.Debug().
		Str("site", siteName).
		Msg("Retrieving controllers for site")
	
	// Get site ID
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
	
	// Get controllers
	endpoint := "/cvad/manage/Controllers"
	body, err := c.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get controllers: %w", err)
	}
	
	var response DDCControllersResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse controllers response: %w", err)
	}
	
	// Filter controllers by site
	var siteControllers []DDCController
	for _, controller := range response.Items {
		if controller.SiteId == siteID {
			siteControllers = append(siteControllers, controller)
		}
	}
	
	c.logger.Info().
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
	
	// Get machines for the site to filter sessions
	machines, err := c.GetMachinesDetailedBySite(ctx, siteName)
	if err != nil {
		return nil, fmt.Errorf("failed to get machines: %w", err)
	}
	
	// Create a map of machine IDs for this site
	siteMachines := make(map[string]bool)
	for _, machine := range machines {
		siteMachines[machine.Id] = true
	}
	
	// Get active sessions
	var allSessions []DDCSession
	continuationToken := ""
	
	for {
		endpoint := "/cvad/manage/Sessions"
		if continuationToken != "" {
			endpoint = fmt.Sprintf("%s?ContinuationToken=%s", endpoint, url.QueryEscape(continuationToken))
		}
		
		body, err := c.makeRequest(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get sessions: %w", err)
		}
		
		var response DDCSessionsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse sessions response: %w", err)
		}
		
		// Filter sessions by machine
		for _, session := range response.Items {
			if siteMachines[session.MachineId] {
				allSessions = append(allSessions, session)
			}
		}
		
		if response.ContinuationToken == "" {
			break
		}
		continuationToken = response.ContinuationToken
	}
	
	c.logger.Info().
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
	
	// Get basic site info
	sites, err := c.GetSites(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get sites: %w", err)
	}
	
	var siteInfo *Site
	for _, site := range sites {
		if site.Name == siteName {
			siteInfo = &site
			break
		}
	}
	
	if siteInfo == nil {
		return nil, fmt.Errorf("site %s not found", siteName)
	}
	
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
		c.logger.Warn().Err(err).Msg("Failed to get controllers for site details")
		controllers = []DDCController{}
	}
	
	sessions, err := c.GetSessionsBySite(ctx, siteName)
	if err != nil {
		c.logger.Warn().Err(err).Msg("Failed to get sessions for site details")
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
	
	c.logger.Info().
		Str("site", siteName).
		Int("machines", details.TotalMachines).
		Int("registered", details.RegisteredMachines).
		Int("sessions", details.ActiveSessions).
		Msg("Retrieved site details")
	
	return details, nil
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