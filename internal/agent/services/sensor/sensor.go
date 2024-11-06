package sensor

import (
	"context"
	"fmt"
	"log"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

type Sensor interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

type sensor struct {
	addDataPoint data_store.AddCallback
}

func NewSensor(addDataPoint data_store.AddCallback) Sensor {
	return &sensor{
		addDataPoint: addDataPoint,
	}
}

func (s *sensor) GetName() string {
	return "Sensor"
}

func (s *sensor) Start(quitChannel chan struct{}) error {
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := s.doCollectSensors()
				if err != nil {
					log.Printf("error collecting sensors: %v", err)
				}

			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return s.doCollectSensors()
}

func (s *sensor) doCollectSensors() error {
	log.Println("collecting sensors")
	s.addDataPoint([]data_store.DataPoint{
		{
			Name:  "temperature",
			Value: "25",
		},
	})

	return nil
}

func (s *sensor) Shutdown(ctx context.Context) error {
	fmt.Println("Shutting down sensor")
	return nil
}
