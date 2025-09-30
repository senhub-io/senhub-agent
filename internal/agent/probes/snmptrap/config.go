package snmptrap

import (
	"fmt"
	"time"
)

// Config holds the configuration for the SNMP Trap probe
type Config struct {
	// Network configuration
	ListenAddress string   `json:"listen_address" yaml:"listen_address" default:"0.0.0.0:162"`
	BufferSize    int      `json:"buffer_size" yaml:"buffer_size" default:"1000"`
	Timeout       int      `json:"timeout" yaml:"timeout" default:"30"`
	
	// Authentication (SNMPv1/v2c)
	Communities []string `json:"communities" yaml:"communities"`
	
	// MIB enrichment configuration
	MIBEnrichment MIBConfig `json:"mib_enrichment" yaml:"mib_enrichment"`
	
	// Filtering configuration
	Filters FilterConfig `json:"filters" yaml:"filters"`
	
	// Severity mapping
	SeverityMapping map[string]string `json:"severity_mapping" yaml:"severity_mapping"`
	
	// Message templates per vendor
	MessageTemplates map[string]map[string]string `json:"message_templates" yaml:"message_templates"`
}

// MIBConfig holds MIB enrichment configuration
type MIBConfig struct {
	Enabled               bool     `json:"enabled" yaml:"enabled" default:"true"`
	ExternalMIBsPath      string   `json:"external_mibs_path" yaml:"external_mibs_path"`
	CacheSize             int      `json:"cache_size" yaml:"cache_size" default:"10000"`
	CacheTTL              string   `json:"cache_ttl" yaml:"cache_ttl" default:"24h"`
	MIBPaths              []string `json:"mib_paths" yaml:"mib_paths"`
	AutoLoadVendorMIBs    bool     `json:"auto_load_vendor_mibs" yaml:"auto_load_vendor_mibs" default:"true"`
	ResolveIndexes        bool     `json:"resolve_indexes" yaml:"resolve_indexes" default:"true"`
	ResolveEnums          bool     `json:"resolve_enums" yaml:"resolve_enums" default:"true"`
	ResolveUnits          bool     `json:"resolve_units" yaml:"resolve_units" default:"true"`
	GenerateHumanReadable bool     `json:"generate_human_readable" yaml:"generate_human_readable" default:"true"`
	AutoSeverityMapping   bool     `json:"auto_severity_mapping" yaml:"auto_severity_mapping" default:"true"`
	AutoMessageGeneration bool     `json:"auto_message_generation" yaml:"auto_message_generation" default:"true"`
}

// FilterConfig holds filtering configuration
type FilterConfig struct {
	AllowedSources   []string        `json:"allowed_sources" yaml:"allowed_sources"`
	BlockedSources   []string        `json:"blocked_sources" yaml:"blocked_sources"`
	AllowedEnterprises []string      `json:"allowed_enterprises" yaml:"allowed_enterprises"`
	RateLimit        RateLimitConfig `json:"rate_limit" yaml:"rate_limit"`
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	MaxTrapsPerMinute int            `json:"max_traps_per_minute" yaml:"max_traps_per_minute" default:"200"`
	PerSourceLimit    int            `json:"per_source_limit" yaml:"per_source_limit" default:"50"`
	BurstLimit        int            `json:"burst_limit" yaml:"burst_limit" default:"100"`
	CriticalSources   map[string]int `json:"critical_sources" yaml:"critical_sources"`
}

// parseConfig parses the probe configuration from parameters
func parseConfig(params interface{}) (*Config, error) {
	config := &Config{
		ListenAddress: "0.0.0.0:162",
		BufferSize:    1000,
		Timeout:       30,
		Communities:   []string{"public"},
		MIBEnrichment: MIBConfig{
			Enabled:               true,
			CacheSize:             10000,
			CacheTTL:              "24h",
			AutoLoadVendorMIBs:    true,
			ResolveIndexes:        true,
			ResolveEnums:          true,
			ResolveUnits:          true,
			GenerateHumanReadable: true,
			AutoSeverityMapping:   true,
			AutoMessageGeneration: true,
		},
		Filters: FilterConfig{
			RateLimit: RateLimitConfig{
				MaxTrapsPerMinute: 200,
				PerSourceLimit:    50,
				BurstLimit:        100,
			},
		},
	}
	
	// Parse params map
	if paramsMap, ok := params.(map[string]interface{}); ok {
		// Listen address
		if addr, ok := paramsMap["listen_address"].(string); ok {
			config.ListenAddress = addr
		}
		
		// Buffer size
		if size, ok := paramsMap["buffer_size"].(float64); ok {
			config.BufferSize = int(size)
		} else if size, ok := paramsMap["buffer_size"].(int); ok {
			config.BufferSize = size
		}
		
		// Communities
		if communities, ok := paramsMap["communities"].([]interface{}); ok {
			config.Communities = make([]string, 0, len(communities))
			for _, c := range communities {
				if community, ok := c.(string); ok {
					config.Communities = append(config.Communities, community)
				}
			}
		}
		
		// MIB enrichment
		if mibConfig, ok := paramsMap["mib_enrichment"].(map[string]interface{}); ok {
			parseMIBConfig(&config.MIBEnrichment, mibConfig)
		}
		
		// Filters
		if filterConfig, ok := paramsMap["filters"].(map[string]interface{}); ok {
			parseFilterConfig(&config.Filters, filterConfig)
		}
		
		// Severity mapping
		if severityMap, ok := paramsMap["severity_mapping"].(map[string]interface{}); ok {
			config.SeverityMapping = make(map[string]string)
			for oid, severity := range severityMap {
				if sev, ok := severity.(string); ok {
					config.SeverityMapping[oid] = sev
				}
			}
		}
		
		// Message templates
		if templates, ok := paramsMap["message_templates"].(map[string]interface{}); ok {
			config.MessageTemplates = make(map[string]map[string]string)
			for vendor, vendorTemplates := range templates {
				if vendorMap, ok := vendorTemplates.(map[string]interface{}); ok {
					config.MessageTemplates[vendor] = make(map[string]string)
					for oid, template := range vendorMap {
						if tmpl, ok := template.(string); ok {
							config.MessageTemplates[vendor][oid] = tmpl
						}
					}
				}
			}
		}
	}
	
	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	
	// Set default MIB paths if not specified
	if len(config.MIBEnrichment.MIBPaths) == 0 {
		config.MIBEnrichment.MIBPaths = []string{
			"embedded", // Embedded MIBs
			"/etc/senhub/mibs/custom",
			"/etc/senhub/mibs/updated",
			"/usr/share/snmp/mibs",
			"./custom_mibs",
		}
	}
	
	// Set default severity mapping if not specified
	if config.SeverityMapping == nil {
		config.SeverityMapping = defaultSeverityMapping()
	}
	
	return config, nil
}

