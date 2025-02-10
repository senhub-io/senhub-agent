// Package probes manages metric collection through various probes
package probes

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

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
	ticker       *time.Ticker              // Timer for periodic collection
	tickerOnce   sync.Once                 // Ensures single initialization
	mutex        sync.Mutex                // Protects probe operations
	logger       *logger.Logger
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
	hash := md5.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// NewProbePoller creates and initializes a new probe instance from the given configuration.
// It sets up logging, data collection callback, and probe-specific initialization.
func NewProbePoller(
	config configuration.ProbeConfig,
	logger *logger.Logger,
	addDataPoint data_store.AddCallback,
) (*ProbePoller, error) {
	probeId := GenerateProbeId(config)
	loggerWithProbeId := logger.With().Str("probe_id", probeId).Logger()

	loggerWithProbeId.Debug().Msg("Creating new probe poller")

	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("No constructor for probe %s\n%v", config.Name, err)
	}

	probe, err := probeConstructor(config.Params, &loggerWithProbeId)
	if err != nil {
		return nil, fmt.Errorf("unable to start probe %s: %v", config.Name, err)
	}

	probePoller := &ProbePoller{
		ProbeId:      probeId,
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
		logger:       &loggerWithProbeId,
	}

	if probeWithCallback, ok := probe.(types.ProbeWithCallback); ok {
		loggerWithProbeId.Debug().Msg("Setting callback for probe")
		probeWithCallback.SetCallback(probePoller.getWrappedCallback())
	}

	loggerWithProbeId.Debug().Msg("Probe poller created successfully")
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
	p.logger.Debug().Msg("Starting probe")

	if !p.Probe.ShouldStart() {
		p.logger.Debug().Msg("Probe should not start")
		return nil
	}

	p.tickerOnce.Do(func() {
		// Run OnStart only once
		if err := p.Probe.OnStart(quitChannel); err != nil {
			// If OnStart fails, don't start the ticker
			// Given this is async, we can't return the error
			p.logger.Error().Err(err).Msg("Failed to start probe")
			return
		}

		p.ticker = time.NewTicker(p.Probe.GetInterval())

		go func() {
			errorCount := 0
			if err := p.collect(); err != nil {
				p.logger.Error().Err(err).Msg("Initial collect failed")
			}

			for {
				select {
				case <-quitChannel:
					p.logger.Debug().Msg("Stopping probe")
					p.ticker.Stop()
					return
				case <-p.ticker.C:
					if err := p.collect(); err != nil {
						errorCount++
						if errorCount > 3 {
							p.logger.Warn().
								Int("error_count", errorCount).
								Err(err).
								Msg("Probe failed too many times")
						}
					} else {
						if errorCount > 0 {
							p.logger.Info().
								Int("error_count", errorCount).
								Msg("Probe recovered")
						}
						errorCount = 0
					}
				}
			}
		}()
	})
	return nil
}

// collect gathers metrics from the probe and routes them to the appropriate
// storage strategies. It handles both direct collection and callback-based collection.
func (p *ProbePoller) collect() error {
	p.logger.Debug().Msg("Collecting data")

	data, err := p.Probe.Collect()
	if err != nil {
		return fmt.Errorf("collect failed: %v", err)
	}

	if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
		p.logger.Debug().Msg("Using probe's strategy router")
		return p.addDataPoint(data, strategyRouter)
	}

	p.logger.Debug().Msg("Using default strategy router")
	return p.addDataPoint(data, &defaultStrategyRouter{})
}

// getWrappedCallback returns a function that handles routing of collected data
// to appropriate storage strategies for callback-based probes
func (p *ProbePoller) getWrappedCallback() func([]datapoint.DataPoint) error {
	return func(data []datapoint.DataPoint) error {
		p.logger.Debug().Int("datapoints_count", len(data)).Msg("Callback triggered")

		if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
			return p.addDataPoint(data, strategyRouter)
		}
		return p.addDataPoint(data, &defaultStrategyRouter{})
	}
}

// Shutdown gracefully stops the probe and cleans up resources
func (p *ProbePoller) Shutdown(ctx context.Context) error {
	p.logger.Debug().Msg("Shutting down probe")

	if p.ticker != nil {
		p.ticker.Stop()
	}

	err := p.Probe.OnShutdown(ctx)
	if err != nil {
		return fmt.Errorf("shutdown failed: %v", err)
	}
	return nil
}
