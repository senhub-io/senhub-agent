// senhub-agent/internal/agent/services/data_store/stategy_senhub.go
package data_store

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/server"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/validators"
)

var (
	DEFAULT_SENHUB_INTERVAL = 5 * time.Second
)

type SenhubDataPoint struct {
	Name      string     `json:"name"`
	Timestamp time.Time  `json:"timestamp"`
	Value     float32    `json:"value"`
	Tags      []tags.Tag `json:"tags,omitempty"`
}

type SyncStrategySenhubParams struct {
	Interval        time.Duration
	RetentionPeriod time.Duration
	ServerUrl       string
}

// Synchronize metrics to senhub backend.
type SyncStrategySenhub struct {
	buffer        Buffer
	agentConfig   configuration.AgentConfiguration
	storageConfig configuration.StorageConfigParams
	server        server.Server // Utilise la nouvelle interface
	logger        *logger.ModuleLogger
	config        SyncStrategySenhubParams
	scheduler     periodic_scheduler.PeriodicScheduler
}

func NewSyncStrategySenhub(
	agentConfig configuration.AgentConfiguration,
	storageConfig configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) SyncStrategy {
	// Create module-specific logger for SenHub strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.senhub")

	srv := server.NewServer(
		agentConfig.GetAuthenticationKey(),
		agentConfig.GetServerUrl(),
		baseLogger,
	)

	return &SyncStrategySenhub{
		buffer:        NewBuffer(),
		agentConfig:   agentConfig,
		storageConfig: storageConfig,
		logger:        moduleLogger,
		server:        srv,
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

func ParseSyncStrategySenhubParams(config configuration.StorageConfigParams) (SyncStrategySenhubParams, error) {
	errs := []error{}
	params := SyncStrategySenhubParams{
		Interval: DEFAULT_SENHUB_INTERVAL,
	}

	if intervalStr, ok := config["interval"]; ok {
		if !validators.IsDuration(intervalStr) {
			errs = append(errs, fmt.Errorf("interval must be a valid duration"))
		} else {
			parsedInterval, err := time.ParseDuration(intervalStr.(string))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing interval: %w", err))
			} else {
				params.Interval = parsedInterval
			}
		}
	}

	if len(errs) > 0 {
		return params, fmt.Errorf("error parsing config: %v", errs)
	}

	return params, nil
}
func (s *SyncStrategySenhub) ValidateConfigParams(params configuration.StorageConfigParams) error {
	config, err := ParseSyncStrategySenhubParams(params)
	if err != nil {
		return err
	}

	s.config = config
	return nil
}

func (s *SyncStrategySenhub) Start() error {
	if (s.scheduler) != nil {
		return nil
	}
	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          s.config.Interval,
		Execute:           s.doSync,
		ExecuteOnStart:    false,
		ExecuteOnShutdown: true,
	}, s.logger.Logger)
	s.scheduler = scheduler

	return s.scheduler.Start(nil)
}

func (s *SyncStrategySenhub) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down sync strategy")
	defer func() {
		s.scheduler = nil
	}()
	return s.scheduler.Shutdown(ctx)
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
