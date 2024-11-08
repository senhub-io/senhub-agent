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

type RemoteConfigurationData struct {
	Url           string        `json:"url"`
	StorageConfig StorageConfig `json:"storage"`
}

// RemoteConfiguration represents a struct that performs periodic tasks.
type RemoteConfiguration struct {
	data         RemoteConfigurationData
	senhubServer senhub_server.SenhubServer
	ticker       *time.Ticker
	tickerOnce   sync.Once
	mutex        sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

// NewService initializes a new Service instance.
func NewRemoteConfiguration(senhubServer senhub_server.SenhubServer) *RemoteConfiguration {
	return &RemoteConfiguration{
		senhubServer: senhubServer,
		data:         RemoteConfigurationData{},
	}
}

func (s *RemoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (s *RemoteConfiguration) GetConfiguration() RemoteConfigurationData {
	return s.data
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
func (s *RemoteConfiguration) Shutdown(context.Context) error {
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
