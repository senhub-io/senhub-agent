package data_store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/senhub_server"
	"senhub-agent.go/internal/agent/tags"
)

type SenhubDataPoint struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Value     float32   `json:"value"`
	// NOTE tags will be converted to a list of strings
	Tags map[string]string `json:"tags,omitempty"`
}

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
		buffer:        NewBuffer(),
		agentConfig:   agentConfig,
		storageConfig: storageConfig,
		logger:        &localLogger,
		server:        server,
	}
}

func (s *SyncStrategySenhub) GetStrategyName() string {
	return "senhub"
}

func (s *SyncStrategySenhub) GetStrategyParams() map[string]interface{} {
	return s.storageConfig
}

func (s *SyncStrategySenhub) AddDataPoints(data []DataPoint) error {
	s.buffer.Append(data)
	return nil
}

func (s *SyncStrategySenhub) ValidateConfigParams(params configuration.StorageConfigParams) error {
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

	// Remove private tags
	transformedData := make([]SenhubDataPoint, 0, len(data))
	for _, dp := range data {

		transformedData = append(transformedData, SenhubDataPoint{
			Name:      dp.Name,
			Timestamp: dp.Timestamp,
			Value:     dp.Value,
			Tags: tags.FormatTagsForServer(
				tags.OnlyPublicTags(dp.Tags),
			),
		})
	}

	s.logger.Debug().Any("data", transformedData).Msg("synchronizing data")
	if err := s.doSyncData(transformedData); err != nil {
		s.logger.Error().Err(err).Msg("error synchronizing data")
		s.buffer.AbortSync(data)
		return err
	}

	return nil
}

func (s *SyncStrategySenhub) doSyncData(data []SenhubDataPoint) error {
	response, err := s.server.Post("/metrics", data)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("unexpected status code: %d\n%v", response.StatusCode, response.Body)
	}

	return nil
}
