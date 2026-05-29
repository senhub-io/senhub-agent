package netscaler

import (
	"sync"
	"time"

	"github.com/citrix/adc-nitro-go/service"
	"senhub-agent.go/probesdk/logger"
)

// configCache stores Netscaler configuration data to enrich metrics with contextual tags
// This avoids N+1 API calls by caching configs and bindings
type configCache struct {
	// Resource configurations
	vservers      map[string]map[string]interface{} // lbvserver configs by name
	services      map[string]map[string]interface{} // service configs by name
	servicegroups map[string]map[string]interface{} // servicegroup configs by name
	sslcertkeys   map[string]map[string]interface{} // sslcertkey configs by certkey name

	// Bindings (relationships)
	vserverToServiceGroups map[string][]string // vserver name → servicegroup names
	servicegroupToVServers map[string][]string // servicegroup name → vserver names
	servicegroupToServices map[string][]string // servicegroup name → service names

	// Cache metadata
	lastRefresh     time.Time
	refreshInterval time.Duration
	mu              sync.RWMutex
	logger          *logger.ModuleLogger
}

// newConfigCache creates a new configuration cache
func newConfigCache(refreshInterval time.Duration, logger *logger.ModuleLogger) *configCache {
	return &configCache{
		vservers:               make(map[string]map[string]interface{}),
		services:               make(map[string]map[string]interface{}),
		servicegroups:          make(map[string]map[string]interface{}),
		sslcertkeys:            make(map[string]map[string]interface{}),
		vserverToServiceGroups: make(map[string][]string),
		servicegroupToVServers: make(map[string][]string),
		servicegroupToServices: make(map[string][]string),
		refreshInterval:        refreshInterval,
		logger:                 logger,
	}
}

// refresh updates the cache with latest configurations from Netscaler
// This function minimizes lock duration by fetching data WITHOUT holding the lock,
// then performing a quick atomic swap with the lock held.
func (c *configCache) refresh(client *service.NitroClient) error {
	start := time.Now()
	c.logger.Debug().Msg("Starting config cache refresh")

	// Step 1: Fetch all data WITHOUT holding the lock (I/O operations)
	// This prevents blocking readers during potentially slow network calls
	newVServers := make(map[string]map[string]interface{})
	newServices := make(map[string]map[string]interface{})
	newServiceGroups := make(map[string]map[string]interface{})
	newSSLCertKeys := make(map[string]map[string]interface{})
	newVServerToServiceGroups := make(map[string][]string)
	newServiceGroupToVServers := make(map[string][]string)
	newServiceGroupToServices := make(map[string][]string)

	// Fetch lbvserver configurations
	if err := c.fetchVServersInto(client, newVServers); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch vServer configs")
		// Continue with other resources even if one fails
	}

	// Fetch service configurations
	if err := c.fetchServicesInto(client, newServices); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch service configs")
	}

	// Fetch servicegroup configurations
	if err := c.fetchServiceGroupsInto(client, newServiceGroups); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch servicegroup configs")
	}

	// Fetch SSL certificate configurations
	if err := c.fetchSSLCertKeysInto(client, newSSLCertKeys); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch SSL certkey configs")
	}

	// Fetch bindings (relationships)
	if err := c.fetchBindingsInto(client, newVServerToServiceGroups, newServiceGroupToVServers, newServiceGroupToServices); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch bindings")
	}

	// Step 2: Atomically swap cached data WITH lock held (fast in-memory operation)
	// This minimizes lock contention - only the pointer swap is locked
	c.mu.Lock()
	c.vservers = newVServers
	c.services = newServices
	c.servicegroups = newServiceGroups
	c.sslcertkeys = newSSLCertKeys
	c.vserverToServiceGroups = newVServerToServiceGroups
	c.servicegroupToVServers = newServiceGroupToVServers
	c.servicegroupToServices = newServiceGroupToServices
	c.lastRefresh = time.Now()
	c.mu.Unlock()

	elapsed := time.Since(start)

	c.logger.Info().
		Int("vservers", len(newVServers)).
		Int("services", len(newServices)).
		Int("servicegroups", len(newServiceGroups)).
		Int("sslcertkeys", len(newSSLCertKeys)).
		Int("bindings", len(newVServerToServiceGroups)).
		Dur("elapsed_ms", elapsed).
		Msg("Config cache refreshed")

	return nil
}

