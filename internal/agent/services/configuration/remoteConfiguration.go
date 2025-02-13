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
	"reflect"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
)

type StorageConfigParams = map[string]interface{}

type StorageConfig struct {
	Name   string              `json:"name"`
	Params StorageConfigParams `json:"params"`
}

type ProbeConfigParams = map[string]interface{}

type ProbeConfig struct {
	Name   string            `json:"name"`
	Params ProbeConfigParams `json:"params"`
}

type RemoteConfigurationData struct {
	StorageConfig []StorageConfig `json:"storage"`
	Probes        []ProbeConfig   `json:"probes"`
}

type RemoteConfiguration struct {
	data          RemoteConfigurationData
	logger        *logger.Logger
	server        server.Server
	eventNotifier *EventNotifier
	mutex         sync.Mutex
	scheduler     periodic_scheduler.PeriodicScheduler
}

func NewRemoteConfiguration(
	serverClient server.Server,
	logger *logger.Logger,
) *RemoteConfiguration {
	localLogger := logger.With().Str("service", "RemoteConfiguration").Logger()
	localLogger.Debug().Msg("Creating new RemoteConfiguration instance")

	rc := &RemoteConfiguration{
		logger:        &localLogger,
		server:        serverClient,
		data:          RemoteConfigurationData{},
		eventNotifier: NewEventNotifier(&localLogger),
	}

	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          10 * time.Second,
		MaxRetries:        3,
		ExecuteOnStart:    true,
		ExecuteOnShutdown: false,
		Execute:           rc.doRefreshConfig,
	}, &localLogger)
	rc.scheduler = scheduler

	localLogger.Debug().Msg("RemoteConfiguration instance created successfully")
	return rc
}

func (rc *RemoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (rc *RemoteConfiguration) GetConfiguration() RemoteConfigurationData {
	return rc.data
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

	probeNames := make(map[string]bool)
	for _, probe := range config.Probes {
		if probe.Name == "" {
			return fmt.Errorf("probe name cannot be empty")
		}
		if probeNames[probe.Name] {
			return fmt.Errorf("duplicate probe name: %s", probe.Name)
		}
		probeNames[probe.Name] = true
	}

	return nil
}

func (rc *RemoteConfiguration) doRefreshConfig() error {
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
				rc.eventNotifier.NotifyObservers("Configuration changed")
			} else {
				rc.logger.Debug().Msg("Configuration unchanged")
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
