package sensor

import (
	"context"
	"fmt"
	"log"
	"time"

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
	config        *configuration.RemoteConfiguration
	startedProbes []probes.Probe
}

func NewSensor(addDataPoint data_store.AddCallback, config *configuration.RemoteConfiguration) Sensor {
	return &sensor{
		addDataPoint:  addDataPoint,
		config:        config,
		startedProbes: []probes.Probe{},
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	for _, probe := range probes.AllProbes {
		p := probe(s.config)
		s.startedProbes = append(s.startedProbes, p)
		go func(p probes.Probe) {
			err := s.startProbe(p, quitChannel)
			if err != nil {
				log.Printf("error starting probe %s: %v", p.GetName(), err)
			}
		}(p)
	}

	return nil
}

func (s *sensor) startProbe(p probes.Probe, quitChannel chan struct{}) error {
	if p.ShouldStart() == false {
		return nil
	}

	p.OnStart(quitChannel)

	ticker := time.NewTicker(p.GetInterval())
	go func() {
		s.doCollectProbe(p)
		for {
			select {
			case <-ticker.C:
				err := s.doCollectProbe(p)
				if err != nil {
					log.Printf("error collecting sensors: %v", err)
				}

			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (s *sensor) doCollectProbe(p probes.Probe) error {
	data, err := p.Collect()
	if err != nil {
		return err
	}
	return s.addDataPoint(data)
}

func (s *sensor) Shutdown(ctx context.Context) error {
	fmt.Println("Shutting down sensor")
	for _, probe := range s.startedProbes {
		err := probe.OnShutdown(ctx)
		if err != nil {
			log.Printf("error shutting down probe %s: %v", probe.GetName(), err)
		}
	}
	return nil
}
