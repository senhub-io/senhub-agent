// Package otel provides monitoring capabilities for OpenTelemetry data collection
package otel

import (
	"context"
	"fmt"
	"strings"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// otelProbe implements the Probe interface for OpenTelemetry data collection
type otelProbe struct {
	*types.BaseProbe
	
	// Probe configuration
	name           string
	interval       time.Duration
	logger         *logger.Logger
	collector      OtelCollector
	telemetryTypes []TelemetryType
	endpoint       string
	ctx            context.Context
	cancelFunc     context.CancelFunc
	quitChannel    chan struct{}
	
	// Internal state
	lastCollectTime time.Time
}

// NewOtelProbe creates a new instance of an OpenTelemetry probe
func NewOtelProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	// Extract required parameter: endpoint
	endpoint, ok := config["endpoint"].(string)
	if !ok || endpoint == "" {
		return nil, fmt.Errorf("otel probe requires 'endpoint' configuration")
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	probe := &otelProbe{
		BaseProbe:      &types.BaseProbe{},
		name:           "otelProbe",
		interval:       60 * time.Second,
		logger:         logger,
		endpoint:       endpoint,
		telemetryTypes: []TelemetryType{TelemetryMetrics, TelemetryTraces, TelemetryLogs},
		ctx:            ctx,
		cancelFunc:     cancel,
	}
	
	// Apply configuration values
	if name, ok := config["name"].(string); ok && name != "" {
		probe.name = name
	}
	
	if interval, ok := config["interval"].(int); ok && interval > 0 {
		probe.interval = time.Duration(interval) * time.Second
	}
	
	// Setup telemetry types to collect
	if typesList, ok := config["telemetry_types"].([]interface{}); ok && len(typesList) > 0 {
		probe.telemetryTypes = []TelemetryType{}
		for _, t := range typesList {
			if typeStr, ok := t.(string); ok {
				telemetryType := TelemetryType(typeStr)
				if isSupportedTelemetryType(telemetryType) {
					probe.telemetryTypes = append(probe.telemetryTypes, telemetryType)
				} else {
					logger.Warn().Msgf("Unsupported telemetry type: %s", typeStr)
				}
			}
		}
		
		if len(probe.telemetryTypes) == 0 {
			return nil, fmt.Errorf("no valid telemetry types configured")
		}
	}
	
	// Determine which protocol to use (HTTP or gRPC)
	protocol := ""
	if protocolStr, ok := config["protocol"].(string); ok && protocolStr != "" {
		protocol = protocolStr
	} else {
		// Auto-detect protocol based on endpoint
		if strings.HasPrefix(endpoint, "grpc://") {
			protocol = "grpc"
		} else if strings.Contains(endpoint, ":4317") {
			protocol = "grpc"
		} else {
			protocol = "http"
		}
	}
	
	// Initialize the appropriate collector
	var err error
	switch protocol {
	case "http":
		httpConfig := make(map[string]interface{})
		httpConfig["endpoint"] = endpoint
		if headers, ok := config["headers"].(map[string]interface{}); ok {
			httpConfig["headers"] = headers
		}
		
		probe.collector, err = NewHTTPCollector(httpConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP collector: %w", err)
		}
		
	case "grpc":
		grpcConfig := make(map[string]interface{})
		grpcConfig["endpoint"] = endpoint
		if insecure, ok := config["insecure"].(bool); ok {
			grpcConfig["insecure"] = insecure
		}
		
		probe.collector, err = NewGRPCCollector(grpcConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC collector: %w", err)
		}
		
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
	
	// Validate that we have a collector
	if probe.collector == nil {
		return nil, fmt.Errorf("failed to initialize collector")
	}
	
	return probe, nil
}

// GetName returns the unique identifier of the probe
func (p *otelProbe) GetName() string {
	return p.name
}

// ShouldStart indicates if probe should be activated based on environment
func (p *otelProbe) ShouldStart() bool {
	return p.collector != nil
}

// GetInterval returns the collection frequency for the probe
func (p *otelProbe) GetInterval() time.Duration {
	return p.interval
}

// Collect gathers metrics and returns collected datapoints
func (p *otelProbe) Collect() ([]data_store.DataPoint, error) {
	if p.collector == nil {
		return nil, fmt.Errorf("otel collector not initialized")
	}

	collectCtx, cancel := context.WithTimeout(p.ctx, p.interval/2)
	defer cancel()
	
	var allDataPoints []data_store.DataPoint
	collectionTime := time.Now()
	
	// Add common tags
	commonTags := []tags.Tag{
		{Key: "endpoint", Value: p.endpoint},
		{Key: "protocol", Value: string(p.collector.GetProtocolType())},
	}
	
	// Collect metrics for each requested telemetry type
	for _, telemetryType := range p.telemetryTypes {
		if !p.collector.IsSupported(telemetryType) {
			p.logger.Debug().
				Str("telemetry_type", string(telemetryType)).
				Msg("Telemetry type not supported by collector, skipping")
			continue
		}
		
		p.logger.Debug().
			Str("collector", string(p.collector.GetProtocolType())).
			Str("telemetry_type", string(telemetryType)).
			Msg("Collecting OpenTelemetry data")
			
		dataPoints, err := p.collector.CollectTelemetry(collectCtx, telemetryType, collectionTime)
		if err != nil {
			p.logger.Error().
				Err(err).
				Str("collector", string(p.collector.GetProtocolType())).
				Str("telemetry_type", string(telemetryType)).
				Msg("Failed to collect OpenTelemetry data")
			continue
		}
		
		// Add common tags to all datapoints
		for i := range dataPoints {
			dataPoints[i].Tags = append(dataPoints[i].Tags, commonTags...)
		}
		
		// Add to aggregate result
		allDataPoints = append(allDataPoints, dataPoints...)
		
		p.logger.Debug().
			Str("collector", string(p.collector.GetProtocolType())).
			Str("telemetry_type", string(telemetryType)).
			Int("datapoints", len(dataPoints)).
			Msg("Successfully collected OpenTelemetry data")
	}
	
	p.lastCollectTime = collectionTime
	
	// Route data through callback if configured
	if p.OnDataPoints != nil && len(allDataPoints) > 0 {
		if err := p.OnDataPoints(allDataPoints, p); err != nil {
			return nil, fmt.Errorf("error handling data points: %v", err)
		}
	}
	
	return allDataPoints, nil
}

// OnStart is called when probe is initialized
// quitChannel signals when probe should stop
func (p *otelProbe) OnStart(quitChannel chan struct{}) error {
	p.quitChannel = quitChannel
	p.logger.Info().Msg("OpenTelemetry probe started")
	
	// Connect the collector
	connectCtx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	
	err := p.collector.Connect(connectCtx)
	if err != nil {
		p.logger.Error().
			Err(err).
			Str("protocol", string(p.collector.GetProtocolType())).
			Msg("Failed to connect OpenTelemetry collector")
		return fmt.Errorf("failed to connect to OpenTelemetry endpoint: %w", err)
	}
	
	p.logger.Info().
		Str("endpoint", p.endpoint).
		Str("protocol", string(p.collector.GetProtocolType())).
		Strs("telemetry_types", telemetryTypesToStrings(p.telemetryTypes)).
		Msg("OpenTelemetry probe initialized")
	
	return nil
}

// OnShutdown handles cleanup when probe is stopped
func (p *otelProbe) OnShutdown(ctx context.Context) error {
	p.logger.Info().Msg("Shutting down OpenTelemetry probe")
	
	// Cancel the context to signal any ongoing operations to stop
	p.cancelFunc()
	
	// Disconnect the collector
	if p.collector != nil {
		return p.collector.Disconnect(ctx)
	}
	
	return nil
}

// GetTargetStrategies returns the strategies this probe's data should be sent to
func (p *otelProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}

// Helper function to convert telemetry types to strings for logging
func telemetryTypesToStrings(types []TelemetryType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

// Helper function to validate telemetry types
func isSupportedTelemetryType(telemetryType TelemetryType) bool {
	switch telemetryType {
	case TelemetryMetrics, TelemetryTraces, TelemetryLogs:
		return true
	default:
		return false
	}
}