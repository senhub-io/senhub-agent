package data_store

import (
	"context"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// Data store is responsible for storing and synchronizing data to the server.

type DataPoint struct {
	Name      string     `json:"name"`
	Timestamp time.Time  `json:"timestamp"`
	Value     float32    `json:"value"`
	Tags      []tags.Tag `json:"tags,omitempty"`
}

type AddCallback func([]DataPoint) error

// SyncStrategy is an interface for synchronization strategies.
// Implement these methods to create a new synchronization strategy.
//
// A synchronization strategy is responsible for synchronizing data to a backend.
type SyncStrategy interface {
	GetStrategyName() string
	ValidateConfigParams(configuration.StorageConfigParams) error
	Start() error
	AddDataPoints([]DataPoint) error
	Shutdown(context.Context) error
}

// DataStore is an interface for data store.
type DataStore interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error

	GetCallback() AddCallback
}

type dataStore struct {
	strategy     SyncStrategy
	logger       *logger.Logger
	remoteConfig *configuration.RemoteConfiguration
	agentConfig  configuration.AgentConfiguration
	ticker       *time.Ticker
	tickerOnce   sync.Once
}

// NewDataStore creates a new data store.
func NewDataStore(
	agentConfig configuration.AgentConfiguration,
	remoteConfig *configuration.RemoteConfiguration,
	logger *logger.Logger,
) DataStore {
	localLogger := logger.With().Str("service", "DataStore").Logger()

	return &dataStore{
		logger:       &localLogger,
		remoteConfig: remoteConfig,
		agentConfig:  agentConfig,
	}
}

func (d *dataStore) GetName() string {
	return "DataStore"
}

func (d *dataStore) GetCallback() AddCallback {
	return func(data []DataPoint) error {
		if d.strategy == nil {
			return nil
		}
		return d.strategy.AddDataPoints(data)
	}
}

func (d *dataStore) OnConfigRefreshed(string) {
	strategyConfig := d.remoteConfig.GetConfiguration().StorageConfig
	strategyName := strategyConfig.Stategy
	if strategyName == "" {
		// Default strategy is senhub
		strategyName = "senhub"
	}

	if d.strategy != nil && d.strategy.GetStrategyName() == strategyName {
		return
	}
	if d.strategy != nil {
		d.logger.Info().
			Str("strategy_name", d.strategy.GetStrategyName()).
			Msg("shutting down strategy")
		d.strategy.Shutdown(context.Background())
	}

	logger := d.logger.With().
		Any("strategy_config", strategyConfig).
		Str("strategy_name", strategyName).
		Logger()
	switch strategyName {
	case "senhub":
		logger.Info().Msg("Initializing strategy")

		d.strategy = NewSyncStrategySenhub(d.agentConfig, strategyConfig.Params, &logger)
		if err := d.strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
			logger.Error().Err(err).Msg("invalid configuration")
			return
		}
		d.strategy.Start()
		return

	case "prtg":
		logger.Info().Msg("Initializing strategy")
		d.strategy = NewSyncStrategyPrtg(d.agentConfig, strategyConfig.Params, &logger)
		if err := d.strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
			logger.Error().Err(err).Msg("invalid configuration")
			return
		}
		d.strategy.Start()
		return

	default:
		d.logger.Error().
			Str("strategy_name", strategyName).
			Msg("unknown strategy")
		return
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	d.OnConfigRefreshed("initial")
	d.remoteConfig.OnConfigChanged(d.OnConfigRefreshed)
	return nil
}

func (d *dataStore) Shutdown(ctx context.Context) error {
	return d.strategy.Shutdown(ctx)
}
