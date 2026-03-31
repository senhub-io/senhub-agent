// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"context"
	"fmt"
	"time"

	"github.com/citrix/adc-nitro-go/service"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// netscalerProbe implements monitoring for Citrix Netscaler (ADC) using official NITRO library
type netscalerProbe struct {
	*types.BaseProbe
	config     map[string]interface{}
	logger     *logger.ModuleLogger
	interval   time.Duration
	client     *service.NitroClient
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Configuration fields
	baseURL            string
	username           string
	password           string
	apiKey             string
	insecureSkipVerify bool
	timeout            int

	// Configuration cache for enriched tags
	cache      *configCache
	customTags []tags.Tag // User-defined tags from configuration

	// System identity (fetched at startup)
	hostname string // Netscaler hostname (e.g., "SRV0006")
	nodeID   int    // HA node ID (0 or 1, -1 if not HA)
}

// NewNetscalerProbe creates a new instance of the Netscaler probe
func NewNetscalerProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger for netscaler probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.netscaler")

	// Default interval: 60 seconds
	interval := 60 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	// Extract base configuration parameters
	baseURL, ok := config["base_url"].(string)
	if !ok || baseURL == "" {
		return nil, fmt.Errorf("netscaler probe requires 'base_url' configuration")
	}

	username, ok := config["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("netscaler probe requires 'username' configuration")
	}

	password, _ := config["password"].(string)
	apiKey, _ := config["api_key"].(string)

	// Either password or API key must be provided
	if password == "" && apiKey == "" {
		return nil, fmt.Errorf("netscaler probe requires either 'password' or 'api_key' configuration")
	}

	// Extract TLS configuration
	insecureSkipVerify := false
	if skip, ok := config["insecure_skip_verify"].(bool); ok {
		insecureSkipVerify = skip
	}

	// Extract timeout configuration (in seconds for NITRO client)
	timeout := 30
	if cfgTimeout, ok := config["timeout"].(int); ok {
		timeout = cfgTimeout
	}

	// Extract probe name from config
	probeName, ok := config["name"].(string)
	if !ok || probeName == "" {
		probeName = "netscaler-probe"
	}

	// Extract custom tags from configuration
	customTags := extractCustomTags(config)

	probe := &netscalerProbe{
		BaseProbe:          &types.BaseProbe{},
		config:             config,
		logger:             moduleLogger,
		interval:           interval,
		baseURL:            baseURL,
		username:           username,
		password:           password,
		apiKey:             apiKey,
		insecureSkipVerify: insecureSkipVerify,
		timeout:            timeout,
		customTags:         customTags,
		nodeID:             -1, // Default: not HA or unknown
	}

	// Initialize configuration cache (refresh every 5 minutes)
	probe.cache = newConfigCache(5*time.Minute, moduleLogger)

	// Initialize base probe using setter methods
	probe.SetName(probeName)
	probe.SetProbeType("netscaler")

	moduleLogger.Info().
		Str("base_url", baseURL).
		Str("username", username).
		Bool("api_key_auth", apiKey != "").
		Bool("insecure_skip_verify", insecureSkipVerify).
		Int("timeout", timeout).
		Int64("interval", int64(interval.Milliseconds())).
		Msg("Netscaler probe initialized")

	return probe, nil
}

// extractCustomTags extracts user-defined custom tags from configuration
func extractCustomTags(config map[string]interface{}) []tags.Tag {
	customTagsRaw, ok := config["custom_tags"].(map[string]interface{})
	if !ok || customTagsRaw == nil {
		return nil
	}

	var customTags []tags.Tag
	for key, value := range customTagsRaw {
		if strValue, ok := value.(string); ok {
			customTags = append(customTags, tags.Tag{
				Key:   key,
				Value: strValue,
			})
		}
	}

	return customTags
}

// GetInterval returns the collection interval for this probe
func (p *netscalerProbe) GetInterval() time.Duration {
	return p.interval
}

// ShouldStart returns whether this probe should be started
func (p *netscalerProbe) ShouldStart() bool {
	return true
}

// OnStart initializes the NITRO client and tests connectivity
func (p *netscalerProbe) OnStart(quitChannel chan struct{}) error {
	p.logger.Info().Msg("On start call")

	p.ctx, p.cancelFunc = context.WithCancel(context.Background())

	p.logger.Info().Msg("Starting Netscaler probe")

	// Create NITRO client using official library
	params := service.NitroParams{
		Url:       p.baseURL,
		Username:  p.username,
		Password:  p.password,
		SslVerify: !p.insecureSkipVerify,
		Timeout:   p.timeout,
		LogLevel:  "error", // Set to "debug" for troubleshooting
	}

	client, err := service.NewNitroClientFromParams(params)
	if err != nil {
		return fmt.Errorf("failed to create NITRO client: %w", err)
	}

	p.client = client

	// Test authentication
	if err := p.client.Login(); err != nil {
		return fmt.Errorf("failed to authenticate with Netscaler: %w", err)
	}

	p.logger.Info().Msg("Successfully authenticated with Netscaler")

	// Fetch system identity (hostname and HA node ID)
	if err := p.fetchSystemIdentity(); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to fetch system identity, will use defaults")
	} else {
		p.logger.Info().
			Str("hostname", p.hostname).
			Int("node_id", p.nodeID).
			Msg("Fetched system identity")
	}

	// Initial cache refresh
	p.logger.Info().Msg("Performing initial config cache refresh")
	if err := p.cache.refresh(p.client); err != nil {
		p.logger.Warn().Err(err).Msg("Initial cache refresh failed, will retry")
	}

	// Start background cache refresh goroutine
	go p.cacheRefreshLoop()

	return nil
}

