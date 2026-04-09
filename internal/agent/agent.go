// Package agent manages the core agent lifecycle and services orchestration
package agent

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/yaml.v2"
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
		remoteConfiguration = configuration.NewRemoteConfiguration(senhubServer, logger, args)
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
		os.Exit(1)
	}
	// On Windows, SIGTERM is not supported — use SIGINT or exit directly
	if err := p.Signal(syscall.SIGINT); err != nil {
		a.logger.Error().Err(err).Msg("Error sending shutdown signal")
		os.Exit(1)
	}
}

// LocalConfigInfo represents essential configuration information extracted from local config file
// This struct supports backward compatibility with legacy configuration formats
type LocalConfigInfo struct {
	AuthenticationKey string // Agent authentication key from config file
	Mode              string // Operating mode: "online" or "offline"
	IsValid           bool   // Whether the configuration was successfully parsed
	ConfigPath        string // Path where the configuration was found
}

// DetectAgentMode automatically determines if the agent should run in offline or online mode
// This is the public interface for mode detection, used by the CLI
func DetectAgentMode(args *agentCliArgs.ParsedArgs) bool {
	// Create a temporary logger for mode detection (minimal logging)
	tempLogger := logger.NewLogger(args)
	return detectAgentMode(args, tempLogger)
}

// detectAgentMode automatically determines if the agent should run in offline or online mode
// This function implements backward compatibility with legacy CLI-only configurations
// while supporting new configuration file-based deployments
func detectAgentMode(args *agentCliArgs.ParsedArgs, logger *logger.Logger) bool {
	// If offline mode is explicitly set via CLI, respect it (legacy compatibility)
	if args.Offline {
		logger.Info().Msg("Offline mode explicitly requested via CLI")
		return true
	}

	// Attempt to load configuration from local file (new configuration-driven approach)
	localConfig := loadLocalConfigInfo(args, logger)

	if localConfig.IsValid {
		// Configuration file found and parsed successfully
		logger.Info().
			Str("config_path", localConfig.ConfigPath).
			Str("mode", localConfig.Mode).
			Str("auth_key", maskAuthenticationKey(localConfig.AuthenticationKey)).
			Msg("Configuration file detected - using file-based configuration")

		// Set mode and config path first
		args.ConfigPath = localConfig.ConfigPath
		isOfflineMode := localConfig.Mode == "offline"

		// Handle authentication key based on mode
		if isOfflineMode {
			// OFFLINE MODE: Always use config file key, ignore CLI key
			if args.AuthenticationKey != "" && args.AuthenticationKey != localConfig.AuthenticationKey {
				logger.Warn().
					Str("cli_key", maskAuthenticationKey(args.AuthenticationKey)).
					Str("config_key", maskAuthenticationKey(localConfig.AuthenticationKey)).
					Msg("CLI authentication key ignored in offline mode - using config file key")
			}
			args.AuthenticationKey = localConfig.AuthenticationKey
			args.Offline = true
			return true
		} else if localConfig.Mode == "online" {
			// ONLINE MODE: Allow CLI key override with validation
			if args.AuthenticationKey == "" {
				// No CLI key provided - use the one from config file
				args.AuthenticationKey = localConfig.AuthenticationKey
				logger.Info().Msg("Using authentication key from configuration file")
			} else if args.AuthenticationKey != localConfig.AuthenticationKey {
				// CLI key differs from config file - validate CLI key first, fallback to config
				logger.Warn().
					Str("cli_key", maskAuthenticationKey(args.AuthenticationKey)).
					Str("config_key", maskAuthenticationKey(localConfig.AuthenticationKey)).
					Msg("Authentication key mismatch between CLI and config file")

				// Test CLI key first, if it fails, use config file key
				if !validateAuthenticationKey(args.AuthenticationKey, args.ServerUrl, logger) {
					logger.Warn().Msg("CLI authentication key validation failed, falling back to config file key")
					args.AuthenticationKey = localConfig.AuthenticationKey
				} else {
					logger.Info().Msg("CLI authentication key validated successfully, using CLI key")
				}
			}
			args.Offline = false
			return false
		} else {
			logger.Warn().
				Str("mode", localConfig.Mode).
				Msg("Unknown mode in configuration file, defaulting based on other factors")
		}
	}

	// Fallback to legacy detection logic (backward compatibility)
	// This maintains compatibility with existing deployments that don't have config files

	// Check if configuration file exists but couldn't be parsed
	// Use the absolute config path already computed in loadLocalConfigInfo
	absoluteConfigPath := localConfig.ConfigPath

	if _, err := os.Stat(absoluteConfigPath); err == nil && !localConfig.IsValid {
		// Config file exists but couldn't be parsed - try offline mode anyway
		logger.Warn().
			Str("config_path", absoluteConfigPath).
			Msg("Configuration file found but invalid - attempting offline mode")
		args.Offline = true
		args.ConfigPath = absoluteConfigPath
		return true
	}

	// No valid config file - check if we have auth key for online mode (legacy path)
	if args.AuthenticationKey == "" {
		logger.Error().
			Str("config_path", absoluteConfigPath).
			Msg("No valid configuration file found and no authentication key provided")
		logger.Info().Msg("To run in offline mode: install the agent first with 'install --offline'")
		logger.Info().Msg("To run in online mode: provide authentication key with '--authentication-key YOUR_KEY'")
		log.Fatal("Cannot determine agent mode - need either valid config file or authentication key")
	}

	// Have auth key but no config file - online mode (legacy compatibility)
	logger.Info().Msg("Authentication key provided via CLI - running in online mode")
	return false
}

