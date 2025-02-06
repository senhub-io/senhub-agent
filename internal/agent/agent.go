// Package agent manages the core agent lifecycle and services orchestration
package agent

import (
	"context"
	"fmt"
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
		fmt.Printf("Starting service %s\n", service.GetName())

		if err := service.Start(a.messageChannel); err != nil {
			fmt.Printf("Error starting service %s: %v\n", service.GetName(), err)
			errors = append(errors, err)
		} else {
			fmt.Printf("Service %s started\n", service.GetName())
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
		fmt.Printf("Stopping service %s\n", service.GetName())

		if err := service.Shutdown(ctx); err != nil {
			fmt.Printf("Error shutting down service %s: %v\n", service.GetName(), err)
			errors = append(errors, err)
		} else {
			fmt.Printf("Service %s shut down\n", service.GetName())
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
		fmt.Printf("Error finding process: %v\n", err)
		log.Fatal(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("Error sending SIGTERM signal: %v\n", err)
		log.Fatal(err)
	}
}
