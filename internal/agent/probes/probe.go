package probes

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
)

// Interface each probe should implement
type Probe interface {
	// Is the remote configuration valid?
	ValidateConfig(config map[string]interface{}) bool
	// GetName returns the name of the probe
	GetName() string
	// ShouldStart returns whether the probe should be started or not
	ShouldStart() bool
	// Interval at which the probe should be run
	GetInterval() time.Duration
	// Collect runs the probe and returns the data
	Collect() ([]data_store.DataPoint, error)
	// Event called when the probe is started
	OnStart(chan struct{}) error
	// Event called when the probe is shutdown
	OnShutdown(context.Context) error
}

type ProbeConstructor func(config map[string]interface{}) Probe

// AllProbeDefinitions is a list of all probes available
// The key is the name of the probe in remote configuration
var AllProbeDefinitions = map[string]ProbeConstructor{
	"host_memory":          NewMemoryProbe,
	"wifi_signal_strength": NewWifiSignalStrengthProbe,
	"ping_gateway":         NewPingGatewayProbe,
	"ping_webapp":          NewPingWebAppProbe,
	"load_webapp":          NewLoadWebAppProbe,
}

type ProbePoller struct {
	ProbeId      string
	Probe        Probe
	config       configuration.ProbeConfig
	addDataPoint data_store.AddCallback
	ticker       *time.Ticker
	tickerOnce   sync.Once
	mutex        sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

// GenerateProbeId generates a unique id for the probe configuration.
// This id is generated based on the probe name and its parameters.
// This id is used to identify the probe in the data store.
func GenerateProbeId(config configuration.ProbeConfig) string {
	input := fmt.Sprintf("%s-%v", config.Name, config.Params)
	hash := md5.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// NewProbePoller creates a new probe from ProbeConfig
func NewProbePoller(config configuration.ProbeConfig, addDataPoint data_store.AddCallback) (*ProbePoller, error) {
	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		return nil, err
	}

	probe := probeConstructor(config.Params)
	if probe.ValidateConfig(config.Params) == false {
		return nil, fmt.Errorf("invalid configuration for probe %s", config.Name)
	}

	probePoller := &ProbePoller{
		ProbeId:      GenerateProbeId(config),
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
	}

	return probePoller, nil
}

func getProbeConstructorForConfig(config configuration.ProbeConfig) (ProbeConstructor, error) {
	if constructor, ok := AllProbeDefinitions[config.Name]; ok {
		return constructor, nil
	}

	return nil, fmt.Errorf("unknown probe %s", config.Name)
}

func (p *ProbePoller) GetName() string {
	return p.Probe.GetName()
}
func (p *ProbePoller) GetProbeId() string {
	return p.ProbeId
}
func (p *ProbePoller) Start(quitChannel chan struct{}) error {
	if p.Probe.ShouldStart() == false {
		return nil
	}

	p.tickerOnce.Do(func() { // Ensure the ticker only starts once
		p.Probe.OnStart(quitChannel)

		p.ticker = time.NewTicker(p.Probe.GetInterval())

		go func() {
			// Ensure an iitial collect is done
			p.doCollectProbe()

			for {
				select {
				case <-quitChannel:
					p.ticker.Stop()
					return
				case <-p.ticker.C:
					p.doCollectProbe()
				}
			}
		}()
	})
	return nil
}

func (p *ProbePoller) doCollectProbe() error {
	data, err := p.Probe.Collect()
	if err != nil {
		log.Printf("Error collecting probe %v: %v", p.config, err)
		return err
	}
	return p.addDataPoint(data)
}

func (p *ProbePoller) Shutdown(ctx context.Context) error {
	return p.Probe.OnShutdown(ctx)
}