// loadLocalConfigInfo attempts to load and parse local configuration file
// Returns configuration information for mode detection and key extraction
func loadLocalConfigInfo(args *agentCliArgs.ParsedArgs, logger *logger.Logger) LocalConfigInfo {
	// Use absolute path based on binary location to fix Windows Service issue
	absolutePath, err := agentCliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("config_path", args.ConfigPath).
			Msg("Failed to determine absolute config path, using fallback")
		// Fallback: try to use provided path or compute from binary location manually
		if args.ConfigPath != "" && filepath.IsAbs(args.ConfigPath) {
			// Provided path is already absolute, use it
			absolutePath = args.ConfigPath
		} else {
			// Last resort: try to get binary directory manually
			if execPath, execErr := os.Executable(); execErr == nil {
				binDir := filepath.Dir(execPath)
				if args.ConfigPath != "" {
					absolutePath = filepath.Join(binDir, args.ConfigPath)
				} else {
					absolutePath = filepath.Join(binDir, "agent-config.yaml")
				}
			} else {
				// Ultimate fallback if even os.Executable() fails
				logger.Error().Err(execErr).Msg("Failed to get executable path, using current directory")
				if args.ConfigPath != "" {
					absolutePath = args.ConfigPath
				} else {
					absolutePath = "./agent-config.yaml"
				}
			}
		}
	}

	result := LocalConfigInfo{
		ConfigPath: absolutePath,
		IsValid:    false,
	}

	// Check if configuration file exists
	if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
		logger.Debug().
			Str("config_path", absolutePath).
			Msg("No local configuration file found")
		return result
	}

	// Read configuration file (path is absolute based on binary location)
	data, err := os.ReadFile(filepath.Clean(absolutePath)) // #nosec G304 - absolutePath is computed from binary location
	if err != nil {
		logger.Warn().
			Err(err).
			Str("config_path", absolutePath).
			Msg("Failed to read configuration file")
		return result
	}

	// Parse YAML configuration (supports both new and legacy formats)
	var config struct {
		Agent struct {
			Key  string `yaml:"key"`
			Mode string `yaml:"mode"`
		} `yaml:"agent"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		logger.Warn().
			Err(err).
			Str("config_path", absolutePath).
			Msg("Failed to parse configuration file as YAML")
		return result
	}

	// Extract configuration information
	result.AuthenticationKey = config.Agent.Key
	result.Mode = config.Agent.Mode
	result.IsValid = true

	// Validate extracted information
	if result.AuthenticationKey == "" {
		logger.Warn().
			Str("config_path", absolutePath).
			Msg("No authentication key found in configuration file")
		result.IsValid = false
	}

	if result.Mode == "" {
		logger.Debug().
			Str("config_path", absolutePath).
			Msg("No mode specified in configuration file, will determine automatically")
		result.Mode = "offline" // Default assumption for config files
	}

	return result
}

// validateAuthenticationKey performs a quick validation of authentication key
// This is used for backward compatibility when CLI and config keys differ
func validateAuthenticationKey(key, serverUrl string, logger *logger.Logger) bool {
	if key == "" {
		return false
	}

	// Basic format validation (UUID-like format expected)
	if len(key) < 10 {
		logger.Debug().Msg("Authentication key too short")
		return false
	}

	// For now, we'll do basic validation. In the future, this could include
	// a lightweight server ping to validate the key
	logger.Debug().
		Str("key", maskAuthenticationKey(key)).
		Msg("Authentication key format appears valid")

	return true
}

// maskAuthenticationKey masks authentication key for secure logging
func maskAuthenticationKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
