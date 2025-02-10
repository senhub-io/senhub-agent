// Package agent manages the core agent lifecycle and services orchestration
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
	"senhub-agent.go/internal/agent/services/sensor"
	"senhub-agent.go/internal/agent/services/server"
)

// Service defines interface for agent services lifecycle
type Service interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

// Agent defines interface for main agent operations
type Agent interface {
	Start() error
	Shutdown(context.Context) error
}

type agent struct {
	startedServices     *[]Service
	messageChannel      chan struct{}
	logger              *logger.Logger
	senhubServer        server.Server
	agentConfiguration  configuration.AgentConfiguration
	remoteConfiguration *configuration.RemoteConfiguration
	store               data_store.DataStore
	sensors             sensor.Sensor
}

// NewAgent initializes new agent with required services
func NewAgent() Agent {
	args := agentCliArgs.MustParse()
	logger := logger.NewLogger(args)
	logger.Debug().Any("args", args).Msg("Agent configuration")

	agentConfiguration := configuration.NewAgentConfiguration(
		args.AuthenticationKey,
		args.ServerUrl,
		logger,
	)

	senhubServer := server.NewServer(
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
		startedServices:     &[]Service{},
		messageChannel:      make(chan struct{}),
		logger:              logger,
		senhubServer:        senhubServer,
		agentConfiguration:  agentConfiguration,
		remoteConfiguration: remoteConfiguration,
		store:               store,
		sensors:             sensors,
	}
}

func (a agent) Start() error {
	servicesToStart := []Service{
		a.remoteConfiguration,
		a.store,
		a.sensors,
	}

	var errors []error
	for _, service := range servicesToStart {
		a.logger.Debug().
			Str("service", service.GetName()).
			Msg("Starting service")

		if err := service.Start(a.messageChannel); err != nil {
			a.logger.Error().
				Str("service", service.GetName()).
				Err(err).
				Msg("Failed to start service")
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
	close(a.messageChannel)

	var errors []error
	for _, service := range *a.startedServices {
		a.logger.Debug().
			Str("service", service.GetName()).
			Msg("Shutting down service")

		if err := service.Shutdown(ctx); err != nil {
			a.logger.Error().
				Str("service", service.GetName()).
				Err(err).
				Msg("Failed to shut down service")
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
