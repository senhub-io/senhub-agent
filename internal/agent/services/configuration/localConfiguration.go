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
	"path/filepath"
	"sync"
	"sync/atomic"

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
	// GlobalTags are applied to every datapoint of every probe (multi-site /
	// multi-tenant labelling). A probe's own custom_tags override a global_tag
	// with the same key. Keep small (< ~10 keys) to bound backend cardinality.
	GlobalTags map[string]string `yaml:"global_tags,omitempty"`
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
	// Version is the target the active updater tracks: "latest" (the default
	// when omitted) resolves the newest stable from the registry; an explicit
	// version pins to it. Empty here used to leave the active updater comparing
	// against an empty string and concluding "no update required" forever (#567).
	Version string `yaml:"version"`
}

// CacheConfig represents cache configuration
type CacheConfig struct {
	RetentionMinutes int `yaml:"retention_minutes"`
}

// LocalConfiguration manages offline configuration
type LocalConfiguration struct {
	// dataPo holds the current configuration as an immutable snapshot
	// swapped atomically: the watcher and rewatch goroutines write it
	// while GetConfiguration (every datapoint batch) plus the sensor,
	// auto_update and HTTP readers read it lock-free — the previous
	// bare field was the root cause of the #140 race flakes (audit
	// C3, #268). Writers always build a FRESH LocalConfigurationData
	// and Store it; nobody mutates a stored snapshot.
	dataPo        atomic.Pointer[LocalConfigurationData]
	logger        *logger.ModuleLogger
	configPath    string
	args          *cliArgs.ParsedArgs
	eventNotifier *EventNotifier
	watcher       *fsnotify.Watcher
	quitChannel   chan struct{}
	// stopCh + watcherWG make the watcher goroutines joinable:
	// Shutdown closes stopCh and waits, so no goroutine outlives the
	// instance (tests saw rewatch goroutines outlive t.TempDir()).
	// quitChannel stays external (owned by the caller, may be nil).
	stopCh    chan struct{}
	stopOnce  sync.Once
	watcherWG sync.WaitGroup
}

// snapshot returns the current immutable configuration snapshot
// (never nil after construction). Callers must not mutate it.
func (lc *LocalConfiguration) snapshot() *LocalConfigurationData {
	return lc.dataPo.Load()
}

// storeData publishes d as the new current snapshot.
func (lc *LocalConfiguration) storeData(d LocalConfigurationData) {
	lc.dataPo.Store(&d)
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
		eventNotifier: NewEventNotifier(moduleLogger.Logger),
	}
	lc.storeData(LocalConfigurationData{})

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
	return lc.snapshot().Agent.Key
}

// GetAuthenticationKey implements AgentConfiguration interface
func (lc *LocalConfiguration) GetAuthenticationKey() string {
	return lc.snapshot().Agent.Key
}

// GetGlobalTags implements AgentConfiguration interface — the
// agent-level global_tags emitted as OTLP Resource attributes (#202).
func (lc *LocalConfiguration) GetGlobalTags() map[string]string {
	return lc.snapshot().Agent.GlobalTags
}

// GetServerUrl implements AgentConfiguration interface
func (lc *LocalConfiguration) GetServerUrl() string {
	// In offline mode, we don't have a server URL
	return ""
}

// GetAutoUpdateConfig returns the auto-update configuration
func (lc *LocalConfiguration) GetAutoUpdateConfig() *AutoUpdateConfig {
	if lc.snapshot().AutoUpdate == nil {
		// Return default configuration
		return &AutoUpdateConfig{
			Enabled: false,
			URL:     "https://eu-west-1.intake.senhub.io/releases",
		}
	}
	return lc.snapshot().AutoUpdate
}