// cacheRefreshLoop periodically refreshes the configuration cache
func (p *netscalerProbe) cacheRefreshLoop() {
	ticker := time.NewTicker(p.cache.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Info().Msg("Cache refresh loop stopped")
			return
		case <-ticker.C:
			if err := p.cache.refresh(p.client); err != nil {
				p.logger.Warn().Err(err).Msg("Periodic cache refresh failed")
			}
		}
	}
}

// OnShutdown cleans up resources
func (p *netscalerProbe) OnShutdown(ctx context.Context) error {
	p.logger.Info().Msg("OnShutdown call")
	p.logger.Info().Msg("Shutting down Netscaler probe")

	if p.cancelFunc != nil {
		p.cancelFunc()
	}

	if p.client != nil && p.client.IsLoggedIn() {
		if err := p.client.Logout(); err != nil {
			p.logger.Warn().Err(err).Msg("Failed to logout from Netscaler")
		} else {
			p.logger.Info().Msg("Successfully logged out from Netscaler")
		}
	}

	return nil
}

// Collect gathers metrics from the Netscaler and returns them as datapoints
func (p *netscalerProbe) Collect() ([]datapoint.DataPoint, error) {
	if p.client == nil {
		return nil, fmt.Errorf("netscaler client not initialized")
	}

	timestamp := time.Now()
	var datapoints []datapoint.DataPoint

	// Base tags for all metrics
	baseTags := []tags.Tag{
		{Key: "probe_name", Value: p.GetName()},
		{Key: "probe_type", Value: "netscaler"},
		{Key: "netscaler", Value: p.baseURL},
	}

	// Append custom tags from configuration
	baseTags = append(baseTags, p.customTags...)

	// Collect system stats
	if systemDP, err := p.collectSystemStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect system stats")
	} else {
		datapoints = append(datapoints, systemDP...)
	}

	// Collect NS stats
	if nsDP, err := p.collectNSStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect NS stats")
	} else {
		datapoints = append(datapoints, nsDP...)
	}

	// Collect LB VServer stats
	if lbDP, err := p.collectLBVServerStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect LB VServer stats")
	} else {
		datapoints = append(datapoints, lbDP...)
	}

	// Collect Service stats
	if svcDP, err := p.collectServiceStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect Service stats")
	} else {
		datapoints = append(datapoints, svcDP...)
	}

	// Collect SSL stats
	if sslDP, err := p.collectSSLStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect SSL stats")
	} else {
		datapoints = append(datapoints, sslDP...)
	}

	// Collect Service Group stats
	if sgDP, err := p.collectServiceGroupStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect Service Group stats")
	} else {
		datapoints = append(datapoints, sgDP...)
	}

	// Collect SSL Certificate expiration stats
	if certDP, err := p.collectSSLCertificateStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect SSL Certificate stats")
	} else {
		datapoints = append(datapoints, certDP...)
	}

	// Collect HA (High Availability) stats
	if haDP, err := p.collectHAStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect HA stats")
	} else {
		datapoints = append(datapoints, haDP...)
	}

	// Disk stats are already collected inside collectSystemStats (same NITRO "system" resource)

	// Collect Interface stats
	if ifaceDP, err := p.collectInterfaceStats(timestamp, baseTags); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect Interface stats")
	} else {
		datapoints = append(datapoints, ifaceDP...)
	}

	// Collect Content Switching vServer stats
	if csDP, err := p.collectContentSwitchingStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Content Switching not configured or not available")
	} else {
		datapoints = append(datapoints, csDP...)
	}

	// Collect Content Switching Policy stats
	if csPolicyDP, err := p.collectContentSwitchingPolicyStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Content Switching policies not configured or not available")
	} else {
		datapoints = append(datapoints, csPolicyDP...)
	}

	// Collect GSLB vServer stats
	if gslbVSDP, err := p.collectGSLBVServerStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("GSLB vServer not configured or not available")
	} else {
		datapoints = append(datapoints, gslbVSDP...)
	}

	// Collect GSLB Site stats
	if gslbSiteDP, err := p.collectGSLBSiteStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("GSLB Sites not configured or not available")
	} else {
		datapoints = append(datapoints, gslbSiteDP...)
	}

	// Collect GSLB Service stats
	if gslbSvcDP, err := p.collectGSLBServiceStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("GSLB Services not configured or not available")
	} else {
		datapoints = append(datapoints, gslbSvcDP...)
	}

	// Collect Cache stats
	if cacheDP, err := p.collectCacheStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Cache not configured or not available")
	} else {
		datapoints = append(datapoints, cacheDP...)
	}

	// Collect Compression stats
	if compressionDP, err := p.collectCompressionStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Compression not configured or not available")
	} else {
		datapoints = append(datapoints, compressionDP...)
	}

	// Collect AAA stats
	if aaaDP, err := p.collectAAAStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("AAA not configured or not available")
	} else {
		datapoints = append(datapoints, aaaDP...)
	}

	// Collect Authentication vServer stats
	if authVSDP, err := p.collectAuthenticationVServerStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Authentication vServers not configured or not available")
	} else {
		datapoints = append(datapoints, authVSDP...)
	}

	// Collect VPN/Gateway stats
	if vpnDP, err := p.collectVPNStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("VPN/Gateway not configured or not available")
	} else {
		datapoints = append(datapoints, vpnDP...)
	}

	// Collect Application Firewall stats
	if appfwDP, err := p.collectApplicationFirewallStats(timestamp, baseTags); err != nil {
		p.logger.Debug().Err(err).Msg("Application Firewall not configured or not available")
	} else {
		datapoints = append(datapoints, appfwDP...)
	}

	p.logger.Debug().Int("datapoint_count", len(datapoints)).Msg("Collected Netscaler metrics")

	return datapoints, nil
}

