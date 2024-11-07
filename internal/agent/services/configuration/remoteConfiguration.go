package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"senhub-agent.go/internal/agent/services/senhub_server"
)

// RemoteConfiguration is an interface for remote configuration.
// Remote configuration is read periodically from the server.

type RemoteConfiguration interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

type RemoteConfigurationData struct {
	Url               string            `json:"url"`
}
type remoteConfiguration struct {
	senhubServer  senhub_server.SenhubServer
	configuration *RemoteConfigurationData
}

func NewRemoteConfiguration(senhubServer senhub_server.SenhubServer) RemoteConfiguration {
	return &remoteConfiguration{
		senhubServer:  senhubServer,
		configuration: nil,
	}
}

func (c remoteConfiguration) GetName() string {
	return "RemoteConfiguration"
}

func (c remoteConfiguration) Start(quitChannel chan struct{}) error {
	// Refresh configuration immediately
	// This is blocking
	log.Println("Fetching initial configuration")
	if err := c.doRefreshConfig(); err != nil {
		log.Fatalf("Unable to fetch initial configuration %v", err)
		os.Exit(1)
	}

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := c.doRefreshConfig()
				if err != nil {
					log.Printf("error fetching configuration: %v", err)
				}

			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (c *remoteConfiguration) doRefreshConfig() error {
	config, err := c.doFetchConfiguration()
	if err != nil {
		return err
	}
	// Replace existing configuration with new one
	c.configuration = config

	return nil
}

func (c remoteConfiguration) doFetchConfiguration() (*RemoteConfigurationData, error) {
	res, err := c.senhubServer.Get("/configs")
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
func (c remoteConfiguration) Shutdown(ctx context.Context) error {
	// Nothing to do for now.
	return nil
}
