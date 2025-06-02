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
	startedProbes  []*probes.ProbePoller
	addDataPoint   data_store.AddCallback
	configProvider configuration.ConfigurationProvider
	logger         *logger.Logger
}

// NewSensor creates a new Sensor instance
func NewSensor(
	addDataPoint data_store.AddCallback,
	configProvider configuration.ConfigurationProvider,
	logger *logger.Logger,
) Sensor {
	localLogger := logger.With().Str("service", "Sensor").Logger()
	return &sensor{
		startedProbes:  []*probes.ProbePoller{},
		addDataPoint:   addDataPoint,
		configProvider: configProvider,
		logger:         &localLogger,
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

// SyncConfiguration synchronizes probes with current configuration
func (s *sensor) SyncConfiguration() error {
	validProbeIds := []string{}
	probeConfigs := s.configProvider.GetConfiguration().Probes

	for _, probeConfig := range probeConfigs {
		probeId := probes.GenerateProbeId(probeConfig)
		validProbeIds = append(validProbeIds, probeId)
		probeLogger := s.getLoggerForProbe(probeConfig)

		for _, startedProbe := range s.startedProbes {
			if startedProbe.ProbeId == probeId {
				continue
			}
		}

		err := s.startProbe(probeConfig, nil)
		if err != nil {
			// For now, just log the error and continue
			// Handling this error differently is not straightforward
			// because this is done asynchronously.
			probeLogger.Error().Err(err).Msgf("Error starting probe")
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
				probeLogger := s.logger.With().
					Str("probe_name", startedProbe.Probe.GetName()).
					Logger()
				probeLogger.Error().Err(err).Msgf("Error stopping probe")
			}
		}
	}
	return nil
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	s.logger.Info().Msg("Starting sensor")
	if err := s.SyncConfiguration(); err != nil {
		return fmt.Errorf("failed to sync configuration: %w", err)
	}

	s.logger.Info().Msg("Starting sensor service")
	s.configProvider.OnConfigChanged(func(string) { s.SyncConfiguration() })
	return nil
}

func (s *sensor) getLoggerForProbe(probeConfig configuration.ProbeConfig) *logger.Logger {
	logger := s.logger.With().
		Str("probe_name", probeConfig.Name).
		Any("probe_params", probeConfig.Params).
		Logger()
	return &logger
}

func (s *sensor) startProbe(probeConfig configuration.ProbeConfig, quitChannel chan struct{}) error {
	probeId := probes.GenerateProbeId(probeConfig)

	for _, startedProbe := range s.startedProbes {
		if startedProbe.ProbeId == probeId {
			return nil
		}
	}

	probePoller, err := probes.NewProbePoller(
		probeConfig,
		s.getLoggerForProbe(probeConfig),
		s.addDataPoint,
	)
	if err != nil {
		return fmt.Errorf("Failed to create probe poller: %w", err)
	}

	s.startedProbes = append(s.startedProbes, probePoller)
	return probePoller.Start(quitChannel)
}

func (s *sensor) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sensor")

	for _, probePoller := range s.startedProbes {
		s.logger.Debug().
			Str("probe_name", probePoller.GetName()).
			Msg("Shutting down sensor")

		err := probePoller.Shutdown(ctx)
		if err != nil {
			s.logger.Error().
				Err(err).
				Str("probe_name", probePoller.GetName()).
				Msg("Error shutting down probe")
		}
	}
	return nil
}
