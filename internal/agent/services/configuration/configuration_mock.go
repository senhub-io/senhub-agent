package configuration

import (
	"encoding/json"
	"os"

	"github.com/rs/zerolog"
	clientService "senhub-agent.go/internal/agent/services/server"
)

func NewMockRemoteConfiguration(url string, config string) *RemoteConfiguration {
	logger := zerolog.New(os.Stderr)

	if config == "" {
		config = `{"storage_config":[],"probes":[], "agent": {}}`
	}
	var configJSON RemoteConfigurationData
	if err := json.Unmarshal([]byte(config), &configJSON); err != nil {
		logger.Error().Msgf("Failed to parse configuration: %v", err)
	}

	httpClient := clientService.NewServer("authKey", url, &logger)
	remoteConfig := NewRemoteConfiguration(
		httpClient,
		&logger,
	)
	remoteConfig.data = configJSON

	return remoteConfig
}
