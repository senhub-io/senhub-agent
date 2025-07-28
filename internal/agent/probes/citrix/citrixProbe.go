// Package citrix provides monitoring capabilities for Citrix Virtual Apps and Desktops via OData API
package citrix

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/services/logger"
)

// citrixProbe implements monitoring for Citrix Virtual Apps and Desktops using OData API
type citrixProbe struct {
	*types.BaseProbe
	config          map[string]interface{}
	logger          *logger.ModuleLogger
	interval        time.Duration
	client          CitrixClient
	ddcClient       DeliveryControllerClient
	metricsCollector *MetricsCollector
	ctx             context.Context
	cancelFunc      context.CancelFunc

	// Configuration fields
	baseURL             string
	environment         string
	authMethod          string
	username            string
	password            string
	verifySSL           bool
	timeout             time.Duration
	maxRetryAttempts    int
	retryBackoffFactor  float64
	
	// Delivery Controller configuration
	ddcConfig           *DeliveryControllerConfig
	siteFilter          string
	filteredMachines    []string
}

// NewCitrixProbe creates a new instance of the Citrix probe
func NewCitrixProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger for citrix probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix")
	
	// Default interval: 2 minutes as specified in requirements
	interval := 120 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	// Extract base configuration parameters
	baseURL, ok := config["base_url"].(string)
	if !ok {
		return nil, fmt.Errorf("citrix probe requires 'base_url' configuration")
	}

	environment, ok := config["environment"].(string)
	if !ok {
		environment = "PROD" // Default environment
	}

	// Extract authentication configuration
	authConfig, ok := config["auth"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("citrix probe requires 'auth' configuration")
	}

	authMethod, ok := authConfig["method"].(string)
	if !ok {
		authMethod = "ntlm" // Default to NTLM
	}

	username, ok := authConfig["username"].(string)
	if !ok {
		return nil, fmt.Errorf("citrix probe requires 'auth.username' configuration")
	}

	password, ok := authConfig["password"].(string)
	if !ok {
		return nil, fmt.Errorf("citrix probe requires 'auth.password' configuration")
	}

	// Extract TLS configuration
	verifySSL := true
	if tlsConfig, ok := config["tls"].(map[string]interface{}); ok {
		if cfgVerifySSL, ok := tlsConfig["verify_ssl"].(bool); ok {
			verifySSL = cfgVerifySSL
		}
	}

	// Use interval for probe execution timing

	// Extract timeout configuration
	timeout := 30 * time.Second
	if cfgTimeout, ok := config["timeout"].(int); ok {
		timeout = time.Duration(cfgTimeout) * time.Second
	}

	// Extract retry configuration
	maxRetryAttempts := 3
	retryBackoffFactor := 2.0
	if retryConfig, ok := config["retry"].(map[string]interface{}); ok {
		if cfgMaxAttempts, ok := retryConfig["max_attempts"].(int); ok {
			maxRetryAttempts = cfgMaxAttempts
		}
		if cfgBackoffFactor, ok := retryConfig["backoff_factor"].(float64); ok {
			retryBackoffFactor = cfgBackoffFactor
		}
	}
	
	// Extract Delivery Controller configuration if present
	var ddcConfig *DeliveryControllerConfig
	var siteFilter string
	if ddcCfg, ok := config["delivery_controller"].(map[string]interface{}); ok {
		ddcConfig = &DeliveryControllerConfig{
			VerifySSL: verifySSL, // Use same SSL config as main client
			Timeout:   timeout,
		}
		
		if url, ok := ddcCfg["url"].(string); ok {
			ddcConfig.URL = url
		}
		
		if fallbackURLs, ok := ddcCfg["fallback_urls"].([]interface{}); ok {
			ddcConfig.FallbackURLs = make([]string, 0, len(fallbackURLs))
			for _, url := range fallbackURLs {
				if urlStr, ok := url.(string); ok {
					ddcConfig.FallbackURLs = append(ddcConfig.FallbackURLs, urlStr)
				}
			}
		}
		
		if site, ok := ddcCfg["site_filter"].(string); ok {
			siteFilter = site
			ddcConfig.SiteFilter = site
		}
		
		moduleLogger.Info().
			Str("ddc_url", ddcConfig.URL).
			Str("site_filter", siteFilter).
			Int("fallback_count", len(ddcConfig.FallbackURLs)).
			Msg("Delivery Controller configuration detected")
	}

	ctx, cancel := context.WithCancel(context.Background())

	probe := &citrixProbe{
		BaseProbe:           &types.BaseProbe{},
		config:              config,
		logger:              moduleLogger,
		interval:            interval,
		ctx:                 ctx,
		cancelFunc:          cancel,
		baseURL:             baseURL,
		environment:         environment,
		authMethod:          authMethod,
		username:            username,
		password:            password,
		verifySSL:           verifySSL,
		timeout:             timeout,
		maxRetryAttempts:    maxRetryAttempts,
		retryBackoffFactor:  retryBackoffFactor,
		ddcConfig:           ddcConfig,
		siteFilter:          siteFilter,
	}

	return probe, nil
}