// GetCacheConfig returns the cache configuration
func (lc *LocalConfiguration) GetCacheConfig() *CacheConfig {
	if lc.snapshot().Cache == nil {
		lc.logger.Warn().Msg("Cache configuration is nil in YAML, using default (5 minutes)")
		// Return default configuration
		return &CacheConfig{
			RetentionMinutes: 5,
		}
	}
	lc.logger.Info().
		Int("retention_minutes", lc.snapshot().Cache.RetentionMinutes).
		Msg("Cache configuration loaded from YAML")
	return lc.snapshot().Cache
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

	// The active updater compares against this target; an empty string makes it
	// conclude "no update required" forever (#567), so an enabled agent with no
	// explicit pin tracks the newest stable ("latest").
	updateVersion := autoUpdate.Version
	if updateVersion == "" {
		updateVersion = "latest"
	}

	// Convert local config format to remote config format
	return RemoteConfigurationData{
		StorageConfig: lc.snapshot().Storage,
		Probes:        lc.snapshot().Probes,
		Agent: AgentConfig{
			RegistryUrl:         autoUpdate.URL,
			Version:             updateVersion,
			UpdateCheckInterval: updateInterval,
			License:             lc.snapshot().Agent.License,
			AuthenticationKey:   lc.snapshot().Agent.Key,
			GlobalTags:          lc.snapshot().Agent.GlobalTags,
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

	// Initialize file watcher. We watch:
	//   - the top-level config file (configPath / agent.yaml) — for
	//     monolithic configs this is the whole story; for multi-file
	//     it carries the global blocks.
	//   - the probes.d/ and strategies.d/ sibling directories when
	//     they exist. fsnotify on a directory emits events for every
	//     entry inside, so add/remove/edit of a fragment file
	//     triggers a reload without an agent restart. Pre-0.2.x
	//     these directories were silently unwatched — operators had
	//     to restart to pick up new fragments.
	var err error
	lc.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	if err := lc.watcher.Add(lc.configPath); err != nil {
		_ = lc.watcher.Close()
		return fmt.Errorf("failed to watch config file %s: %w", lc.configPath, err)
	}
	lc.logger.Info().Str("config_path", lc.configPath).Msg("Started watching configuration file")

	baseDir := filepath.Dir(lc.configPath)
	for _, sub := range []string{"probes.d", "strategies.d"} {
		dir := filepath.Join(baseDir, sub)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			// Directory absent (typical for a legacy monolithic
			// install) — skip silently. A later `agent config
			// migrate` will create it and we'll be watching once
			// the operator restarts the agent. The trade-off here
			// is deliberate: hot-detecting a new directory means
			// watching the PARENT for create events, which then
			// triggers reloads on every unrelated file in
			// /etc/senhub-agent/ — too noisy.
			lc.logger.Debug().Str("dir", dir).Msg("fragment directory not watched (absent)")
			continue
		}
		if err := lc.watcher.Add(dir); err != nil {
			_ = lc.watcher.Close()
			return fmt.Errorf("failed to watch fragment directory %s: %w", dir, err)
		}
		lc.logger.Info().Str("dir", dir).Msg("Started watching fragment directory")
	}

	// Start watching goroutine (joinable: Shutdown closes stopCh and
	// waits on watcherWG).
	lc.stopCh = make(chan struct{})
	lc.watcherWG.Add(1)
	go lc.watchConfigFile()

	return nil
}

// Shutdown performs cleanup and stops file watching
func (lc *LocalConfiguration) Shutdown(ctx context.Context) error {
	lc.logger.Info().Msg("Shutting down LocalConfiguration")

	if lc.stopCh != nil {
		lc.stopOnce.Do(func() { close(lc.stopCh) })
	}
	if lc.watcher != nil {
		if err := lc.watcher.Close(); err != nil {
			lc.logger.Warn().Err(err).Msg("Error closing file watcher")
		}
	}
	// Join the watcher + rewatch goroutines: nothing may outlive the
	// instance (audit C3 — unjoinable watcher).
	lc.watcherWG.Wait()

	return nil
}
