package data_store

import (
	"context"
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/senhub_server"
)

// Synchronize metrics to senhub backend.
type SyncStrategySenhub struct {
	agentConfig configuration.AgentConfiguration
	server      senhub_server.SenhubServer
}

func NewSyncStrategySenhub(agentConfig configuration.AgentConfiguration) SyncStrategy {
	return &SyncStrategySenhub{
		agentConfig: agentConfig,
	}
}

func (s SyncStrategySenhub) GetName() string {
	return "DataStoreStrategySenhub"
}

func (s *SyncStrategySenhub) Start(chan struct{}, configuration.PersistanceConfig) error {
	// Create new senhub server
	s.server = senhub_server.NewSenhubServer(
		s.agentConfig.GetAuthenticationKey(),
		s.agentConfig.GetServerUrl(),
	)

	return nil
}

func (s *SyncStrategySenhub) Shutdown(ctx context.Context) error {
	return nil
}

func (s *SyncStrategySenhub) Sync(data []DataPoint, configuration configuration.PersistanceConfig) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", response.StatusCode, response.Body)
	}

	return nil
}
