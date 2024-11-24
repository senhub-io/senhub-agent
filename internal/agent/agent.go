package agent

import (
	"context"
	"log"
	"os"
	"syscall"

	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
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

	logger              *logger.Logger
	senhubServer        senhub_server.SenhubServer
	agentConfiguration  configuration.AgentConfiguration
	remoteConfiguration *configuration.RemoteConfiguration
	store               data_store.DataStore
	sensors             sensor.Sensor
}

// Create new agent from context
func NewAgent() Agent {

	args := agentCliArgs.MustParse()

	logger := logger.NewLogger(args)
	logger.Debug().
		Any("args", args).
		Msg("Agent configuration")

	agentConfiguration := configuration.NewAgentConfiguration(
		args.AuthenticationKey,
		args.ServerUrl,
	)

	senhubServer := senhub_server.NewSenhubServer(
		agentConfiguration.GetAuthenticationKey(),
		agentConfiguration.GetServerUrl(),
		logger,
	)
	remoteConfiguration := configuration.NewRemoteConfiguration(
		senhubServer,
		logger,
	)
	store := data_store.NewDataStore(
		agentConfiguration,
		remoteConfiguration,
		logger,
	)
	sensors := sensor.NewSensor(
		store.GetCallback(),
		remoteConfiguration,
		logger,
	)

	return agent{
		startedServices: &[]Service{},
		messageChannel:  make(chan struct{}),

		logger:              logger,
		senhubServer:        senhubServer,
		agentConfiguration:  agentConfiguration,
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
		a.logger.Info().
			Str("service", service.GetName()).
			Msg("Starting service")
		if err := service.Start(a.messageChannel); err != nil {
			a.logger.Error().Err(err).
				Str("service", service.GetName()).
				Msg("Error starting service")
			errors = append(errors, err)
		} else {
			a.logger.Info().
				Str("service", service.GetName()).
				Msg("Service started")
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
		a.logger.Info().
			Str("service", service.GetName()).
			Msg("Stopping service")
		if err := service.Shutdown(ctx); err != nil {
			a.logger.Error().Err(err).
				Str("service", service.GetName()).
				Msg("Error shutting down service")
			errors = append(errors, err)
		} else {
			a.logger.Info().
				Str("service", service.GetName()).
				Msg("Service shut down")
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
		a.logger.Error().Err(err).Msg("Error finding process")
		log.Fatal(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		a.logger.Error().Err(err).Msg("Error sending SIGTERM signal")
		log.Fatal(err)
	}
}
