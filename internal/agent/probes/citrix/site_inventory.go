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
	SiteID             string
	SiteName           string
	Machines           map[string]DDCMachine       // MachineDNS → Machine (Registered only)
	AllMachines        map[string]DDCMachine       // MachineDNS → Machine (ALL states)
	MachinesByID       map[string]DDCMachine       // MachineID → Machine
	MachinesByState    map[string][]DDCMachine     // RegistrationState → []Machine  
	DeliveryGroups     map[string]DDCDeliveryGroup // GroupID → Group
	Controllers        map[string]bool             // ControllerDNS → exists
	Applications       map[string]DDCApplication   // AppID → Application
	LastUpdate         time.Time
	UpdateDuration     time.Duration
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
		SiteName:        siteFilter,
		Machines:        make(map[string]DDCMachine),
		AllMachines:     make(map[string]DDCMachine),
		MachinesByID:    make(map[string]DDCMachine),
		MachinesByState: make(map[string][]DDCMachine),
		DeliveryGroups:  make(map[string]DDCDeliveryGroup),
		Controllers:     make(map[string]bool),
		Applications:    make(map[string]DDCApplication),
		LastUpdate:      startTime,
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
		var validMachineCount int
		var excludedMachineCount int
		var totalMachineCount int
		
		for _, machine := range machines {
			totalMachineCount++
			
			// Index each machine only once with priority: DNSName > MachineName > Name
			var key string
			if machine.DNSName != "" {
				key = machine.DNSName
			} else if machine.MachineName != "" {
				key = machine.MachineName
			} else if machine.Name != "" {
				key = machine.Name
			}
			
			// Index ALL machines regardless of state
			if key != "" {
				newInventory.AllMachines[key] = machine
				newInventory.MachinesByID[machine.Id] = machine
				
				// Group by registration state
				state := machine.RegistrationState
				newInventory.MachinesByState[state] = append(newInventory.MachinesByState[state], machine)
				
				// Only include registered machines in the main collection (for backward compatibility)
				if machine.RegistrationState == "Registered" {
					validMachineCount++
					newInventory.Machines[key] = machine
				} else {
					excludedMachineCount++
					s.logger.Debug().
						Str("machine_dns", machine.DNSName).
						Str("machine_name", machine.MachineName).
						Str("registration_state", machine.RegistrationState).
						Str("site", siteFilter).
						Msg("📊 Machine stored in inventory but excluded from active filtering")
				}
			}

			// Also index by ID for session filtering
			if machine.Id != "" {
				newInventory.MachinesByID[machine.Id] = machine
			}
		}
		
		s.logger.Info().
			Int("total_cvad_machines", len(machines)).
			Int("all_machines_stored", len(newInventory.AllMachines)).
			Int("registered_machines", validMachineCount).
			Int("excluded_machines", excludedMachineCount).
			Str("site", siteFilter).
			Msg("🎯 Processed CVAD machines by registration state")

		s.logger.Info().
			Int("registered_indexed", len(newInventory.Machines)).
			Int("all_machines_indexed", len(newInventory.AllMachines)).
			Int("states_available", len(newInventory.MachinesByState)).
			Str("site", siteFilter).
			Msg("📦 Loaded complete machine inventory")
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
		s.logger.Debug().
			Err(err).
			Str("site", siteFilter).
			Msg("Controllers endpoint not available - skipping (inventory will still function)")
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
		s.logger.Debug().
			Err(err).
			Str("site", siteFilter).
			Msg("Applications endpoint not available - skipping (inventory will still function)")
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
// GetMachinesForSite returns DNS names of REGISTERED machines in the site for filtering
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
	
	s.logger.Info().
		Int("cached_machines", len(s.cache.Machines)).
		Int("returned_dns_names", len(machines)).
		Msg("🎯 Returned REGISTERED machine DNS names for operational filtering")
	
	return machines
}

// GetAllMachinesForSite returns DNS names of ALL machines in the site (including off/unregistered)
func (s *InventoryService) GetAllMachinesForSite() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return []string{}
	}

	machines := make([]string, 0, len(s.cache.AllMachines))
	for dns := range s.cache.AllMachines {
		machines = append(machines, dns)
	}
	
	s.logger.Info().
		Int("all_cached_machines", len(s.cache.AllMachines)).
		Int("returned_dns_names", len(machines)).
		Msg("🏭 Returned ALL machine DNS names for inventory filtering")
	
	return machines
}

// GetMachinesByRegistrationState returns DNS names of machines by their registration state
func (s *InventoryService) GetMachinesByRegistrationState(state string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cache == nil {
		return []string{}
	}

	machines := make([]string, 0)
	if stateGroup, exists := s.cache.MachinesByState[state]; exists {
		for _, machine := range stateGroup {
			if machine.DNSName != "" {
				machines = append(machines, machine.DNSName)
			} else if machine.MachineName != "" {
				machines = append(machines, machine.MachineName)
			} else if machine.Name != "" {
				machines = append(machines, machine.Name)
			}
		}
	}
	
	s.logger.Debug().
		Str("registration_state", state).
		Int("machines_found", len(machines)).
		Msg("🔍 Returned machines for specific registration state")
	
	return machines
}

// GetInventoryStats returns detailed statistics about the machine inventory
func (s *InventoryService) GetMachineInventoryStats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]int{
		"registered_machines":   0,
		"all_machines":         0,
		"unregistered_machines": 0,
		"agent_error_machines":  0,
		"unknown_state_machines": 0,
	}

	if s.cache == nil {
		return stats
	}

	stats["registered_machines"] = len(s.cache.Machines)
	stats["all_machines"] = len(s.cache.AllMachines)
	
	for state, machinesList := range s.cache.MachinesByState {
		switch state {
		case "Unregistered":
			stats["unregistered_machines"] = len(machinesList)
		case "AgentError":
			stats["agent_error_machines"] = len(machinesList)
		case "Registered":
			// Already counted above
		default:
			stats["unknown_state_machines"] += len(machinesList)
		}
	}

	return stats
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
