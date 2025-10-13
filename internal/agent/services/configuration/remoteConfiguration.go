// RemoteConfiguration handles dynamic configuration
// Responsibilities:
// - Initial configuration loading
// - Hot configuration updates
// - Component change notifications
package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
)

// maskKey masks an authentication key for logging purposes
func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}

type StorageConfigParams = map[string]interface{}

type StorageConfig struct {
	Name   string              `json:"name"`
	Params StorageConfigParams `json:"params"`
}

type ProbeConfigParams = map[string]interface{}

type ProbeConfig struct {
	Name   string            `json:"name"`             // Display name (free-form)
	Type   string            `json:"type,omitempty"`   // Probe type (technical identifier)
	Params ProbeConfigParams `json:"params"`
}

type AgentConfig struct {
	RegistryUrl         string `json:"registry_url"`
	Version             string `json:"version"`
	UpdateCheckInterval any    `json:"update_check_interval" default:"3600"`
}

type RemoteConfigurationData struct {
	StorageConfig []StorageConfig `json:"storage"`
	Probes        []ProbeConfig   `json:"probes"`
	Agent         AgentConfig     `json:"agent"`
	Cache         *CacheConfig    `json:"cache,omitempty"`
}

type RemoteConfiguration struct {
	data             RemoteConfigurationData
	logger           *logger.ModuleLogger
	server           server.Server
	eventNotifier    *EventNotifier
	mutex            sync.Mutex
	scheduler        periodic_scheduler.PeriodicScheduler
	args             *cliArgs.ParsedArgs // CLI args for local replication
	localReplicaPath string              // Path for local replica file
	agentKey         string              // Agent authentication key
}

func NewRemoteConfiguration(
	serverClient server.Server,
	baseLogger *logger.Logger,
	args interface{}, // CLI args for accessing ConfigPath
) *RemoteConfiguration {
	// Create module-specific logger for remote configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.remote")
	moduleLogger.Debug().Msg("Creating new RemoteConfiguration instance")

	// Convert args to ParsedArgs for accessing ConfigPath
	var parsedArgs *cliArgs.ParsedArgs
	var localReplicaPath string
	var agentKey string

	if a, ok := args.(*cliArgs.ParsedArgs); ok {
		parsedArgs = a
		agentKey = a.AuthenticationKey

		// Determine local replica path
		if a.ConfigPath != "" {
			localReplicaPath = a.ConfigPath
		} else {
			localReplicaPath = "./agent-config.yaml" // Default path
		}

		moduleLogger.Debug().
			Str("local_replica_path", localReplicaPath).
			Str("agent_key", maskKey(agentKey)).
			Msg("Local configuration replication will be enabled")
	} else {
		moduleLogger.Warn().Msg("CLI args not provided, local replication disabled")
	}

	rc := &RemoteConfiguration{
		logger:           moduleLogger,
		server:           serverClient,
		data:             RemoteConfigurationData{},
		eventNotifier:    NewEventNotifier(moduleLogger.Logger),
		args:             parsedArgs,
		localReplicaPath: localReplicaPath,
		agentKey:         agentKey,
	}

	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          10 * time.Second,
		MaxRetries:        3,
		ExecuteOnStart:    true,
		ExecuteOnShutdown: false,
		Execute:           rc.UpdateSync,
	}, moduleLogger.Logger)
	rc.scheduler = scheduler

	moduleLogger.Debug().Msg("RemoteConfiguration instance created successfully")
	return rc
}

func (rc *RemoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (rc *RemoteConfiguration) GetConfiguration() RemoteConfigurationData {
	return rc.data
}

// GetCacheConfig returns cache configuration from remote config
func (rc *RemoteConfiguration) GetCacheConfig() *CacheConfig {
	if rc.data.Cache == nil {
		// Return default configuration if not provided by server
		return &CacheConfig{
			RetentionMinutes: 5,
		}
	}
	return rc.data.Cache
}

func (rc *RemoteConfiguration) OnConfigChanged(callback func(string)) {
	rc.logger.Debug().Msg("Registering new configuration change callback")
	rc.eventNotifier.RegisterObserver(callback)
}

func (rc *RemoteConfiguration) Start(quitChannel chan struct{}) error {
	rc.logger.Info().Msg("Starting RemoteConfiguration")
	if err := rc.scheduler.Start(quitChannel); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}
	return nil
}

func (rc *RemoteConfiguration) Shutdown(ctx context.Context) error {
	rc.logger.Info().Msg("Shutting down RemoteConfiguration")
	if err := rc.scheduler.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown scheduler: %w", err)
	}
	return nil
}

func (rc *RemoteConfiguration) validateStorageParams(storage StorageConfig) error {
	rc.logger.Debug().Msgf("Validating storage params for %s", storage.Name)

	switch storage.Name {
	case "senhub":
		return nil
	case "prtg", "event":
		if _, ok := storage.Params["server_url"]; !ok {
			return fmt.Errorf("%s storage requires server_url parameter", storage.Name)
		}
		serverURL, ok := storage.Params["server_url"].(string)
		if !ok || serverURL == "" {
			return fmt.Errorf("%s storage server_url must be a non-empty string", storage.Name)
		}
	case "http":
		// HTTP strategy is valid, no required parameters
		return nil
	default:
		return fmt.Errorf("unknown storage strategy: %s", storage.Name)
	}
	return nil
}

