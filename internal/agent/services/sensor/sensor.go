// Package sensor handles the configuration and lifecycle of monitoring probes
package sensor

import (
	"context"
	"fmt"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// Sensor defines interface for starting and stopping probes
type Sensor interface {
	// GetName returns service identifier
	GetName() string
	// Start launches configured probes
	Start(chan struct{}) error
	// Shutdown gracefully stops probes
	Shutdown(context.Context) error
}

type sensor struct {
	startedProbes []*probes.ProbePoller
	addDataPoint  data_store.AddCallback
	remoteConfig  *configuration.RemoteConfiguration
	logger        *logger.Logger
}

// NewSensor creates a new Sensor instance
func NewSensor(
	addDataPoint data_store.AddCallback,
	remoteConfig *configuration.RemoteConfiguration,
	logger *logger.Logger,
) Sensor {
	localLogger := logger.With().Str("service", "Sensor").Logger()
	return &sensor{
		startedProbes: []*probes.ProbePoller{},
		addDataPoint:  addDataPoint,
		remoteConfig:  remoteConfig,
		logger:        &localLogger,
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

// SyncConfiguration synchronizes probes with current configuration
func (s *sensor) SyncConfiguration() error {
	validProbeIds := []string{}
	probeConfigs := s.remoteConfig.GetConfiguration().Probes

	for _, probeConfig := range probeConfigs {
		probeId := probes.GenerateProbeId(probeConfig)
		validProbeIds = append(validProbeIds, probeId)

		for _, startedProbe := range s.startedProbes {
			if startedProbe.ProbeId == probeId {
				continue
			}
		}

		err := s.startProbe(probeConfig, nil)
		if err != nil {
			fmt.Printf("Error starting probe %s: %v\n", probeConfig.Name, err)
		}
	}

	for _, startedProbe := range s.startedProbes {
		found := false
		for _, validProbeId := range validProbeIds {
			if startedProbe.ProbeId == validProbeId {
				found = true
				break
			}
		}
		if !found {
			err := startedProbe.Shutdown(context.Background())
			if err != nil {
				fmt.Printf("Error stopping probe %s: %v\n", startedProbe.GetName(), err)
			}
		}
	}
	return nil
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	if err := s.SyncConfiguration(); err != nil {
		return fmt.Errorf("failed to sync configuration: %w", err)
	}
	fmt.Printf("Starting sensor service\n")
	s.remoteConfig.OnConfigChanged(func(string) { s.SyncConfiguration() })
	return nil
}

func (s *sensor) startProbe(probeConfig configuration.ProbeConfig, quitChannel chan struct{}) error {
	probeId := probes.GenerateProbeId(probeConfig)

	for _, startedProbe := range s.startedProbes {
		if startedProbe.ProbeId == probeId {
			return nil
		}
	}

	localLogger := s.logger.With().
		Str("probe_name", probeConfig.Name).
		Any("probe_params", probeConfig.Params).
		Logger()

	fmt.Printf("Starting probe %s\n", probeConfig.Name)

	probePoller, err := probes.NewProbePoller(probeConfig, &localLogger, s.addDataPoint)
	if err != nil {
		return fmt.Errorf("failed to create probe poller: %w", err)
	}

	s.startedProbes = append(s.startedProbes, probePoller)
	return probePoller.Start(quitChannel)
}

func (s *sensor) Shutdown(ctx context.Context) error {
	fmt.Printf("Shutting down sensor\n")

	for _, probePoller := range s.startedProbes {
		fmt.Printf("Shutting down probe %s\n", probePoller.GetName())

		err := probePoller.Shutdown(ctx)
		if err != nil {
			fmt.Printf("Error shutting down probe %s: %v\n",
				probePoller.GetName(), err)
		}
	}
	return nil
}
