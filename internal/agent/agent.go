// Package agent manages the core agent lifecycle and services orchestration
package agent

import (
	"context"
	"log"
	"os"
	"syscall"

	agentCliArgs "senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
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
	localConfiguration  *configuration.LocalConfiguration
	store               data_store.DataStore
	sensors             sensor.Sensor
	updater             auto_update.AutoUpdate
	isOfflineMode       bool
}

// NewAgent initializes new agent with required services
func NewAgent() Agent {
	args := agentCliArgs.MustParse()
	return NewAgentWithArgs(args)
}

// NewAgentWithArgs initializes new agent with provided CLI arguments
func NewAgentWithArgs(args *agentCliArgs.ParsedArgs) Agent {
	logger := logger.NewLogger(args)
	logger.Debug().Any("args", args).Msg("Agent configuration")

	// Auto-detect mode based on configuration file existence and auth key
	isOfflineMode := detectAgentMode(args, logger)

	// Initialize configuration provider based on mode
	var configProvider configuration.ConfigurationProvider
	var remoteConfiguration *configuration.RemoteConfiguration
	var localConfiguration *configuration.LocalConfiguration
	var senhubServer server.Server
	var agentKey string

	if isOfflineMode {
		logger.Info().Msg("Initializing agent in offline mode")
		localConfiguration = configuration.NewLocalConfiguration(args, logger)
		configProvider = localConfiguration

		// Get the agent key directly from the local configuration data
		agentKey = localConfiguration.GetAgentKey()
		if agentKey == "" {
			// Fallback to args if config doesn't have a key
			agentKey = args.AuthenticationKey
			if agentKey == "" {
				agentKey = "offline-pending" // Last resort temporary value
			}
		}
	} else {
		agentKey = args.AuthenticationKey
		logger.Info().Msg("Initializing agent in online mode")
	}

	// Create agent configuration with the correct key
	var agentConfiguration configuration.AgentConfiguration
	if isOfflineMode && localConfiguration != nil {
		// In offline mode, create agentConfiguration with LocalConfiguration reference
		agentConfiguration = configuration.NewAgentConfigurationWithLocal(
			agentKey,
			args.ServerUrl,
			localConfiguration,
			logger,
		)
	} else {
		// In online mode, create standard agentConfiguration
		agentConfiguration = configuration.NewAgentConfiguration(
			agentKey,
			args.ServerUrl,
			logger,
		)
	}

	if !isOfflineMode {
		senhubServer = server.NewServer(
			agentConfiguration.GetAuthenticationKey(),
			agentConfiguration.GetServerUrl(),
			logger,
		)
		remoteConfiguration = configuration.NewRemoteConfiguration(senhubServer, logger)
		configProvider = remoteConfiguration

		// Update agentConfiguration to include remoteConfiguration reference
		agentConfiguration = configuration.NewAgentConfigurationWithRemote(
			agentKey,
			args.ServerUrl,
			remoteConfiguration,
			logger,
		)
	}

	store := data_store.NewDataStore(
		agentConfiguration,
		configProvider,
		logger,
	)

	sensors := sensor.NewSensor(
		store.GetCallback(),
		configProvider,
		logger,
	)

	var updater auto_update.AutoUpdate
	if !isOfflineMode {
		// Create auto-updater in online mode
		updater = auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
			ConfigSource: remoteConfiguration,
			Logger:       logger,
			DryRun:       false,
		})
	} else if localConfiguration != nil {
		// In offline mode, check if auto-update is enabled
		autoUpdateConfig := localConfiguration.GetAutoUpdateConfig()
		if autoUpdateConfig.Enabled {
			logger.Info().
				Str("url", autoUpdateConfig.URL).
				Bool("enabled", autoUpdateConfig.Enabled).
				Msg("🔄 Auto-update enabled in offline mode")

			updater = auto_update.NewAutoUpdate(auto_update.AutoUpdateConfig{
				ConfigSource: localConfiguration,
				Logger:       logger,
				DryRun:       false,
			})
		} else {
			logger.Info().Msg("Auto-update disabled in offline mode")
		}
	}

	return agent{
		startedServices:     &[]Service{},
		messageChannel:      make(chan struct{}),
		logger:              logger,
		senhubServer:        senhubServer,
		agentConfiguration:  agentConfiguration,
		remoteConfiguration: remoteConfiguration,
		localConfiguration:  localConfiguration,
		store:               store,
		sensors:             sensors,
		updater:             updater,
		isOfflineMode:       isOfflineMode,
	}
}

func (a agent) Start() error {
	var servicesToStart []Service

	if a.isOfflineMode {
		// Offline mode: start local configuration, store, and sensors
		servicesToStart = []Service{
			a.localConfiguration,
			a.store,
			a.sensors,
		}

		// Add auto-updater if enabled in offline mode
		if a.updater != nil {
			a.logger.Info().Msg("Adding auto-updater to offline mode services")
			servicesToStart = append(servicesToStart, a.updater)
		}
	} else {
		// Online mode: start remote configuration, store, sensors, and updater
		servicesToStart = []Service{
			a.remoteConfiguration,
			a.store,
			a.sensors,
			a.updater,
		}
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

// detectAgentMode automatically determines if the agent should run in offline or online mode
func detectAgentMode(args *agentCliArgs.ParsedArgs, logger *logger.Logger) bool {
	// If offline mode is explicitly set, respect it
	if args.Offline {
		logger.Info().Msg("Offline mode explicitly requested")
		return true
	}

	// Check if configuration file exists
	configPath := args.ConfigPath
	if configPath == "" {
		configPath = "./agent-config.yaml"
	}

	if _, err := os.Stat(configPath); err == nil {
		// Config file exists - auto-detect offline mode
		logger.Info().
			Str("config_path", configPath).
			Msg("Configuration file detected - automatically switching to offline mode")

		// Update args to reflect the detected mode
		args.Offline = true
		args.ConfigPath = configPath
		return true
	}

	// No config file - check if we have auth key for online mode
	if args.AuthenticationKey == "" {
		logger.Error().
			Str("config_path", configPath).
			Msg("No configuration file found and no authentication key provided")
		logger.Info().Msg("To run in offline mode: install the agent first with 'install --offline'")
		logger.Info().Msg("To run in online mode: provide authentication key with '--authentication-key YOUR_KEY'")
		log.Fatal("Cannot determine agent mode - need either config file or authentication key")
	}

	// Have auth key but no config file - online mode
	logger.Info().Msg("Authentication key provided - running in online mode")
	return false
}
