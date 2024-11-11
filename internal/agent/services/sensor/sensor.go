package sensor

import (
	"context"
	"fmt"
	"log"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
)

type Sensor interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

type sensor struct {
	addDataPoint  data_store.AddCallback
	remoteConfig  *configuration.RemoteConfiguration
	startedProbes []*probes.ProbePoller
}

func NewSensor(addDataPoint data_store.AddCallback, remoteConfig *configuration.RemoteConfiguration) Sensor {
	return &sensor{
		addDataPoint:  addDataPoint,
		remoteConfig:  remoteConfig,
		startedProbes: []*probes.ProbePoller{},
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	probeConfigs := s.remoteConfig.GetConfiguration().Probes
	for _, probeConfig := range probeConfigs {
		err := s.startProbe(probeConfig, quitChannel)
		if err != nil {
			log.Printf("error starting probe %s: %v", probeConfig, err)
		}
	}

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
			log.Printf("error shutting down probe %s: %v", probePoller.GetName(), err)
		}
	}
	return nil
}