// fetchVServersInto retrieves all lbvserver configurations into the provided map
func (c *configCache) fetchVServersInto(client *service.NitroClient, dest map[string]map[string]interface{}) error {
	resources, err := client.FindAllResources("lbvserver")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "name"); name != "" {
			dest[name] = resource
		}
	}

	return nil
}

// fetchServicesInto retrieves all service configurations into the provided map
func (c *configCache) fetchServicesInto(client *service.NitroClient, dest map[string]map[string]interface{}) error {
	resources, err := client.FindAllResources("service")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "name"); name != "" {
			dest[name] = resource
		}
	}

	return nil
}

// fetchServiceGroupsInto retrieves all servicegroup configurations into the provided map
func (c *configCache) fetchServiceGroupsInto(client *service.NitroClient, dest map[string]map[string]interface{}) error {
	resources, err := client.FindAllResources("servicegroup")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "servicegroupname"); name != "" {
			dest[name] = resource
		}
	}

	return nil
}

// fetchSSLCertKeysInto retrieves all SSL certificate configurations into the provided map
func (c *configCache) fetchSSLCertKeysInto(client *service.NitroClient, dest map[string]map[string]interface{}) error {
	resources, err := client.FindAllResources("sslcertkey")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "certkey"); name != "" {
			dest[name] = resource
		}
	}

	return nil
}

// fetchBindingsInto retrieves binding relationships between resources into the provided maps
func (c *configCache) fetchBindingsInto(
	client *service.NitroClient,
	vserverToSG map[string][]string,
	sgToVServer map[string][]string,
	sgToService map[string][]string,
) error {
	// Fetch vServer → ServiceGroup bindings (one call for all bindings)
	vserverBindings, err := client.FindAllResources("lbvserver_servicegroup_binding")
	if err != nil {
		c.logger.Debug().Err(err).Msg("No lbvserver_servicegroup_binding found (may be normal if no bindings exist)")
	} else {
		for _, binding := range vserverBindings {
			vserver := getString(binding, "name")
			servicegroup := getString(binding, "servicegroupname")

			if vserver != "" && servicegroup != "" {
				vserverToSG[vserver] = append(vserverToSG[vserver], servicegroup)
				sgToVServer[servicegroup] = append(sgToVServer[servicegroup], vserver)
			}
		}
	}

	// Fetch ServiceGroup → Service bindings (one call for all bindings)
	servicegroupBindings, err := client.FindAllResources("servicegroup_servicegroupmember_binding")
	if err != nil {
		c.logger.Debug().Err(err).Msg("No servicegroup_servicegroupmember_binding found (may be normal if no members)")
	} else {
		for _, binding := range servicegroupBindings {
			servicegroup := getString(binding, "servicegroupname")
			service := getString(binding, "servername")

			if servicegroup != "" && service != "" {
				sgToService[servicegroup] = append(sgToService[servicegroup], service)
			}
		}
	}

	return nil
}

// getVServerConfig retrieves cached vServer configuration
func (c *configCache) getVServerConfig(name string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vservers[name]
}

// getServiceConfig retrieves cached service configuration
func (c *configCache) getServiceConfig(name string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.services[name]
}

// getAllSSLCertKeys retrieves all cached SSL certificates
func (c *configCache) getAllSSLCertKeys() map[string]map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid concurrent access issues
	result := make(map[string]map[string]interface{}, len(c.sslcertkeys))
	for k, v := range c.sslcertkeys {
		result[k] = v
	}
	return result
}

// getServiceGroupsForVServer retrieves servicegroups bound to a vServer
func (c *configCache) getServiceGroupsForVServer(vserverName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vserverToServiceGroups[vserverName]
}

// getVServersForServiceGroup retrieves vServers that use a servicegroup
func (c *configCache) getVServersForServiceGroup(servicegroupName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servicegroupToVServers[servicegroupName]
}
