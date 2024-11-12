package sensor

import (
	"context"
	"fmt"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type Sensor interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

type sensor struct {
	startedProbes []*probes.ProbePoller
	addDataPoint  data_store.AddCallback
	remoteConfig  *configuration.RemoteConfiguration
	logger        *logger.Logger
}

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

func (s *sensor) SyncConfiguration() error {
	validProbeIds := []string{}
	probeConfigs := s.remoteConfig.GetConfiguration().Probes
	for _, probeConfig := range probeConfigs {
		probeId := probes.GenerateProbeId(probeConfig)
		validProbeIds = append(validProbeIds, probeId)

		// Is there a probe with this configuration already running?
		for _, startedProbe := range s.startedProbes {
			if startedProbe.ProbeId == probeId {
				continue
			}
		}

		// Start a new probe poller
		err := s.startProbe(probeConfig, nil)
		if err != nil {
			s.logger.Error().Err(err).
				Str("probe_name", probeConfig.Name).
				Any("probe_params", probeConfig.Params).
				Msg("error starting probe")
		}
	}

	// Stop probes that are no longer in the Configuration
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
				s.logger.Error().Err(err).
					Str("probe_name", startedProbe.GetName()).
					Msg("error stopping probe")
			}
		}
	}

	return nil
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	s.SyncConfiguration()
	s.remoteConfig.OnConfigChanged(func(string) { s.SyncConfiguration() })

	return nil
}

func (s *sensor) startProbe(probeConfig configuration.ProbeConfig, quitChannel chan struct{}) error {
	probeId := probes.GenerateProbeId(probeConfig)

	// Is there a probe with this configuration already running?
	for _, startedProbe := range s.startedProbes {
		if startedProbe.ProbeId == probeId {
			return nil
		}
	}

	// Start a new probe poller
	probePoller, err := probes.NewProbePoller(probeConfig, s.addDataPoint)
	if err != nil {
		return err
	}

	s.startedProbes = append(s.startedProbes, probePoller)

	return probePoller.Start(quitChannel)
}

func (s *sensor) Shutdown(ctx context.Context) error {
	fmt.Println("Shutting down sensor")
	for _, probePoller := range s.startedProbes {
		err := probePoller.Shutdown(ctx)
		if err != nil {
			s.logger.Error().Err(err).
				Str("probe_name", probePoller.GetName()).
				Msg("error shutting down probe")
		}
	}
	return nil
}
