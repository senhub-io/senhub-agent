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
	errCount     int // Tracks consecutive failures
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
	fmt.Printf("[DEBUG] Generating probe ID for %s\n", config.Name)
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
	fmt.Printf("[DEBUG] Creating new probe poller for %s\n", config.Name)

	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		fmt.Printf("[ERROR] Failed to get constructor for probe %s: %v\n", config.Name, err)
		return nil, err
	}

	probe, err := probeConstructor(config.Params, logger)
	if err != nil {
		fmt.Printf("[ERROR] Failed to create probe %s: %v\n", config.Name, err)
		return nil, fmt.Errorf("unable to start probe %s: %v", config.Name, err)
	}

	probePoller := &ProbePoller{
		ProbeId:      GenerateProbeId(config),
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
		logger:       logger,
	}

	if probeWithCallback, ok := probe.(types.ProbeWithCallback); ok {
		fmt.Printf("[DEBUG] Setting callback for probe %s\n", config.Name)
		probeWithCallback.SetCallback(probePoller.getWrappedCallback())
	}

	fmt.Printf("[DEBUG] Probe poller created successfully for %s\n", config.Name)
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
	fmt.Printf("[DEBUG] Starting probe %s\n", p.GetName())

	if !p.Probe.ShouldStart() {
		fmt.Printf("[DEBUG] Probe %s should not start\n", p.GetName())
		return nil
	}

	p.tickerOnce.Do(func() {
		if err := p.Probe.OnStart(quitChannel); err != nil {
			fmt.Printf("[ERROR] Failed to start probe %s: %v\n", p.GetName(), err)
			return
		}

		p.ticker = time.NewTicker(p.Probe.GetInterval())

		go func() {
			if err := p.collect(); err != nil {
				fmt.Printf("[ERROR] Initial collect failed for probe %s: %v\n",
					p.GetName(), err)
			}

			for {
				select {
				case <-quitChannel:
					fmt.Printf("[DEBUG] Stopping probe %s\n", p.GetName())
					p.ticker.Stop()
					return
				case <-p.ticker.C:
					if err := p.collect(); err != nil {
						p.errCount++
						if p.errCount > 3 {
							fmt.Printf("[WARN] Probe %s failed %d times consecutively\n",
								p.GetName(), p.errCount)
						}
					} else {
						if p.errCount > 0 {
							fmt.Printf("[INFO] Probe %s recovered after %d failures\n",
								p.GetName(), p.errCount)
						}
						p.errCount = 0
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
	fmt.Printf("[DEBUG] Collecting data from probe %s\n", p.GetName())

	data, err := p.Probe.Collect()
	if err != nil {
		fmt.Printf("[ERROR] Failed to collect data from probe %s: %v\n",
			p.GetName(), err)
		return fmt.Errorf("collect failed: %v", err)
	}

	if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
		fmt.Printf("[DEBUG] Using probe's strategy router for %s\n", p.GetName())
		return p.addDataPoint(data, strategyRouter)
	}

	fmt.Printf("[DEBUG] Using default strategy router for %s\n", p.GetName())
	return p.addDataPoint(data, &defaultStrategyRouter{})
}

// getWrappedCallback returns a function that handles routing of collected data
// to appropriate storage strategies for callback-based probes
func (p *ProbePoller) getWrappedCallback() func([]datapoint.DataPoint) error {
	return func(data []datapoint.DataPoint) error {
		fmt.Printf("[DEBUG] Callback triggered for probe %s with %d datapoints\n",
			p.GetName(), len(data))

		if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
			return p.addDataPoint(data, strategyRouter)
		}
		return p.addDataPoint(data, &defaultStrategyRouter{})
	}
}

// Shutdown gracefully stops the probe and cleans up resources
func (p *ProbePoller) Shutdown(ctx context.Context) error {
	fmt.Printf("[DEBUG] Shutting down probe %s\n", p.GetName())

	if p.ticker != nil {
		p.ticker.Stop()
	}

	err := p.Probe.OnShutdown(ctx)
	if err != nil {
		fmt.Printf("[ERROR] Error shutting down probe %s: %v\n", p.GetName(), err)
	}
	return err
}
