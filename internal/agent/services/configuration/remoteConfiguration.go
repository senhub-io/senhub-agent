package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

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
	senhubServer  senhub_server.SenhubServer
	eventNotifier *EventNotifier
	ticker        *time.Ticker
	tickerOnce    sync.Once
	mutex         sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

// NewService initializes a new Service instance.
func NewRemoteConfiguration(senhubServer senhub_server.SenhubServer) *RemoteConfiguration {
	return &RemoteConfiguration{
		senhubServer:  senhubServer,
		data:          RemoteConfigurationData{},
		eventNotifier: NewEventNotifier(),
	}
}

func (s *RemoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (s *RemoteConfiguration) GetConfiguration() RemoteConfigurationData {
	return s.data
}

// Register a callback to be called when the configuration changes.
func (s *RemoteConfiguration) OnConfigChanged(callback func(string)) {
	s.eventNotifier.RegisterObserver(callback)
}

// StartPeriodicTask starts calling doRefreshConfig at the specified interval.
func (s *RemoteConfiguration) Start(quitChannel chan struct{}) error {
	s.tickerOnce.Do(func() { // Ensure the ticker only starts once
		s.ticker = time.NewTicker(3 * time.Second)

		go func() {
			for {
				select {
				case <-quitChannel:
					s.ticker.Stop()
					return
				case <-s.ticker.C:
					s.doRefreshConfig()
				}
			}
		}()
	})

	// Refresh configuration immediately
	// This is blocking
	log.Println("Fetching initial configuration")
	if err := s.doRefreshConfig(); err != nil {
		log.Fatalf("Unable to fetch initial configuration %v", err)
		return err
	}

	return nil
}

// StopPeriodicTask stops the periodic execution of doRefreshConfig.
func (rc *RemoteConfiguration) Shutdown(context.Context) error {
	rc.ticker.Stop()
	return nil
}

// doRefreshConfig is the method called periodically, now thread-safe.
func (s *RemoteConfiguration) doRefreshConfig() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	config, err := s.doFetchConfiguration()
	if err != nil {
		return err
	}

	// Replace existing configuration with new one
	s.data = *config
	s.eventNotifier.NotifyObservers("Configuration changed")

	return nil
}

func (s *RemoteConfiguration) doFetchConfiguration() (*RemoteConfigurationData, error) {
	res, err := s.senhubServer.Get("/configs")
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