// fetchSystemIdentity retrieves the system hostname and HA node ID
// This is called once at probe startup to enrich HA metrics with node identification
func (p *netscalerProbe) fetchSystemIdentity() error {
	p.logger.Debug().Msg("Fetching system identity from /config/nshostname")

	// Fetch nshostname configuration
	// URL: /nitro/v1/config/nshostname
	// Returns: { "nshostname": [ { "hostname": "SRV0006", "ownernode": 0 }] }
	resources, err := p.client.FindAllResources("nshostname")
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to fetch nshostname, will try hanode instead")
		// Try alternative approach: fetch hanode config to determine our node ID
		return p.fetchSystemIdentityFromHANode()
	}

	if len(resources) == 0 {
		p.logger.Warn().Msg("No nshostname data returned, will try hanode instead")
		return p.fetchSystemIdentityFromHANode()
	}

	// Take the first entry (should only be one)
	hostnameData := resources[0]

	// DEBUG: Log the entire response to see what fields are present
	p.logger.Debug().Interface("nshostname_response", hostnameData).Msg("Received nshostname data")

	// Extract hostname (string)
	if hostname, ok := hostnameData["hostname"].(string); ok && hostname != "" {
		p.hostname = hostname
		p.logger.Debug().Str("hostname", hostname).Msg("Extracted hostname")
	} else {
		p.logger.Warn().Interface("hostnameData", hostnameData).Msg("Hostname not found or invalid type in nshostname response")
	}

	// Extract ownernode (HA node ID: 0 or 1)
	// This field only exists in HA configurations
	if ownernode, ok := hostnameData["ownernode"].(float64); ok {
		p.nodeID = int(ownernode)
		p.logger.Debug().Int("node_id", p.nodeID).Msg("Extracted node ID from ownernode field")
	} else {
		// Check if field exists but is different type
		if ownernodeRaw, exists := hostnameData["ownernode"]; exists {
			p.logger.Warn().
				Interface("ownernode_value", ownernodeRaw).
				Str("ownernode_type", fmt.Sprintf("%T", ownernodeRaw)).
				Msg("ownernode field exists but is not float64")
		} else {
			p.logger.Debug().Msg("ownernode field not present in nshostname response (not HA or standalone)")
		}
		// Not an HA configuration or field not present
		p.nodeID = -1
	}

	p.logger.Info().
		Str("hostname", p.hostname).
		Int("node_id", p.nodeID).
		Msg("System identity fetched from nshostname")

	return nil
}

// fetchSystemIdentityFromHANode attempts to get system identity from hanode config
// This is a fallback when nshostname doesn't provide the information
func (p *netscalerProbe) fetchSystemIdentityFromHANode() error {
	p.logger.Debug().Msg("Attempting to fetch system identity from /config/hanode")

	resources, err := p.client.FindAllResources("hanode")
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to fetch hanode config, assuming standalone node")
		p.nodeID = -1
		return nil // Not a fatal error - just means not HA
	}

	if len(resources) == 0 {
		p.logger.Debug().Msg("No hanode data returned, assuming standalone node")
		p.nodeID = -1
		return nil
	}

	p.logger.Debug().
		Int("hanode_count", len(resources)).
		Interface("hanode_data", resources).
		Msg("Received hanode configuration")

	// In HA setup, we need to identify which node we're connected to
	// The hanode with state="Primary" or "Secondary" and routemonitor="ENABLED" might be local
	// But safer approach: check all nodes and see if we can match by some criteria
	// For now, if we have hanode data, assume node 0 (will be corrected by actual HA stats)
	if len(resources) > 0 {
		// Try to find our node ID from the first entry
		if id, ok := resources[0]["id"].(float64); ok {
			p.nodeID = int(id)
			p.logger.Debug().Int("node_id", p.nodeID).Msg("Using first hanode ID as fallback")
		}
	}

	return nil
}