func (rc *RemoteConfiguration) validateConfiguration(config *RemoteConfigurationData) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	rc.logger.Debug().Msg("Validating configuration")

	if len(config.StorageConfig) == 0 {
		return fmt.Errorf("at least one storage strategy is required")
	}

	strategyNames := make(map[string]bool)
	for _, storage := range config.StorageConfig {
		if storage.Name == "" {
			return fmt.Errorf("storage strategy name cannot be empty")
		}
		if strategyNames[storage.Name] {
			return fmt.Errorf("duplicate storage strategy name: %s", storage.Name)
		}
		strategyNames[storage.Name] = true

		if err := rc.validateStorageParams(storage); err != nil {
			return fmt.Errorf("invalid params for strategy %s: %v", storage.Name, err)
		}
	}

	// Check for empty probe names, but allow multiple probes of the same type with different params
	// Each probe instance is uniquely identified by its ID (hash of name + params) in the sensor module
	for _, probe := range config.Probes {
		if probe.Name == "" {
			return fmt.Errorf("probe name cannot be empty")
		}
	}

	return nil
}

func (rc *RemoteConfiguration) UpdateSync() error {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()

	maxRetries := 3
	backoffDuration := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		rc.logger.Debug().
			Int("attempt", attempt+1).
			Int("max_retries", maxRetries).
			Str("retry", fmt.Sprintf("%d/%d", attempt+1, maxRetries)).
			Msgf("Fetching configuration attempt")

		config, err := rc.doFetchConfiguration()
		if err == nil {
			if err := rc.validateConfiguration(config); err != nil {
				rc.logger.Error().Err(err).Msg("Invalid configuration received")
				return fmt.Errorf("invalid configuration: %v", err)
			}

			if !reflect.DeepEqual(rc.data, *config) {
				rc.logger.Info().
					Any("old_config", rc.data).
					Any("new_config", *config).
					Msg("Configuration changed")
				rc.data = *config

				// Replicate configuration locally for transition purposes
				if err := rc.replicateConfigurationLocally(); err != nil {
					rc.logger.Warn().Err(err).Msg("Failed to replicate configuration locally")
				}

				rc.eventNotifier.NotifyObservers("Configuration changed")
			} else {
				rc.logger.Debug().Msg("Configuration unchanged")

				// Always attempt to create local replica if it doesn't exist (first run)
				if rc.localReplicaPath != "" {
					if _, err := os.Stat(rc.localReplicaPath); os.IsNotExist(err) {
						rc.logger.Info().Msg("Local replica doesn't exist, creating initial copy")
						if err := rc.replicateConfigurationLocally(); err != nil {
							rc.logger.Warn().Err(err).Msg("Failed to create initial local replica")
						}
					}
				}
			}
			return nil
		}

		rc.logger.Error().Err(err).Msg("Failed to fetch configuration")
		if attempt < maxRetries-1 {
			rc.logger.Info().Msgf("Retrying in %v seconds", backoffDuration)
			time.Sleep(backoffDuration)
			backoffDuration *= 2
		}
	}

	return fmt.Errorf("failed to fetch configuration after %d attempts", maxRetries)
}

func (rc *RemoteConfiguration) doFetchConfiguration() (*RemoteConfigurationData, error) {
	rc.logger.Debug().Msg("Fetching configuration from server")

	res, err := rc.server.Get("/configs")
	if err != nil {
		return nil, fmt.Errorf("server request failed: %v", err)
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response failed: %v", err)
	}

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d, response: %s",
			res.StatusCode, string(respBody))
	}

	rc.logger.Debug().
		Str("response", string(respBody)).
		Msg("Raw configuration response")

	var config RemoteConfigurationData
	if err := json.Unmarshal(respBody, &config); err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %v", err)
	}

	return &config, nil
}

