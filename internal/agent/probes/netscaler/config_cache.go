package netscaler

import (
	"sync"
	"time"

	"github.com/citrix/adc-nitro-go/service"
	"senhub-agent.go/internal/agent/services/logger"
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
func (c *configCache) refresh(client *service.NitroClient) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	c.logger.Debug().Msg("Starting config cache refresh")

	// Fetch lbvserver configurations
	if err := c.fetchVServers(client); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch vServer configs")
		// Continue with other resources even if one fails
	}

	// Fetch service configurations
	if err := c.fetchServices(client); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch service configs")
	}

	// Fetch servicegroup configurations
	if err := c.fetchServiceGroups(client); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch servicegroup configs")
	}

	// Fetch SSL certificate configurations
	if err := c.fetchSSLCertKeys(client); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch SSL certkey configs")
	}

	// Fetch bindings (relationships)
	if err := c.fetchBindings(client); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to fetch bindings")
	}

	c.lastRefresh = time.Now()
	elapsed := time.Since(start)

	c.logger.Info().
		Int("vservers", len(c.vservers)).
		Int("services", len(c.services)).
		Int("servicegroups", len(c.servicegroups)).
		Int("sslcertkeys", len(c.sslcertkeys)).
		Int("bindings", len(c.vserverToServiceGroups)).
		Dur("elapsed_ms", elapsed).
		Msg("Config cache refreshed")

	return nil
}

// fetchVServers retrieves all lbvserver configurations
func (c *configCache) fetchVServers(client *service.NitroClient) error {
	resources, err := client.FindAllResources("lbvserver")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "name"); name != "" {
			c.vservers[name] = resource
		}
	}

	return nil
}

// fetchServices retrieves all service configurations
func (c *configCache) fetchServices(client *service.NitroClient) error {
	resources, err := client.FindAllResources("service")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "name"); name != "" {
			c.services[name] = resource
		}
	}

	return nil
}

// fetchServiceGroups retrieves all servicegroup configurations
func (c *configCache) fetchServiceGroups(client *service.NitroClient) error {
	resources, err := client.FindAllResources("servicegroup")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "servicegroupname"); name != "" {
			c.servicegroups[name] = resource
		}
	}

	return nil
}

// fetchSSLCertKeys retrieves all SSL certificate configurations
func (c *configCache) fetchSSLCertKeys(client *service.NitroClient) error {
	resources, err := client.FindAllResources("sslcertkey")
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if name := getString(resource, "certkey"); name != "" {
			c.sslcertkeys[name] = resource
		}
	}

	return nil
}

// fetchBindings retrieves binding relationships between resources
func (c *configCache) fetchBindings(client *service.NitroClient) error {
	// Clear existing bindings
	c.vserverToServiceGroups = make(map[string][]string)
	c.servicegroupToVServers = make(map[string][]string)
	c.servicegroupToServices = make(map[string][]string)

	// Fetch vServer → ServiceGroup bindings (one call for all bindings)
	vserverBindings, err := client.FindAllResources("lbvserver_servicegroup_binding")
	if err != nil {
		c.logger.Debug().Err(err).Msg("No lbvserver_servicegroup_binding found (may be normal if no bindings exist)")
	} else {
		for _, binding := range vserverBindings {
			vserver := getString(binding, "name")
			servicegroup := getString(binding, "servicegroupname")

			if vserver != "" && servicegroup != "" {
				c.vserverToServiceGroups[vserver] = append(c.vserverToServiceGroups[vserver], servicegroup)
				c.servicegroupToVServers[servicegroup] = append(c.servicegroupToVServers[servicegroup], vserver)
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
				c.servicegroupToServices[servicegroup] = append(c.servicegroupToServices[servicegroup], service)
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

// getServiceGroupConfig retrieves cached servicegroup configuration
func (c *configCache) getServiceGroupConfig(name string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servicegroups[name]
}

// getSSLCertKeyConfig retrieves cached SSL certificate configuration
func (c *configCache) getSSLCertKeyConfig(name string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sslcertkeys[name]
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

// getServicesForServiceGroup retrieves services that are members of a servicegroup
func (c *configCache) getServicesForServiceGroup(servicegroupName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servicegroupToServices[servicegroupName]
}

// getAllServiceGroups retrieves all cached servicegroups
func (c *configCache) getAllServiceGroups() map[string]map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.servicegroups
}

// needsRefresh checks if cache needs to be refreshed
func (c *configCache) needsRefresh() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.lastRefresh) > c.refreshInterval
}

// age returns the age of the cached data
func (c *configCache) age() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.lastRefresh)
}
