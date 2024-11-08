package agent

import (
	"context"
	"log"
	"os"
	"syscall"

	"github.com/alexflint/go-arg"
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
	agentConfiguration  configuration.AgentConfiguration
	remoteConfiguration *configuration.RemoteConfiguration
	store               data_store.DataStore
	sensors             sensor.Sensor
}

type AgentCliArgs struct {
	AuthenticationKey string `arg:"required,--authentication-key,env:SENHUB_KEY"`
	ServerUrl         string `arg:"--server-url,env:SENHUB_SERVER_URL" default:"https://nats.sensorfactory.eu:8443"`
}

// Create new agent from context
func NewAgent() Agent {
	var args AgentCliArgs
	arg.MustParse(&args)

	agentConfiguration := configuration.NewAgentConfiguration(
		args.AuthenticationKey,
		args.ServerUrl,
	)

	senhubServer := senhub_server.NewSenhubServer(
		agentConfiguration.GetAuthenticationKey(),
		agentConfiguration.GetServerUrl(),
	)
	remoteConfiguration := configuration.NewRemoteConfiguration(senhubServer)
	store := data_store.NewDataStore(
		agentConfiguration,
		remoteConfiguration,
	)
	sensors := sensor.NewSensor(store.GetCallback(), remoteConfiguration)

	return agent{
		startedServices: &[]Service{},
		messageChannel:  make(chan struct{}),

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