// replicateConfigurationLocally creates a local YAML replica of the remote configuration
// This allows for easier transition between online and offline modes
func (rc *RemoteConfiguration) replicateConfigurationLocally() error {
	if rc.localReplicaPath == "" {
		return fmt.Errorf("local replica path not configured")
	}

	rc.logger.Debug().
		Str("replica_path", rc.localReplicaPath).
		Msg("Creating local configuration replica")

	// Convert RemoteConfigurationData to LocalConfigurationData format
	localConfig := LocalConfigurationData{
		Agent: LocalAgentConfig{
			Key:       rc.agentKey,
			Mode:      "online", // Important: this stays "online"
			Generated: false,    // Key was provided via CLI, not generated
		},
		Storage:    rc.data.StorageConfig,
		Probes:     rc.data.Probes,
		AutoUpdate: rc.convertAutoUpdateConfig(),
		Cache:      rc.data.Cache,
	}

	// Create directory if it doesn't exist
	configDir := filepath.Dir(rc.localReplicaPath)
	if err := os.MkdirAll(configDir, 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Generate YAML content
	yamlData, err := rc.generateConfigYAML(&localConfig)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	// Write configuration file
	if err := os.WriteFile(rc.localReplicaPath, yamlData, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	rc.logger.Info().
		Str("replica_path", rc.localReplicaPath).
		Msg("Local configuration replica created successfully")

	return nil
}

// convertAutoUpdateConfig converts remote AutoUpdateConfig to local format
func (rc *RemoteConfiguration) convertAutoUpdateConfig() *AutoUpdateConfig {
	if rc.data.Agent.UpdateCheckInterval == nil {
		return &AutoUpdateConfig{
			Enabled: false,
			URL:     "https://eu-west-1.intake.senhub.io/releases",
		}
	}

	// Convert update interval to enabled/disabled
	var enabled bool
	switch interval := rc.data.Agent.UpdateCheckInterval.(type) {
	case int:
		enabled = interval > 0
	case float64:
		enabled = interval > 0
	case string:
		enabled = interval != "0" && interval != ""
	default:
		enabled = false
	}

	return &AutoUpdateConfig{
		Enabled: enabled,
		URL:     rc.data.Agent.RegistryUrl,
	}
}

// generateConfigYAML generates YAML configuration with comments for the online mode replica
func (rc *RemoteConfiguration) generateConfigYAML(config *LocalConfigurationData) ([]byte, error) {
	yamlTemplate := `# Agent configuration (replicated from SenHub server)
# This file is automatically maintained by the agent in online mode
# It provides a local backup and enables easier mode transition
agent:
  key: "%s"
  mode: online
  generated: false

# Auto-update configuration (from server)
auto_update:
  enabled: %t      # Enable/disable automatic updates
  url: "%s"        # Update server URL

# Cache configuration (from server)
cache:
  retention_minutes: %d  # Cache retention time in minutes

# Storage configuration (from server)
storage:
%s

# Probes configuration (from server)
probes:
%s

# This file was automatically generated by SenHub Agent in online mode
# Last updated: %s
# Source: Remote configuration from %s
`

	// Generate storage YAML section
	storageYaml, err := rc.generateStorageYAML(config.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to generate storage YAML: %w", err)
	}

	// Generate probes YAML section
	probesYaml, err := rc.generateProbesYAML(config.Probes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate probes YAML: %w", err)
	}

	// Get cache retention value
	cacheRetention := 5
	if config.Cache != nil {
		cacheRetention = config.Cache.RetentionMinutes
	}

	// Generate timestamp and server info
	timestamp := time.Now().Format("2006-01-02 15:04:05 MST")
	serverUrl := "SenHub Server"
	if rc.args != nil && rc.args.ServerUrl != "" {
		serverUrl = rc.args.ServerUrl
	}

	return []byte(fmt.Sprintf(yamlTemplate,
		config.Agent.Key,
		config.AutoUpdate.Enabled,
		config.AutoUpdate.URL,
		cacheRetention,
		storageYaml,
		probesYaml,
		timestamp,
		serverUrl,
	)), nil
}

// generateStorageYAML generates the storage section of the YAML
func (rc *RemoteConfiguration) generateStorageYAML(storage []StorageConfig) (string, error) {
	var yamlLines []string

	for _, s := range storage {
		yamlLines = append(yamlLines, fmt.Sprintf("  - name: %s", s.Name))
		yamlLines = append(yamlLines, "    params:")

		// Convert params to YAML format
		paramsYaml, err := yaml.Marshal(s.Params)
		if err != nil {
			return "", fmt.Errorf("failed to marshal storage params: %w", err)
		}

		// Indent the params YAML
		lines := strings.Split(string(paramsYaml), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				yamlLines = append(yamlLines, fmt.Sprintf("      %s", line))
			}
		}
	}

	return strings.Join(yamlLines, "\n"), nil
}

// generateProbesYAML generates the probes section of the YAML
func (rc *RemoteConfiguration) generateProbesYAML(probes []ProbeConfig) (string, error) {
	var yamlLines []string

	for _, p := range probes {
		yamlLines = append(yamlLines, fmt.Sprintf("  - name: %s", p.Name))

		// Add type field (v2 format)
		if p.Type != "" {
			yamlLines = append(yamlLines, fmt.Sprintf("    type: %s", p.Type))
		}

		yamlLines = append(yamlLines, "    params:")

		// Convert params to YAML format
		paramsYaml, err := yaml.Marshal(p.Params)
		if err != nil {
			return "", fmt.Errorf("failed to marshal probe params: %w", err)
		}

		// Indent the params YAML
		lines := strings.Split(string(paramsYaml), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				yamlLines = append(yamlLines, fmt.Sprintf("      %s", line))
			}
		}
	}

	return strings.Join(yamlLines, "\n"), nil
}
