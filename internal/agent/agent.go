package agent

import (
	"context"
	"log"
	"os"
	"syscall"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/senhub_server"
	"senhub-agent.go/internal/agent/services/sensor"
)

type Service interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

type Agent interface {
	Start() error
	Shutdown(context.Context) error
}

type agent struct {
	// List of services that have been started
	startedServices *[]Service
	// Channel to communicate with services
	messageChannel chan struct{}

	senhubServer        senhub_server.SenhubServer
	localConfiguration  configuration.LocalConfiguration
	remoteConfiguration configuration.RemoteConfiguration
	store               data_store.DataStore
	sensors             sensor.Sensor
}

// Create new agent from context
func NewAgent() Agent {
	localConfiguration := configuration.NewLocalConfiguration()

	senhubServer := senhub_server.NewSenhubServer(
		localConfiguration.GetAuthenticationKey(),
		localConfiguration.GetServerUrl(),
	)
	remoteConfiguration := configuration.NewRemoteConfiguration(senhubServer)
	store := data_store.NewDataStore(senhubServer)
	sensors := sensor.NewSensor(store.GetCallback())

	return agent{
		startedServices: &[]Service{},
		messageChannel:  make(chan struct{}),

		senhubServer:        senhubServer,
		localConfiguration:  localConfiguration,
		remoteConfiguration: remoteConfiguration,
		store:               store,
		sensors:             sensors,
	}
}

func (a agent) Start() error {
	// List of all services that should be started
	servicesToStart := []Service{
		a.remoteConfiguration,
		a.store,
		a.sensors,
	}

	// Attempt to start all services
	// Create a message channel to communicate with the services
	var errors []error
	for _, service := range servicesToStart {

		if err := service.Start(a.messageChannel); err != nil {
			log.Printf("Error starting service: %s\n %v", service.GetName(), err)
			errors = append(errors, err)
		} else {
			log.Printf("Service started: %s", service.GetName())
			*a.startedServices = append(*a.startedServices, service)
		}
	}

	if len(errors) > 0 {
		a.handleStartError()
	}

	return nil
}

func (a agent) Shutdown(ctx context.Context) error {
	// Notify all services to shutdown
	close(a.messageChannel)

	// Explicitely call Shutdown method on all services
	// Not sure this is usefull since the message channel already notified to close
	var errors []error
	for _, service := range *a.startedServices {
		if err := service.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down service: %s %v", service.GetName(), err)
			errors = append(errors, err)
		} else {
			log.Printf("Service shut down: %s", service.GetName())
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

func (a agent) handleStartError() {
	// Attempt to shutdown all services that were started
	// by sending a SIGTEMR signal to the agent process
	pid := os.Getpid()
	p, err := os.FindProcess(pid)
	if err != nil {
		log.Fatal(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		log.Fatal(err)
	}
}
