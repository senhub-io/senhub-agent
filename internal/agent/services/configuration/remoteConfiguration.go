package configuration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

type remoteConfiguration struct {
	senhubServer  senhub_server.SenhubServer
	configuration *RemoteConfiguration
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
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				config, err := c.doFetchConfiguration()
				if err != nil {
					log.Printf("error fetching configuration: %v", err)
				}
				// Replace existing configuration with new one
				c.configuration = &config

			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (c remoteConfiguration) doFetchConfiguration() (RemoteConfiguration, error) {
	res, err := c.senhubServer.Get("/config")
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var config RemoteConfiguration
	err = json.Unmarshal(respBody, &config)
	return config, err
}
func (c remoteConfiguration) Shutdown(ctx context.Context) error {
	// Nothing to do for now.
	return nil
}
