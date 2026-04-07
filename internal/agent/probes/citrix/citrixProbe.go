// Package citrix provides monitoring capabilities for Citrix Virtual Apps and Desktops via OData API
package citrix

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// citrixProbe implements monitoring for Citrix Virtual Apps and Desktops using OData API
type citrixProbe struct {
	*types.BaseProbe
	config           map[string]interface{}
	logger           *logger.ModuleLogger
	interval         time.Duration
	client           CitrixClient
	ddcClient        DeliveryControllerClient
	metricsCollector *MetricsCollector
	ctx              context.Context
	cancelFunc       context.CancelFunc

	// Per-component configuration (new format)
	directorConfig *ComponentConfig // Required: Director/OData API
	ddcConfig      *DeliveryControllerConfig // Optional: Delivery Controller/CVAD API
	licenseConfig  *ComponentConfig // Optional: License Server

	// Probe-level settings (shared across components)
	timeout            time.Duration
	maxRetryAttempts   int
	retryBackoffFactor float64

	// Site filtering
	siteFilter       string
	filteredMachines []string

	// Site inventory service
	inventoryService *InventoryService

	// Debug mode
	debugMode bool
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

	// Extract timeout configuration (probe-level)
	timeout := 30 * time.Second
	if cfgTimeout, ok := config["timeout"].(int); ok {
		timeout = time.Duration(cfgTimeout) * time.Second
	}

	// Extract retry configuration (probe-level)
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

	// Detect configuration format: new (per-component) vs old (flat)
	var directorConfig *ComponentConfig
	var ddcConfig *DeliveryControllerConfig
	var licenseConfig *ComponentConfig
	var siteFilter string

	if _, isNewFormat := config["director"].(map[string]interface{}); isNewFormat {
		// ===== NEW FORMAT: per-component config blocks =====
		var err error
		directorConfig, err = parseComponentConfig(config["director"].(map[string]interface{}))
		if err != nil {
			return nil, fmt.Errorf("citrix probe 'director' block: %w", err)
		}
		if directorConfig.URL == "" {
			return nil, fmt.Errorf("citrix probe requires 'director.url' configuration")
		}
		if directorConfig.Auth.Username == "" {
			return nil, fmt.Errorf("citrix probe requires 'director.auth.username' configuration")
		}
		if directorConfig.Auth.Password == "" {
			return nil, fmt.Errorf("citrix probe requires 'director.auth.password' configuration")
		}
		// Director always uses NTLM
		directorConfig.Auth.Method = "ntlm"

		// Parse delivery_controller block (optional)
		if ddcBlock, ok := config["delivery_controller"].(map[string]interface{}); ok {
			ddcComponent, err := parseComponentConfig(ddcBlock)
			if err != nil {
				return nil, fmt.Errorf("citrix probe 'delivery_controller' block: %w", err)
			}
			// DDC always uses Basic auth
			ddcComponent.Auth.Method = "basic"
			// If DDC auth not specified, inherit from director
			if ddcComponent.Auth.Username == "" {
				ddcComponent.Auth.Username = directorConfig.Auth.Username
				ddcComponent.Auth.Password = directorConfig.Auth.Password
			}

			ddcConfig = &DeliveryControllerConfig{
				URL:          ddcComponent.URL,
				FallbackURLs: ddcComponent.FallbackURLs,
				VerifySSL:    ddcComponent.VerifySSL,
				Timeout:      timeout,
				Auth: AuthConfig{
					Method:   "basic",
					Username: ddcComponent.Auth.Username,
					Password: ddcComponent.Auth.Password,
				},
			}
			if site, ok := ddcBlock["site_filter"].(string); ok {
				siteFilter = site
				ddcConfig.SiteFilter = site
			}

			moduleLogger.Info().
				Str("ddc_url", ddcConfig.URL).
				Str("site_filter", siteFilter).
				Int("fallback_count", len(ddcConfig.FallbackURLs)).
				Msg("Delivery Controller configuration detected")
		}

		// Parse license_server block (optional)
		if lsBlock, ok := config["license_server"].(map[string]interface{}); ok {
			licenseConfig, err = parseComponentConfig(lsBlock)
			if err != nil {
				return nil, fmt.Errorf("citrix probe 'license_server' block: %w", err)
			}
			// License Server uses Basic auth
			licenseConfig.Auth.Method = "basic"
			// If license auth not specified, inherit from director
			if licenseConfig.Auth.Username == "" {
				licenseConfig.Auth.Username = directorConfig.Auth.Username
				licenseConfig.Auth.Password = directorConfig.Auth.Password
			}
			moduleLogger.Info().
				Str("license_server_url", licenseConfig.URL).
				Msg("License Server configuration detected")
		}
	} else {
		// ===== OLD FORMAT: flat config with global auth (backward compatibility) =====
		moduleLogger.Warn().Msg("Using deprecated flat configuration format - consider migrating to per-component config blocks (director/delivery_controller/license_server)")

		// Extract Director URL (prefer director_url, fallback to base_url)
		baseURL, ok := config["director_url"].(string)
		if !ok {
			baseURL, ok = config["base_url"].(string)
			if !ok {
				return nil, fmt.Errorf("citrix probe requires 'director_url' (or 'base_url') configuration")
			}
			moduleLogger.Info().Str("base_url", baseURL).Msg("Using deprecated 'base_url' parameter - consider renaming to 'director_url'")
		}

		// Extract global authentication configuration
		authCfg, ok := config["auth"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("citrix probe requires 'auth' configuration")
		}

		username, ok := authCfg["username"].(string)
		if !ok {
			return nil, fmt.Errorf("citrix probe requires 'auth.username' configuration")
		}

		password, ok := authCfg["password"].(string)
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

		// Build directorConfig from flat fields
		directorConfig = &ComponentConfig{
			URL:       baseURL,
			VerifySSL: verifySSL,
			Auth: AuthConfig{
				Method:   "ntlm",
				Username: username,
				Password: password,
			},
		}

		// Extract Delivery Controller configuration if present (old format)
		if ddcCfg, ok := config["delivery_controller"].(map[string]interface{}); ok {
			ddcConfig = &DeliveryControllerConfig{
				VerifySSL: verifySSL,
				Timeout:   timeout,
				Auth: AuthConfig{
					Method:   "basic",
					Username: username,
					Password: password,
				},
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

			// Also support []string (from Go test configs)
			if fallbackURLs, ok := ddcCfg["fallback_urls"].([]string); ok {
				ddcConfig.FallbackURLs = fallbackURLs
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

		// Extract license server configuration (old format - string or map with url)
		if lsCfg, ok := config["license_server"].(map[string]interface{}); ok {
			if url, ok := lsCfg["url"].(string); ok {
				licenseConfig = &ComponentConfig{
					URL:       url,
					VerifySSL: verifySSL,
					Auth: AuthConfig{
						Method:   "basic",
						Username: username,
						Password: password,
					},
				}
				moduleLogger.Info().
					Str("license_server_url", url).
					Msg("License Server configuration detected")
			}
		} else if lsURL, ok := config["license_server"].(string); ok {
			licenseConfig = &ComponentConfig{
				URL:       lsURL,
				VerifySSL: verifySSL,
				Auth: AuthConfig{
					Method:   "basic",
					Username: username,
					Password: password,
				},
			}
			moduleLogger.Info().
				Str("license_server_url", lsURL).
				Msg("License Server configuration detected")
		}
	}

	// Extract debug mode configuration
	debugMode := false
	if debug, ok := config["debug_identifiers"].(bool); ok {
		debugMode = debug
		if debugMode {
			moduleLogger.Info().Msg("Debug identifier extraction mode enabled")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	probe := &citrixProbe{
		BaseProbe:          &types.BaseProbe{},
		config:             config,
		logger:             moduleLogger,
		interval:           interval,
		ctx:                ctx,
		cancelFunc:         cancel,
		directorConfig:     directorConfig,
		ddcConfig:          ddcConfig,
		licenseConfig:      licenseConfig,
		timeout:            timeout,
		maxRetryAttempts:   maxRetryAttempts,
		retryBackoffFactor: retryBackoffFactor,
		siteFilter:         siteFilter,
		debugMode:          debugMode,
	}

	return probe, nil
}

// parseComponentConfig parses a per-component config block (director, delivery_controller, license_server)
func parseComponentConfig(block map[string]interface{}) (*ComponentConfig, error) {
	cfg := &ComponentConfig{
		VerifySSL: true, // default
	}

	if url, ok := block["url"].(string); ok {
		cfg.URL = url
	}

	if verifySSL, ok := block["verify_ssl"].(bool); ok {
		cfg.VerifySSL = verifySSL
	}

	// Parse fallback_urls ([]interface{} from YAML or []string from Go)
	if fallbackURLs, ok := block["fallback_urls"].([]interface{}); ok {
		cfg.FallbackURLs = make([]string, 0, len(fallbackURLs))
		for _, url := range fallbackURLs {
			if urlStr, ok := url.(string); ok {
				cfg.FallbackURLs = append(cfg.FallbackURLs, urlStr)
			}
		}
	} else if fallbackURLs, ok := block["fallback_urls"].([]string); ok {
		cfg.FallbackURLs = fallbackURLs
	}

	// Parse auth sub-block
	if authBlock, ok := block["auth"].(map[string]interface{}); ok {
		if username, ok := authBlock["username"].(string); ok {
			cfg.Auth.Username = username
		}
		if password, ok := authBlock["password"].(string); ok {
			cfg.Auth.Password = password
		}
		if method, ok := authBlock["method"].(string); ok {
			cfg.Auth.Method = method
		}
	}

	return cfg, nil
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "citrix", "citrix2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

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
	// Create Citrix OData client using director config
	var err error
	p.client, err = NewCitrixClient(CitrixClientConfig{
		BaseURL:            p.directorConfig.URL,
		Environment:        "",
		AuthMethod:         p.directorConfig.Auth.Method,
		Username:           p.directorConfig.Auth.Username,
		Password:           p.directorConfig.Auth.Password,
		VerifySSL:          p.directorConfig.VerifySSL,
		Timeout:            p.timeout,
		MaxRetryAttempts:   p.maxRetryAttempts,
		RetryBackoffFactor: p.retryBackoffFactor,
	}, p.logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to create Citrix client: %v", err)
	}

	// Test connection to the Citrix OData API
	if err := p.client.Connect(p.ctx); err != nil {
		return fmt.Errorf("failed to connect to Citrix OData API at %s: %v", p.directorConfig.URL, err)
	}

	// Initialize Delivery Controller client if configured
	if p.ddcConfig != nil {
		p.ddcClient, err = NewDeliveryControllerClient(*p.ddcConfig, p.ddcConfig.Auth, p.logger.Logger)
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
			// If site filter is configured, initialize inventory service
			if p.siteFilter != "" {
				// Create inventory service with 5-minute cache
				p.inventoryService = NewInventoryService(p.ddcClient, 5*time.Minute, p.logger.Logger)

				// Initialize cache (RefreshInventory handles errors internally)
				if err := p.inventoryService.RefreshInventory(p.ctx, p.siteFilter); err != nil {
					p.logger.Warn().
						Err(err).
						Str("site", p.siteFilter).
						Msg("Failed to initialize inventory service - OData filtering disabled")
				} else {
					// Use ALL machines from the site inventory (Directory-first approach)
					// This aligns with Director console showing all machines regardless of state
					machines := p.inventoryService.GetAllMachinesForSite()
					p.filteredMachines = machines

					// Configure client-side filtering with all machine DNS names
					p.client.SetValidMachineDNS(machines)

					p.logger.Debug().
						Str("site", p.siteFilter).
						Int("machine_count", len(machines)).
						Msg("CVAD inventory service initialized - using ALL machines from Director (OData filtering enabled)")
				}
			}
		}
	}

	// Create metrics collector with filtered client wrapper
	filteredClient := &filteredCitrixClient{
		originalClient: p.client,
		probe:          p,
	}
	p.metricsCollector = NewMetricsCollectorWithEnv(filteredClient, "", p.directorConfig.URL, p.logger.Logger)

	// Pass license-related config to the metrics collector
	p.metricsCollector.ddcClient = p.ddcClient
	p.metricsCollector.siteFilter = p.siteFilter
	p.metricsCollector.licenseConfig = p.licenseConfig

	p.logger.Debug().
		Str("director_url", p.directorConfig.URL).
		Str("auth_method", p.directorConfig.Auth.Method).
		Bool("verify_ssl", p.directorConfig.VerifySSL).
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

	// Debug mode: extract identifiers instead of collecting metrics
	if p.debugMode {
		p.logger.Debug().Msg("Debug mode active - extracting identifiers instead of collecting metrics")
		if err := p.DebugIdentifierMapping(); err != nil {
			p.logger.Error().Err(err).Msg("Debug identifier extraction failed")
		}
		// Return empty datapoints in debug mode
		return []datapoint.DataPoint{}, nil
	}

	// Collect all metrics - no frequency logic needed
	allDatapoints, err := p.metricsCollector.CollectMetricsWithInventory(p.ctx, now, p.inventoryService)
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
	p.logger.Debug().Msg("Shutting down Citrix probe")
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

// GetMachinesForMetrics returns machines using site filtering (Directory-first approach)
func (p *citrixProbe) GetMachinesForMetrics(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	// If we have inventory service and filtered machines, use site filtering
	if p.inventoryService != nil && len(p.filteredMachines) > 0 {
		// Refresh inventory cache if stale
		if p.inventoryService.IsStale() {
			if err := p.inventoryService.RefreshInventory(ctx, p.siteFilter); err != nil {
				p.logger.Warn().Err(err).Msg("Failed to refresh inventory - using existing cache")
			} else {
				// Update filtered machines list with ALL machines from Director
				p.filteredMachines = p.inventoryService.GetAllMachinesForSite()

				// Update client-side filtering with refreshed machine DNS names
				p.client.SetValidMachineDNS(p.filteredMachines)

				p.logger.Debug().
					Int("machine_count", len(p.filteredMachines)).
					Msg("Updated filtered machines list with ALL machines from Director")
			}
		}

		// Use filtered query with all site machines
		return p.client.GetMachinesFiltered(ctx, sinceTime, p.filteredMachines)
	}

	// Fallback to unfiltered query
	return p.client.GetMachines(ctx, sinceTime)
}

// filteredCitrixClient wraps the original client to provide filtered results
type filteredCitrixClient struct {
	originalClient CitrixClient
	probe          *citrixProbe
}

// Implement CitrixClient interface by delegating to original client
func (f *filteredCitrixClient) Connect(ctx context.Context) error {
	return f.originalClient.Connect(ctx)
}

func (f *filteredCitrixClient) Disconnect(ctx context.Context) error {
	return f.originalClient.Disconnect(ctx)
}

func (f *filteredCitrixClient) GetSessions(ctx context.Context, sinceTime time.Time) ([]Session, error) {
	return f.originalClient.GetSessions(ctx, sinceTime)
}

func (f *filteredCitrixClient) GetSessionsByConnectionState(ctx context.Context, connectionStates []int) ([]Session, error) {
	return f.originalClient.GetSessionsByConnectionState(ctx, connectionStates)
}

// GetMachines uses filtering if available
func (f *filteredCitrixClient) GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	return f.probe.GetMachinesForMetrics(ctx, sinceTime)
}

func (f *filteredCitrixClient) GetMachinesFiltered(ctx context.Context, sinceTime time.Time, dnsNames []string) ([]Machine, error) {
	return f.originalClient.GetMachinesFiltered(ctx, sinceTime, dnsNames)
}

func (f *filteredCitrixClient) GetDesktopGroups(ctx context.Context) ([]DesktopGroup, error) {
	return f.originalClient.GetDesktopGroups(ctx)
}

func (f *filteredCitrixClient) GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error) {
	return f.originalClient.GetConnectionFailureLogs(ctx, sinceTime)
}

func (f *filteredCitrixClient) GetConnectionFailureLogsWithExpand(ctx context.Context, sinceTime time.Time, expand []string) ([]ConnectionFailureLog, error) {
	return f.originalClient.GetConnectionFailureLogsWithExpand(ctx, sinceTime, expand)
}

func (f *filteredCitrixClient) GetConnectionFailureCategories(ctx context.Context) ([]ConnectionFailureCategory, error) {
	return f.originalClient.GetConnectionFailureCategories(ctx)
}

func (f *filteredCitrixClient) GetDeliveryGroupById(ctx context.Context, deliveryGroupId string) (*DesktopGroup, error) {
	return f.originalClient.GetDeliveryGroupById(ctx, deliveryGroupId)
}

func (f *filteredCitrixClient) GetConnections(ctx context.Context, sinceTime time.Time) ([]Connection, error) {
	return f.originalClient.GetConnections(ctx, sinceTime)
}

func (f *filteredCitrixClient) GetLoadIndexes(ctx context.Context) ([]LoadIndex, error) {
	return f.originalClient.GetLoadIndexes(ctx)
}

func (f *filteredCitrixClient) SetValidMachineDNS(dnsNames []string) {
	f.originalClient.SetValidMachineDNS(dnsNames)
}

// DebugIdentifierMapping extracts identifiers from both CVAD and OData APIs for manual comparison
func (p *citrixProbe) DebugIdentifierMapping() error {
	p.logger.Debug().Msg("Starting debug identifier extraction (CVAD vs OData)")

	debugDir := "/tmp/citrix-debug"
	if err := os.MkdirAll(debugDir, 0750); err != nil {
		return fmt.Errorf("failed to create debug directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	siteName := p.siteFilter
	if siteName == "" {
		siteName = "default"
	}

	// Extract CVAD identifiers
	if err := p.extractCVADIdentifiers(debugDir, timestamp, siteName); err != nil {
		p.logger.Error().Err(err).Msg("Failed to extract CVAD identifiers")
	}

	// Extract OData identifiers
	if err := p.extractODataIdentifiers(debugDir, timestamp, siteName); err != nil {
		p.logger.Error().Err(err).Msg("Failed to extract OData identifiers")
	}

	p.logger.Debug().
		Str("debug_dir", debugDir).
		Str("timestamp", timestamp).
		Msg("Debug identifier extraction completed - check files for manual comparison")

	return nil
}

// extractCVADIdentifiers extracts machine and session identifiers from CVAD API
func (p *citrixProbe) extractCVADIdentifiers(debugDir, timestamp, siteName string) error {
	if p.ddcClient == nil {
		p.logger.Warn().Msg("DDC client not available - skipping CVAD extraction")
		return nil
	}

	ctx := context.Background()

	// CVAD Machines
	p.logger.Debug().Msg("Extracting CVAD machines...")
	cvadMachines, err := p.ddcClient.GetMachinesDetailedBySite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("failed to get CVAD machines: %w", err)
	}

	// CVAD Sessions (optional - skip if connection issues)
	var cvadSessions []DDCSession
	p.logger.Debug().Msg("Extracting CVAD sessions...")
	if sessions, err := p.ddcClient.GetSessionsBySite(ctx, siteName); err != nil {
		p.logger.Debug().Err(err).Msg("CVAD sessions extraction failed - skipping (not critical for identifier mapping)")
		cvadSessions = []DDCSession{} // Empty array for consistent JSON structure
	} else {
		cvadSessions = sessions
	}

	// Create debug data structure
	cvadData := map[string]interface{}{
		"extraction_time": time.Now().Format(time.RFC3339),
		"site_name":       siteName,
		"api_type":        "CVAD_REST",
		"machines_count":  len(cvadMachines),
		"sessions_count":  len(cvadSessions),
		"machines":        cvadMachines,
		"sessions":        cvadSessions,
	}

	// Save to file
	filename := filepath.Join(debugDir, fmt.Sprintf("cvad_identifiers_%s_%s.json", siteName, timestamp))
	return p.saveDebugData(filename, cvadData)
}

// extractODataIdentifiers extracts machine and session identifiers from OData API
func (p *citrixProbe) extractODataIdentifiers(debugDir, timestamp, siteName string) error {
	if p.client == nil {
		return fmt.Errorf("OData client not available")
	}

	ctx := context.Background()

	// OData Machines (use very old date to get all machines)
	p.logger.Debug().Msg("Extracting OData machines...")
	veryOldDate := time.Now().AddDate(-1, 0, 0) // 1 year ago
	odataMachines, err := p.client.GetMachines(ctx, veryOldDate)
	if err != nil {
		return fmt.Errorf("failed to get OData machines: %w", err)
	}

	// OData Sessions (recent ones only to avoid too much data)
	p.logger.Debug().Msg("Extracting OData sessions...")
	since := time.Now().Add(-24 * time.Hour) // Last 24h
	odataSessions, err := p.client.GetSessions(ctx, since)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to get OData sessions - continuing without them")
		odataSessions = []Session{}
	}

	// Create debug data structure
	odataData := map[string]interface{}{
		"extraction_time": time.Now().Format(time.RFC3339),
		"site_name":       siteName,
		"api_type":        "OData_Director",
		"machines_count":  len(odataMachines),
		"sessions_count":  len(odataSessions),
		"machines":        odataMachines,
		"sessions":        odataSessions,
	}

	// Save to file
	filename := filepath.Join(debugDir, fmt.Sprintf("odata_identifiers_%s_%s.json", siteName, timestamp))
	return p.saveDebugData(filename, odataData)
}

// saveDebugData saves debug data to JSON file
func (p *citrixProbe) saveDebugData(filename string, data interface{}) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create debug file %s: %w", filename, err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Pretty print
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode debug data: %w", err)
	}

	p.logger.Debug().
		Str("file", filename).
		Msg("Debug data saved")

	return nil
}
