// Package probes manages metric collection through various probes
package probes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// ProbePoller handles the lifecycle and scheduling of a probe.
// It manages initialization, periodic collection, error tracking,
// and shutdown of an individual probe instance.
type ProbePoller struct {
	ProbeId      string                    // Unique identifier for the probe instance
	Probe        types.Probe               // The actual probe implementation
	config       configuration.ProbeConfig // Probe configuration
	addDataPoint data_store.AddCallback    // Callback to store collected data
	moduleLogger *logger.ModuleLogger
	scheduler    periodic_scheduler.PeriodicScheduler
}

// defaultStrategyRouter provides default routing to senhub and prtg strategies
// for probes that don't implement custom routing
type defaultStrategyRouter struct{}

// GetTargetStrategies returns the default target strategies
func (d *defaultStrategyRouter) GetTargetStrategies() []string {
	return []string{"senhub", "prtg"}
}

// GenerateProbeId creates a unique identifier for a probe configuration
// by hashing its name and parameters
func GenerateProbeId(config configuration.ProbeConfig) string {
	input := fmt.Sprintf("%s-%v", config.Name, config.Params)
	hash := sha256.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// NewProbePoller creates and initializes a new probe instance from the given configuration.
// It sets up logging, data collection callback, and probe-specific initialization.
func NewProbePoller(
	config configuration.ProbeConfig,
	baseLogger *logger.Logger,
	addDataPoint data_store.AddCallback,
) (*ProbePoller, error) {
	probeId := GenerateProbeId(config)

	// Create module-specific logger for probe poller with readable probe name
	probeModuleName := fmt.Sprintf("probe.%s", config.Name)
	moduleLogger := logger.NewModuleLogger(baseLogger, probeModuleName)

	moduleLogger.Debug().Msg("Creating new probe poller")

	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("No constructor for probe %s\n%v", config.Name, err)
	}

	probe, err := probeConstructor(config.Params, baseLogger)
	if err != nil {
		return nil, fmt.Errorf("unable to start probe %s: %v", config.Name, err)
	}

	probePoller := &ProbePoller{
		ProbeId:      probeId,
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
		moduleLogger: moduleLogger,
	}

	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          probe.GetInterval(),
		MaxRetries:        3,
		ExecuteOnStart:    true,
		ExecuteOnShutdown: false,
		Execute:           probePoller.collect,
		OnStart:           probe.OnStart,
		OnShutdown:        probe.OnShutdown,
	}, moduleLogger.Logger)
	probePoller.scheduler = scheduler

	if probeWithCallback, ok := probe.(types.ProbeWithCallback); ok {
		moduleLogger.Debug().Msg("Setting callback for probe")
		probeWithCallback.SetCallback(probePoller.getWrappedCallback())
	}

	moduleLogger.Debug().Msg("Probe poller created successfully")
	return probePoller, nil
}

// getProbeConstructorForConfig retrieves the appropriate constructor function
// for the specified probe type
func getProbeConstructorForConfig(config configuration.ProbeConfig) (ProbeConstructor, error) {
	constructor, exists := probeConstructors[config.Name]
	if !exists {
		return nil, fmt.Errorf("unknown probe type: %s", config.Name)
	}
	return constructor, nil
}

// GetName returns the probe's identifier
func (p *ProbePoller) GetName() string {
	return p.Probe.GetName()
}

// GetProbeId returns the unique identifier for this probe instance
func (p *ProbePoller) GetProbeId() string {
	return p.ProbeId
}

// GetProbeParams returns the probe's configuration parameters
func (p *ProbePoller) GetProbeParams() configuration.ProbeConfigParams {
	return p.config.Params
}

// Start begins the periodic collection of metrics from the probe.
// It handles initialization, scheduling, and error recovery.
func (p *ProbePoller) Start(quitChannel chan struct{}) error {
	p.moduleLogger.Debug().Msg("Starting probe")

	if !p.Probe.ShouldStart() {
		p.moduleLogger.Debug().Msg("Probe should not start")
		return nil
	}

	return p.scheduler.Start(quitChannel)
}

// collect gathers metrics from the probe and routes them to the appropriate
// storage strategies. It handles both direct collection and callback-based collection.
func (p *ProbePoller) collect() error {
	p.moduleLogger.Debug().Msg("Collecting data")

	data, err := p.Probe.Collect()
	if err != nil {
		return fmt.Errorf("collect failed: %v", err)
	}

	if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
		p.moduleLogger.Debug().Msg("Using probe's strategy router")
		return p.addDataPoint(data, strategyRouter)
	}

	p.moduleLogger.Debug().Msg("Using default strategy router")
	return p.addDataPoint(data, &defaultStrategyRouter{})
}

// getWrappedCallback returns a function that handles routing of collected data
// to appropriate storage strategies for callback-based probes
func (p *ProbePoller) getWrappedCallback() func([]datapoint.DataPoint) error {
	return func(data []datapoint.DataPoint) error {
		p.moduleLogger.Debug().Int("datapoints_count", len(data)).Msg("Callback triggered")

		if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
			return p.addDataPoint(data, strategyRouter)
		}
		return p.addDataPoint(data, &defaultStrategyRouter{})
	}
}

// Shutdown gracefully stops the probe and cleans up resources
func (p *ProbePoller) Shutdown(ctx context.Context) error {
	p.moduleLogger.Debug().Msg("Shutting down probe")

	return p.scheduler.Shutdown(ctx)
}
