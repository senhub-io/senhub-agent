// Package redfish provides monitoring capabilities for hardware systems via Redfish API
package redfish

import (
	"context"
	"fmt"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// redfishProbe implements monitoring for hardware systems using the Redfish API
type redfishProbe struct {
	*types.BaseProbe
	config         map[string]interface{}
	logger         *logger.ModuleLogger
	interval       time.Duration
	collector      RedfishCollector
	endpoint       string
	username       string
	password       string
	verifySSL      bool
	collections    []CollectionType
	ctx            context.Context
	cancelFunc     context.CancelFunc
}

// NewRedfishProbe creates a new instance of the Redfish probe
func NewRedfishProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger for redfish probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.redfish")
	interval := 300 * time.Second // Default: 5 minutes
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	// Extract configuration parameters
	endpoint, ok := config["endpoint"].(string)
	if !ok {
		return nil, fmt.Errorf("redfish probe requires 'endpoint' configuration")
	}

	username, ok := config["username"].(string)
	if !ok {
		return nil, fmt.Errorf("redfish probe requires 'username' configuration")
	}

	password, ok := config["password"].(string)
	if !ok {
		return nil, fmt.Errorf("redfish probe requires 'password' configuration")
	}

	// SSL verification configuration (default: true)
	verifySSL := true
	if cfgVerifySSL, ok := config["verify_ssl"].(bool); ok {
		verifySSL = cfgVerifySSL
	}

	// Cache duration configuration was removed as scheduling is handled by the probe poller

	// Default collections to gather if not specified
	collections := []CollectionType{
		CollectionSystem,
		CollectionThermal,
		CollectionPower,
		CollectionProcessor,
		CollectionMemory,
		CollectionStorage,  // Added storage collection by default for PowerVault metrics
	}

	// Override collections if specified
	if cfgCollections, ok := config["collections"].([]interface{}); ok {
		collections = make([]CollectionType, 0, len(cfgCollections))
		for _, col := range cfgCollections {
			if colStr, ok := col.(string); ok {
				collections = append(collections, CollectionType(colStr))
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	probe := &redfishProbe{
		BaseProbe:      &types.BaseProbe{},
		config:         config,
		logger:         moduleLogger,
		interval:       interval,
		endpoint:       endpoint,
		username:       username,
		password:       password,
		verifySSL:      verifySSL,
		collections:    collections,
		ctx:            ctx,
		cancelFunc:     cancel,
	}

	// We'll initialize the actual collector in OnStart after we can detect the vendor
	
	return probe, nil
}

// GetName returns the unique identifier of the probe
func (p *redfishProbe) GetName() string {
	return "redfishProbe"
}

// ShouldStart indicates if probe should be activated
func (p *redfishProbe) ShouldStart() bool {
	return true
}

// GetInterval returns the collection frequency
func (p *redfishProbe) GetInterval() time.Duration {
	return p.interval
}

// OnStart initializes the probe when it's started
func (p *redfishProbe) OnStart(quitChannel chan struct{}) error {
	// Create an initial generic collector
	// Later we'll detect the vendor and create the appropriate collector
	var err error
	p.collector, err = NewGenericCollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
	if err != nil {
		return fmt.Errorf("failed to create Redfish collector: %v", err)
	}

	// Connect to the Redfish API
	if err := p.collector.Connect(p.ctx); err != nil {
		return fmt.Errorf("failed to connect to Redfish API at %s: %v", p.endpoint, err)
	}

	// Check if vendor detection found a specific vendor
	detectedVendor := p.collector.GetVendorType()
	if detectedVendor != VendorGeneric {
		// Create vendor-specific collector based on detected vendor
		var vendorCollector RedfishCollector
		var err error

		switch detectedVendor {
		case VendorDell:
			p.logger.Info().Msg("Dell server detected, creating Dell-specific collector")
			vendorCollector, err = NewDellCollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
		case VendorHPE:
			p.logger.Info().Msg("HPE server detected, creating HPE-specific collector")
			vendorCollector, err = NewHPECollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
		case VendorLenovo:
			p.logger.Info().Msg("Lenovo server detected, creating Lenovo-specific collector")
			vendorCollector, err = NewLenovoCollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
		case VendorCisco:
			p.logger.Info().Msg("Cisco server detected, creating Cisco-specific collector")
			vendorCollector, err = NewCiscoCollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
		case VendorStorage:
			p.logger.Info().Msg("Storage system detected, creating Storage-specific collector")
			vendorCollector, err = NewStorageCollector(p.endpoint, p.username, p.password, p.logger.Logger, p.verifySSL)
		default:
			p.logger.Info().
				Str("vendor", string(detectedVendor)).
				Msg("Detected vendor does not have a specific collector, using generic implementation")
		}

		if err != nil {
			p.logger.Warn().
				Err(err).
				Str("vendor", string(detectedVendor)).
				Msg("Failed to create vendor-specific collector, falling back to generic")
		} else if vendorCollector != nil {
			// Disconnect the old collector
			if err := p.collector.Disconnect(p.ctx); err != nil {
				p.logger.Warn().
					Err(err).
					Msg("Failed to disconnect generic collector")
			}

			// Connect the new vendor-specific collector
			p.collector = vendorCollector
			if err := p.collector.Connect(p.ctx); err != nil {
				return fmt.Errorf("failed to connect vendor-specific collector: %v", err)
			}
		}
	}

	p.logger.Info().
		Str("endpoint", p.endpoint).
		Str("vendor", string(p.collector.GetVendorType())).
		Bool("verify_ssl", p.verifySSL).
		Msg("Redfish probe initialized")

	return nil
}

// Collect gathers metrics and returns collected datapoints
func (p *redfishProbe) Collect() ([]data_store.DataPoint, error) {
	if p.collector == nil {
		return nil, fmt.Errorf("redfish collector not initialized")
	}

	now := time.Now()
	allDatapoints := make([]data_store.DataPoint, 0)

	// Add common tags
	commonTags := []tags.Tag{
		{Key: "endpoint", Value: p.endpoint},
		{Key: "vendor", Value: string(p.collector.GetVendorType())},
	}

	// Collect metrics for each requested collection type
	for _, collectionType := range p.collections {
		// Skip if collection type is not supported by this vendor
		if !p.collector.IsSupported(collectionType) {
			p.logger.Debug().
				Str("collection", string(collectionType)).
				Str("vendor", string(p.collector.GetVendorType())).
				Msg("Collection type not supported by vendor, skipping")
			continue
		}

		// Collect metrics for this collection type
		datapoints, err := p.collector.CollectMetrics(p.ctx, collectionType, now)
		if err != nil {
			p.logger.Error().
				Err(err).
				Str("collection", string(collectionType)).
				Msg("Failed to collect metrics")
			continue
		}

		// Add common tags to all datapoints
		for i := range datapoints {
			datapoints[i].Tags = append(datapoints[i].Tags, commonTags...)
		}

		// Add to aggregate result
		allDatapoints = append(allDatapoints, datapoints...)
	}

	// Classification removed per user request

	// Enrich with probe name
	enrichedDatapoints := p.BaseProbe.EnrichDataPointsWithProbeName(allDatapoints, p.GetName())

	// Route data through callback if configured
	if p.OnDataPoints != nil && len(enrichedDatapoints) > 0 {
		if err := p.OnDataPoints(enrichedDatapoints, p); err != nil {
			return nil, fmt.Errorf("error handling data points: %v", err)
		}
	}

	return enrichedDatapoints, nil
}

// OnShutdown handles cleanup when probe is stopped
func (p *redfishProbe) OnShutdown(ctx context.Context) error {
	p.cancelFunc() // Cancel the context to signal any ongoing operations to stop

	if p.collector != nil {
		return p.collector.Disconnect(ctx)
	}
	return nil
}

// GetTargetStrategies returns the strategies this probe's data should be sent to
func (p *redfishProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}