// parseMIBConfig parses MIB configuration
func parseMIBConfig(config *MIBConfig, params map[string]interface{}) {
	if enabled, ok := params["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	
	if externalMIBsPath, ok := params["external_mibs_path"].(string); ok {
		config.ExternalMIBsPath = externalMIBsPath
	}
	
	if cacheSize, ok := params["cache_size"].(float64); ok {
		config.CacheSize = int(cacheSize)
	} else if cacheSize, ok := params["cache_size"].(int); ok {
		config.CacheSize = cacheSize
	}
	
	if cacheTTL, ok := params["cache_ttl"].(string); ok {
		config.CacheTTL = cacheTTL
	}
	
	if paths, ok := params["mib_paths"].([]interface{}); ok {
		config.MIBPaths = make([]string, 0, len(paths))
		for _, p := range paths {
			if path, ok := p.(string); ok {
				config.MIBPaths = append(config.MIBPaths, path)
			}
		}
	}
	
	if autoLoad, ok := params["auto_load_vendor_mibs"].(bool); ok {
		config.AutoLoadVendorMIBs = autoLoad
	}
	
	if resolveIndexes, ok := params["resolve_indexes"].(bool); ok {
		config.ResolveIndexes = resolveIndexes
	}
}

// parseFilterConfig parses filter configuration
func parseFilterConfig(config *FilterConfig, params map[string]interface{}) {
	if allowed, ok := params["allowed_sources"].([]interface{}); ok {
		config.AllowedSources = make([]string, 0, len(allowed))
		for _, s := range allowed {
			if source, ok := s.(string); ok {
				config.AllowedSources = append(config.AllowedSources, source)
			}
		}
	}
	
	if blocked, ok := params["blocked_sources"].([]interface{}); ok {
		config.BlockedSources = make([]string, 0, len(blocked))
		for _, s := range blocked {
			if source, ok := s.(string); ok {
				config.BlockedSources = append(config.BlockedSources, source)
			}
		}
	}
	
	if enterprises, ok := params["allowed_enterprises"].([]interface{}); ok {
		config.AllowedEnterprises = make([]string, 0, len(enterprises))
		for _, e := range enterprises {
			if enterprise, ok := e.(string); ok {
				config.AllowedEnterprises = append(config.AllowedEnterprises, enterprise)
			}
		}
	}
	
	if rateLimit, ok := params["rate_limit"].(map[string]interface{}); ok {
		if maxPerMin, ok := rateLimit["max_traps_per_minute"].(float64); ok {
			config.RateLimit.MaxTrapsPerMinute = int(maxPerMin)
		} else if maxPerMin, ok := rateLimit["max_traps_per_minute"].(int); ok {
			config.RateLimit.MaxTrapsPerMinute = maxPerMin
		}
		
		if perSource, ok := rateLimit["per_source_limit"].(float64); ok {
			config.RateLimit.PerSourceLimit = int(perSource)
		} else if perSource, ok := rateLimit["per_source_limit"].(int); ok {
			config.RateLimit.PerSourceLimit = perSource
		}
	}
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	if config.BufferSize < 100 {
		return fmt.Errorf("buffer_size must be at least 100")
	}
	
	if config.BufferSize > 100000 {
		return fmt.Errorf("buffer_size cannot exceed 100000")
	}
	
	if len(config.Communities) == 0 {
		config.Communities = []string{"public"}
	}
	
	if config.MIBEnrichment.CacheSize < 100 {
		config.MIBEnrichment.CacheSize = 100
	}
	
	// Validate cache TTL
	if config.MIBEnrichment.CacheTTL != "" {
		if _, err := time.ParseDuration(config.MIBEnrichment.CacheTTL); err != nil {
			return fmt.Errorf("invalid cache_ttl format: %w", err)
		}
	}
	
	return nil
}

// defaultSeverityMapping returns the default severity mapping
func defaultSeverityMapping() map[string]string {
	return map[string]string{
		// Standard SNMP traps
		"1.3.6.1.6.3.1.1.5.1": "info",      // coldStart
		"1.3.6.1.6.3.1.1.5.2": "warning",   // warmStart
		"1.3.6.1.6.3.1.1.5.3": "critical",  // linkDown
		"1.3.6.1.6.3.1.1.5.4": "info",      // linkUp
		"1.3.6.1.6.3.1.1.5.5": "major",     // authenticationFailure
		"1.3.6.1.6.3.1.1.5.6": "warning",   // egpNeighborLoss
		
		// Default
		"default": "info",
	}
}