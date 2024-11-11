package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/senhub_server"
)

// RemoteConfiguration is an interface for remote configuration.
// Remote configuration is read periodically from the server.

// StorageConfig represents the configuration for synchronization strategy.
type StorageConfig struct {
	Stategy   string `json:"strategy"`
	ServerUrl string `json:"server_url"`
}

type ProbeConfig struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

type RemoteConfigurationData struct {
	StorageConfig StorageConfig `json:"storage"`
	Probes        []ProbeConfig `json:"probes"`
}

// RemoteConfiguration represents a struct that performs periodic tasks.
type RemoteConfiguration struct {
	data          RemoteConfigurationData
	logger        *logger.Logger
	senhubServer  senhub_server.SenhubServer
	eventNotifier *EventNotifier
	ticker        *time.Ticker
	tickerOnce    sync.Once
	mutex         sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

// NewService initializes a new Service instance.
func NewRemoteConfiguration(
	senhubServer senhub_server.SenhubServer,
	logger *logger.Logger,
) *RemoteConfiguration {
	localLogger := logger.With().Str("service", "RemoteConfiguration").Logger()

	return &RemoteConfiguration{
		logger:        &localLogger,
		senhubServer:  senhubServer,
		data:          RemoteConfigurationData{},
		eventNotifier: NewEventNotifier(),
	}
}

func (rc *RemoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (rc *RemoteConfiguration) GetConfiguration() RemoteConfigurationData {
	return rc.data
}

// Register a callback to be called when the configuration changes.
func (rc *RemoteConfiguration) OnConfigChanged(callback func(string)) {
	rc.eventNotifier.RegisterObserver(callback)
}

// StartPeriodicTask starts calling doRefreshConfig at the specified interval.
func (rc *RemoteConfiguration) Start(quitChannel chan struct{}) error {
	rc.tickerOnce.Do(func() { // Ensure the ticker only starts once
		rc.ticker = time.NewTicker(3 * time.Second)

		// Attempt to first fetch configuration
		rc.logger.Info().Msg("Fetching initial configuration")
		if err := rc.doRefreshConfig(); err != nil {
			rc.logger.Error().Err(err).Msg("Failed to fetch initial configuration")
		}

		go func() {
			for {
				select {
				case <-quitChannel:
					rc.ticker.Stop()
					return
				case <-rc.ticker.C:
					rc.logger.Info().Msg("Fetching configuration")
					if err := rc.doRefreshConfig(); err != nil {
						rc.logger.Error().Err(err).Msg("Failed to fetch configuration")
					}
				}
			}
		}()
	})

	return nil
}

// StopPeriodicTask stops the periodic execution of doRefreshConfig.
func (rc *RemoteConfiguration) Shutdown(context.Context) error {
	rc.logger.Info().Msg("Shutting down")
	if rc.ticker != nil {
		rc.ticker.Stop()
	}
	return nil
}

// doRefreshConfig is the method called periodically, now thread-safe.
func (rc *RemoteConfiguration) doRefreshConfig() error {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()

	config, err := rc.doFetchConfiguration()
	if err != nil {
		return err
	}

	// Replace existing configuration with new one
	rc.data = *config
	rc.eventNotifier.NotifyObservers("Configuration changed")

	return nil
}

func (rc *RemoteConfiguration) doFetchConfiguration() (*RemoteConfigurationData, error) {
	res, err := rc.senhubServer.Get("/configs")
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d, %v", res.StatusCode, string(respBody))
	}

	var config RemoteConfigurationData
	err = json.Unmarshal(respBody, &config)
	return &config, err
}
