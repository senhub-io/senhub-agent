package data_store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/senhub_server"
)

// Synchronize metrics to senhub backend.
type SyncStrategySenhub struct {
	/** Store all datapoints */
	buffer Buffer

	agentConfig   configuration.AgentConfiguration
	storageConfig configuration.StorageConfigParams
	server        senhub_server.SenhubServer
	logger        *logger.Logger
	ticker        *time.Ticker
	tickerOnce    sync.Once
}

func NewSyncStrategySenhub(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	logger *logger.Logger,
) SyncStrategy {
	localLogger := logger.With().Str("sync_strategy", "SyncStrategySenhub").Logger()
	server := senhub_server.NewSenhubServer(
		agentConfig.GetAuthenticationKey(),
		agentConfig.GetServerUrl(),
		logger,
	)

	return &SyncStrategySenhub{
		buffer:      NewBuffer(),
		agentConfig: agentConfig,
		logger:      &localLogger,
		server:      server,
	}
}

func (s *SyncStrategySenhub) GetStrategyName() string {
	return "senhub"
}

func (s *SyncStrategySenhub) AddDataPoints(data []DataPoint) error {
	s.buffer.Append(data)
	return nil
}

func (s *SyncStrategySenhub) Start() error {
	s.tickerOnce.Do(func() { // Ensure the ticker only starts once
		s.logger.Info().Msg("Starting sync strategy")
		ticker := time.NewTicker(5 * time.Second)

		go func() {
			for {
				select {
				case <-ticker.C:
					err := s.doSync()
					if err != nil {
						s.logger.Error().Err(err).Msg("error synchronizing data")
					}
				}
			}
		}()
	})

	return nil
}

func (s *SyncStrategySenhub) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sync strategy")
	if s.ticker != nil {
		s.ticker.Stop()
	}

	return s.doSync()
}

func (s *SyncStrategySenhub) doSync() error {
	data := s.buffer.Sync()

	if len(data) == 0 {
		return nil
	}

	s.logger.Debug().Any("data", data).Msg("synchronizing data")
	if err := s.doSyncData(data); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
		s.buffer.AbortSync(data)
		return err
	}

	return nil
}

func (s *SyncStrategySenhub) doSyncData(data []DataPoint) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", response.StatusCode, response.Body)
	}

	return nil
}