// GetName returns the unique identifier of the probe
func (p *citrixProbe) GetName() string {
	return "citrix"
}

// ShouldStart indicates if probe should be activated
func (p *citrixProbe) ShouldStart() bool {
	return true
}

// GetInterval returns the collection frequency
func (p *citrixProbe) GetInterval() time.Duration {
	return p.interval
}

// OnStart initializes the probe when it's started
func (p *citrixProbe) OnStart(quitChannel chan struct{}) error {
	// Create Citrix OData client
	var err error
	p.client, err = NewCitrixClient(CitrixClientConfig{
		BaseURL:            p.baseURL,
		Environment:        p.environment,
		AuthMethod:         p.authMethod,
		Username:           p.username,
		Password:           p.password,
		VerifySSL:          p.verifySSL,
		Timeout:            p.timeout,
		MaxRetryAttempts:   p.maxRetryAttempts,
		RetryBackoffFactor: p.retryBackoffFactor,
	}, p.logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to create Citrix client: %v", err)
	}

	// Test connection to the Citrix OData API
	if err := p.client.Connect(p.ctx); err != nil {
		return fmt.Errorf("failed to connect to Citrix OData API at %s: %v", p.baseURL, err)
	}

	// Initialize Delivery Controller client if configured
	if p.ddcConfig != nil {
		authConfig := AuthConfig{
			Method:   p.authMethod,
			Username: p.username,
			Password: p.password,
		}
		
		p.ddcClient, err = NewDeliveryControllerClient(*p.ddcConfig, authConfig, p.logger.Logger)
		if err != nil {
			return fmt.Errorf("failed to create Delivery Controller client: %v", err)
		}
		
		// Test DDC connectivity
		if err := p.ddcClient.TestConnectivity(p.ctx); err != nil {
			p.logger.Warn().
				Err(err).
				Str("ddc_url", p.ddcConfig.URL).
				Msg("Failed to connect to Delivery Controller - site filtering disabled")
		} else {
			// If site filter is configured, get filtered machines
			if p.siteFilter != "" {
				machines, err := p.ddcClient.GetMachinesBySite(p.ctx, p.siteFilter)
				if err != nil {
					p.logger.Warn().
						Err(err).
						Str("site", p.siteFilter).
						Msg("Failed to get machines for site - using all machines")
				} else {
					p.filteredMachines = machines
					p.logger.Info().
						Str("site", p.siteFilter).
						Int("machine_count", len(machines)).
						Msg("Site filtering enabled - will monitor specific machines only")
				}
			}
		}
	}

	// Create metrics collector
	p.metricsCollector = NewMetricsCollectorWithEnv(p.client, p.environment, p.baseURL, p.logger.Logger)

	p.logger.Info().
		Str("base_url", p.baseURL).
		Str("environment", p.environment).
		Str("auth_method", p.authMethod).
		Bool("verify_ssl", p.verifySSL).
		Int("interval_seconds", int(p.interval.Seconds())).
		Msg("Citrix probe initialized")

	return nil
}

// Collect gathers metrics and returns collected datapoints
func (p *citrixProbe) Collect() ([]datapoint.DataPoint, error) {
	if p.client == nil {
		return nil, fmt.Errorf("citrix client not initialized")
	}

	if p.metricsCollector == nil {
		return nil, fmt.Errorf("citrix metrics collector not initialized")
	}

	now := time.Now()
	p.logger.Debug().Msg("Starting Citrix metrics collection")

	// Collect all metrics - no frequency logic needed
	allDatapoints, err := p.metricsCollector.CollectMetrics(p.ctx, now)
	if err != nil {
		p.logger.Error().
			Err(err).
			Msg("Failed to collect Citrix metrics")
		return nil, fmt.Errorf("failed to collect Citrix metrics: %v", err)
	}

	// Enrich with probe name
	enrichedDatapoints := p.BaseProbe.EnrichDataPointsWithProbeName(allDatapoints, p.GetName())

	// Route data through callback if configured
	if p.OnDataPoints != nil && len(enrichedDatapoints) > 0 {
		if err := p.OnDataPoints(enrichedDatapoints, p); err != nil {
			return nil, fmt.Errorf("error handling data points: %v", err)
		}
	}

	p.logger.Debug().
		Int("datapoints_count", len(enrichedDatapoints)).
		Msg("Citrix metrics collection completed")

	return enrichedDatapoints, nil
}

// OnShutdown handles cleanup when probe is stopped
func (p *citrixProbe) OnShutdown(ctx context.Context) error {
	p.logger.Info().Msg("Shutting down Citrix probe")
	p.cancelFunc() // Cancel the context to signal any ongoing operations to stop

	if p.client != nil {
		return p.client.Disconnect(ctx)
	}
	return nil
}

// GetTargetStrategies returns the strategies this probe's data should be sent to
func (p *citrixProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}