// senhub-agent/internal/agent/probes/probe_poller.go
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
)

type ProbePoller struct {
	ProbeId      string
	Probe        types.Probe
	config       configuration.ProbeConfig
	addDataPoint data_store.AddCallback
	ticker       *time.Ticker
	tickerOnce   sync.Once
	mutex        sync.Mutex // Ensures thread-safe execution of doRefreshConfig
	logger       *logger.Logger
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
func NewProbePoller(
	config configuration.ProbeConfig,
	logger *logger.Logger,
	addDataPoint data_store.AddCallback,
) (*ProbePoller, error) {
	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		return nil, err
	}
	probe, err := probeConstructor(config.Params, logger)
	if err != nil {
		return nil, fmt.Errorf("Unable to start probe %s\n%v", config.Name, err)
	}

	// Si la probe supporte les callbacks, on configure le callback
	if probeWithCallback, ok := probe.(types.ProbeWithCallback); ok {
		probeWithCallback.SetCallback(addDataPoint)
	}

	probePoller := &ProbePoller{
		ProbeId:      GenerateProbeId(config),
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
		logger:       logger,
	}
	return probePoller, nil
}

func getProbeConstructorForConfig(config configuration.ProbeConfig) (ProbeConstructor, error) {
	constructor, exists := probeConstructors[config.Name]
	if !exists {
		return nil, fmt.Errorf("unknown probe type: %s", config.Name)
	}
	return constructor, nil
}

func (p *ProbePoller) GetName() string {
	return p.Probe.GetName()
}
func (p *ProbePoller) GetProbeId() string {
	return p.ProbeId
}
func (p *ProbePoller) GetProbeParams() configuration.ProbeConfigParams {
	return p.config.Params
}
func (p *ProbePoller) Start(quitChannel chan struct{}) error {
	if p.Probe.ShouldStart() == false {
		p.logger.Debug().Msg("Probe should not start")
		return nil
	}
	p.tickerOnce.Do(func() {
		p.Probe.OnStart(quitChannel)
		p.ticker = time.NewTicker(p.Probe.GetInterval())
		go func() {

			if err := p.collect(); err != nil {
				p.logger.Debug().
					Err(err).
					Msg("Error during initial collect")
			}
			for {
				select {
				case <-quitChannel:
					p.ticker.Stop()
					return
				case <-p.ticker.C:
					p.collect()

				}
			}
		}()
	})
	return nil
}

func (p *ProbePoller) collect() error {
	data, err := p.Probe.Collect()
	if err != nil {
		p.logger.Warn().
			Err(err).
			Interface("probe_config", p.config).
			Str("probe_name", p.Probe.GetName()).
			Msg("Error collecting probe")
		return err
	}
	return p.addDataPoint(data)
}

func (p *ProbePoller) Shutdown(ctx context.Context) error {
	if p.ticker != nil {
		p.ticker.Stop()
	}
	return p.Probe.OnShutdown(ctx)
}
