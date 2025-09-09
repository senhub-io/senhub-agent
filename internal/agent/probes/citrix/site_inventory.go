package citrix

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// SiteInventory holds cached information about a Citrix site
type SiteInventory struct {
	SiteID         string
	SiteName       string
	Machines       map[string]DDCMachine       // MachineDNS → Machine
	MachinesByID   map[string]DDCMachine       // MachineID → Machine
	DeliveryGroups map[string]DDCDeliveryGroup // GroupID → Group
	Controllers    map[string]bool             // ControllerDNS → exists
	Applications   map[string]DDCApplication   // AppID → Application
	LastUpdate     time.Time
	UpdateDuration time.Duration
}

// InventoryService manages the site inventory cache
type InventoryService struct {
	ddcClient DeliveryControllerClient
	cache     *SiteInventory
	cacheTTL  time.Duration
	logger    *logger.ModuleLogger
	stopChan  chan struct{}
	mu        sync.RWMutex
}

// NewInventoryService creates a new inventory service
func NewInventoryService(ddcClient DeliveryControllerClient, cacheTTL time.Duration, baseLogger *logger.Logger) *InventoryService {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.inventory")

	return &InventoryService{
		ddcClient: ddcClient,
		cacheTTL:  cacheTTL,
		logger:    moduleLogger,
		stopChan:  make(chan struct{}),
	}
}

// RefreshInventory loads or refreshes the inventory for a specific site
func (s *InventoryService) RefreshInventory(ctx context.Context, siteFilter string) error {
	s.logger.Info().
		Str("site", siteFilter).
		Msg("Refreshing site inventory")

	startTime := time.Now()

	// Create new inventory
	newInventory := &SiteInventory{
		SiteName:       siteFilter,
		Machines:       make(map[string]DDCMachine),
		MachinesByID:   make(map[string]DDCMachine),
		DeliveryGroups: make(map[string]DDCDeliveryGroup),
		Controllers:    make(map[string]bool),
		Applications:   make(map[string]DDCApplication),
		LastUpdate:     startTime,
	}

	// Get site details first
	siteDetails, err := s.ddcClient.GetSiteDetails(ctx, siteFilter)
	if err != nil {
		return fmt.Errorf("failed to get site details: %w", err)
	}

	newInventory.SiteID = siteDetails.Site.Id

	// Load machines
	machines, err := s.ddcClient.GetMachinesDetailedBySite(ctx, siteFilter)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("site", siteFilter).
			Msg("Failed to get machines for inventory")
	} else {
		for _, machine := range machines {
			// Index by DNS name
			if machine.DNSName != "" {
				newInventory.Machines[machine.DNSName] = machine
			} else if machine.MachineName != "" {
				newInventory.Machines[machine.MachineName] = machine
			} else if machine.Name != "" {
				newInventory.Machines[machine.Name] = machine
			}

			// Also index by ID for session filtering
			if machine.Id != "" {
				newInventory.MachinesByID[machine.Id] = machine
			}
		}

		s.logger.Debug().
			Int("machine_count", len(machines)).
			Str("site", siteFilter).
			Msg("Loaded machines into inventory")
	}

	// Load delivery groups
	deliveryGroups, err := s.ddcClient.GetDeliveryGroupsBySite(ctx, siteFilter)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("site", siteFilter).
			Msg("Failed to get delivery groups for inventory")
	} else {
		for _, dg := range deliveryGroups {
			newInventory.DeliveryGroups[dg.Id] = dg
		}

		s.logger.Debug().
			Int("delivery_group_count", len(deliveryGroups)).
			Str("site", siteFilter).
			Msg("Loaded delivery groups into inventory")
	}

	// Load controllers
	controllers, err := s.ddcClient.GetControllersBySite(ctx, siteFilter)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("site", siteFilter).
			Msg("Failed to get controllers for inventory")
	} else {
		for _, ctrl := range controllers {
			if ctrl.DNSName != "" {
				newInventory.Controllers[ctrl.DNSName] = true
			}
		}

		s.logger.Debug().
			Int("controller_count", len(controllers)).
			Str("site", siteFilter).
			Msg("Loaded controllers into inventory")
	}

	// Load applications
	applications, err := s.ddcClient.GetApplicationsBySite(ctx, siteFilter)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("site", siteFilter).
			Msg("Failed to get applications for inventory")
	} else {
		for _, app := range applications {
			newInventory.Applications[app.Id] = app
		}

		s.logger.Debug().
			Int("application_count", len(applications)).
			Str("site", siteFilter).
			Msg("Loaded applications into inventory")
	}

	newInventory.UpdateDuration = time.Since(startTime)

	// Update cache atomically
	s.mu.Lock()
	s.cache = newInventory
	s.mu.Unlock()

	s.logger.Info().
		Str("site", siteFilter).
		Int("machines", len(newInventory.Machines)).
		Int("delivery_groups", len(newInventory.DeliveryGroups)).
		Int("controllers", len(newInventory.Controllers)).
		Int("applications", len(newInventory.Applications)).
		Dur("duration", newInventory.UpdateDuration).
		Msg("Site inventory refreshed successfully")

	return nil
}

