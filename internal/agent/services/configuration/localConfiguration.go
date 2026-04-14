// LocalConfiguration handles offline configuration from YAML files
// Responsibilities:
// - Local YAML configuration loading
// - Agent key generation for offline mode
// - TLS certificate management for offline mode
package configuration

import (
	"context"
	"fmt"
	"os"

	"github.com/fsnotify/fsnotify"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// LocalConfigurationData represents the YAML configuration structure
type LocalConfigurationData struct {
	ConfigVersion int               `yaml:"config_version"` // Configuration format version
	Agent         LocalAgentConfig  `yaml:"agent"`
	Storage       []StorageConfig   `yaml:"storage"`
	Probes        []ProbeConfig     `yaml:"probes"`
	AutoUpdate    *AutoUpdateConfig `yaml:"auto_update,omitempty"`
	Cache         *CacheConfig      `yaml:"cache,omitempty"`
}

// LocalAgentConfig represents agent-specific configuration
type LocalAgentConfig struct {
	Key     string `yaml:"key"`
	Mode    string `yaml:"mode"`
	License string `yaml:"license,omitempty"` // JWT license token or JSON for testing
}

// TLSConfig represents TLS/HTTPS configuration
type TLSConfig struct {
	Enabled       bool     `yaml:"enabled"`
	MinTlsVersion string   `yaml:"min_tls_version"`
	CipherSuites  []string `yaml:"cipher_suites"`
}

// AutoUpdateConfig represents auto-update configuration
type AutoUpdateConfig struct {
	Enabled     bool   `yaml:"enabled"`
	IncludeBeta bool   `yaml:"include_beta"`
	URL         string `yaml:"url"`
}

// CacheConfig represents cache configuration
type CacheConfig struct {
	RetentionMinutes int `yaml:"retention_minutes"`
}

// LocalConfiguration manages offline configuration
type LocalConfiguration struct {
	data          LocalConfigurationData
	logger        *logger.ModuleLogger
	configPath    string
	args          *cliArgs.ParsedArgs
	eventNotifier *EventNotifier
	watcher       *fsnotify.Watcher
	quitChannel   chan struct{}
}

// NewLocalConfiguration creates a new LocalConfiguration instance
func NewLocalConfiguration(
	args *cliArgs.ParsedArgs,
	baseLogger *logger.Logger,
) *LocalConfiguration {
	// Create module-specific logger for local configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.local")
	moduleLogger.Debug().Msg("Creating new LocalConfiguration instance")

	// Determine config path using absolute path based on binary location
	// This fixes Windows Service issue where working directory != binary directory
	configPath := args.ConfigPath
	absolutePath, err := cliArgs.GetAbsoluteConfigPath(configPath)
	if err != nil {
		moduleLogger.Error().
			Err(err).
			Str("config_path", configPath).
			Msg("Failed to determine absolute config path, using provided path as-is")
		// Fallback to provided path or default
		if configPath == "" {
			configPath = "./agent-config.yaml"
		}
	} else {
		configPath = absolutePath
	}

	lc := &LocalConfiguration{
		logger:        moduleLogger,
		configPath:    configPath,
		args:          args,
		data:          LocalConfigurationData{},
		eventNotifier: NewEventNotifier(moduleLogger.Logger),
	}

	// Try to load existing configuration immediately
	if _, err := os.Stat(lc.configPath); err == nil {
		// File exists, load it
		if err := lc.loadConfiguration(); err != nil {
			moduleLogger.Warn().Err(err).Msg("Failed to load existing configuration, will use defaults")
		}
	}

	moduleLogger.Debug().
		Str("config_path", configPath).
		Msg("LocalConfiguration instance created successfully")
	return lc
}

// GetName returns the service name
func (lc *LocalConfiguration) GetName() string {
	return "LocalConfiguration"
}

// GetAgentKey returns the agent key from the local configuration
func (lc *LocalConfiguration) GetAgentKey() string {
	return lc.data.Agent.Key
}

// GetAuthenticationKey implements AgentConfiguration interface
func (lc *LocalConfiguration) GetAuthenticationKey() string {
	return lc.data.Agent.Key
}

// GetServerUrl implements AgentConfiguration interface
func (lc *LocalConfiguration) GetServerUrl() string {
	// In offline mode, we don't have a server URL
	return ""
}

// GetAutoUpdateConfig returns the auto-update configuration
func (lc *LocalConfiguration) GetAutoUpdateConfig() *AutoUpdateConfig {
	if lc.data.AutoUpdate == nil {
		// Return default configuration
		return &AutoUpdateConfig{
			Enabled: false,
			URL:     "https://eu-west-1.intake.senhub.io/releases",
		}
	}
	return lc.data.AutoUpdate
}

// GetCacheConfig returns the cache configuration
func (lc *LocalConfiguration) GetCacheConfig() *CacheConfig {
	if lc.data.Cache == nil {
		lc.logger.Warn().Msg("Cache configuration is nil in YAML, using default (5 minutes)")
		// Return default configuration
		return &CacheConfig{
			RetentionMinutes: 5,
		}
	}
	lc.logger.Info().
		Int("retention_minutes", lc.data.Cache.RetentionMinutes).
		Msg("Cache configuration loaded from YAML")
	return lc.data.Cache
}

// GetConfiguration returns the configuration data in RemoteConfigurationData format
func (lc *LocalConfiguration) GetConfiguration() RemoteConfigurationData {
	// Get auto-update configuration
	autoUpdate := lc.GetAutoUpdateConfig()

	// Convert auto-update interval based on enabled status
	var updateInterval int
	if autoUpdate.Enabled {
		updateInterval = 3600 // 1 hour in seconds
	} else {
		updateInterval = 0 // Disabled
	}

	// Convert local config format to remote config format
	return RemoteConfigurationData{
		StorageConfig: lc.data.Storage,
		Probes:        lc.data.Probes,
		Agent: AgentConfig{
			RegistryUrl:         autoUpdate.URL,
			Version:             "",
			UpdateCheckInterval: updateInterval,
			License:             lc.data.Agent.License,
			AuthenticationKey:   lc.data.Agent.Key,
		},
	}
}

// OnConfigChanged registers a callback for configuration changes
func (lc *LocalConfiguration) OnConfigChanged(callback func(string)) {
	lc.logger.Info().Msg("Registering new configuration change callback")
	lc.eventNotifier.RegisterObserver(callback)
}

// Start initializes the local configuration and begins file watching
func (lc *LocalConfiguration) Start(quitChannel chan struct{}) error {
	lc.logger.Info().Msg("Starting LocalConfiguration with file watching")
	lc.quitChannel = quitChannel

	// Migrate configuration if needed (before loading)
	migrator := NewConfigMigrator(lc.configPath, lc.logger.Logger)
	if err := migrator.MigrateIfNeeded(); err != nil {
		lc.logger.Warn().Err(err).Msg("Configuration migration failed, continuing with current format")
	}

	// Load or create configuration
	if err := lc.loadOrCreateConfiguration(); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize file watcher
	var err error
	lc.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Add config file to watcher
	err = lc.watcher.Add(lc.configPath)
	if err != nil {
		_ = lc.watcher.Close()
		return fmt.Errorf("failed to watch config file %s: %w", lc.configPath, err)
	}

	lc.logger.Info().Str("config_path", lc.configPath).Msg("Started watching configuration file")

	// Start watching goroutine
	go lc.watchConfigFile()

	return nil
}

// Shutdown performs cleanup and stops file watching
func (lc *LocalConfiguration) Shutdown(ctx context.Context) error {
	lc.logger.Info().Msg("Shutting down LocalConfiguration")

	if lc.watcher != nil {
		if err := lc.watcher.Close(); err != nil {
			lc.logger.Warn().Err(err).Msg("Error closing file watcher")
		}
	}

	return nil
}
