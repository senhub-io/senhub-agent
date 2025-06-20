package configuration

import (
	"encoding/json"
	"os"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
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
		nil, // No args for mock
	)
	remoteConfig.data = configJSON

	return remoteConfig
}

// SetReplicationParams sets the replication parameters for testing
func (rc *RemoteConfiguration) SetReplicationParams(args *cliArgs.ParsedArgs) {
	rc.args = args
	rc.agentKey = args.AuthenticationKey
	if args.ConfigPath != "" {
		rc.localReplicaPath = args.ConfigPath
	} else {
		rc.localReplicaPath = "./agent-config.yaml"
	}

	// Create a proper logger from args
	baseLogger := logger.NewLogger(args)
	rc.logger = logger.NewModuleLogger(baseLogger, "configuration.remote")
}

// TestReplication tests the replication functionality directly
func (rc *RemoteConfiguration) TestReplication() error {
	return rc.replicateConfigurationLocally()
}