// StartPeriodicRefresh starts automatic inventory refresh
func (s *InventoryService) StartPeriodicRefresh(ctx context.Context, interval time.Duration, siteFilter string) {
	s.logger.Info().
		Dur("interval", interval).
		Str("site", siteFilter).
		Msg("Starting periodic inventory refresh")

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := s.RefreshInventory(ctx, siteFilter); err != nil {
					s.logger.Error().
						Err(err).
						Msg("Failed to refresh inventory")
				}
			case <-ctx.Done():
				ticker.Stop()
				s.logger.Info().Msg("Stopping periodic inventory refresh")
				return
			case <-s.stopChan:
				ticker.Stop()
				s.logger.Info().Msg("Inventory refresh stopped")
				return
			}
		}
	}()
}

// Stop stops the periodic refresh
func (s *InventoryService) Stop() {
	close(s.stopChan)
}

// IsInSite checks if a machine DNS name belongs to the cached site
func (s *InventoryService) IsInSite(machineDNS string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return false
	}

	_, exists := s.cache.Machines[machineDNS]
	return exists
}

// IsMachineIDInSite checks if a machine ID belongs to the cached site
func (s *InventoryService) IsMachineIDInSite(machineID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return false
	}

	_, exists := s.cache.MachinesByID[machineID]
	return exists
}

// IsDeliveryGroupInSite checks if a delivery group belongs to the cached site
func (s *InventoryService) IsDeliveryGroupInSite(deliveryGroupID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return false
	}

	_, exists := s.cache.DeliveryGroups[deliveryGroupID]
	return exists
}

// IsControllerInSite checks if a controller belongs to the cached site
func (s *InventoryService) IsControllerInSite(controllerDNS string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return false
	}

	return s.cache.Controllers[controllerDNS]
}

// GetDeliveryGroupsForSite returns all delivery group IDs for the cached site
func (s *InventoryService) GetDeliveryGroupsForSite() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return []string{}
	}

	groups := make([]string, 0, len(s.cache.DeliveryGroups))
	for id := range s.cache.DeliveryGroups {
		groups = append(groups, id)
	}
	return groups
}

// GetControllersForSite returns all controller DNS names for the cached site
func (s *InventoryService) GetControllersForSite() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return []string{}
	}

	controllers := make([]string, 0, len(s.cache.Controllers))
	for dns := range s.cache.Controllers {
		controllers = append(controllers, dns)
	}
	return controllers
}

// GetMachinesForSite returns all machine DNS names for the cached site
func (s *InventoryService) GetMachinesForSite() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return []string{}
	}

	machines := make([]string, 0, len(s.cache.Machines))
	for dns := range s.cache.Machines {
		machines = append(machines, dns)
	}
	return machines
}

// GetInventoryStats returns statistics about the cached inventory
func (s *InventoryService) GetInventoryStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return map[string]interface{}{
			"cached": false,
		}
	}

	return map[string]interface{}{
		"cached":          true,
		"site_name":       s.cache.SiteName,
		"site_id":         s.cache.SiteID,
		"machine_count":   len(s.cache.Machines),
		"delivery_groups": len(s.cache.DeliveryGroups),
		"controllers":     len(s.cache.Controllers),
		"applications":    len(s.cache.Applications),
		"last_update":     s.cache.LastUpdate,
		"update_duration": s.cache.UpdateDuration,
		"cache_age":       time.Since(s.cache.LastUpdate),
	}
}

// IsStale checks if the cache is older than TTL
func (s *InventoryService) IsStale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return true
	}

	return time.Since(s.cache.LastUpdate) > s.cacheTTL
}

// GetSiteInfo returns basic site information from cache
func (s *InventoryService) GetSiteInfo() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return "", ""
	}

	return s.cache.SiteID, s.cache.SiteName
}
