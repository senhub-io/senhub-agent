package data_store

import (
	"context"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// Data store is responsible for storing and synchronizing data to the server.

type DataPoint struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Value     float32   `json:"value"`
}

type AddCallback func([]DataPoint) error

// SyncStrategy is an interface for synchronization strategies.
// Implement these methods to create a new synchronization strategy.
//
// A synchronization strategy is responsible for synchronizing data to a backend.
type SyncStrategy interface {
	GetStrategyName() string
	Start(chan struct{}, configuration.StorageConfig) error
	Sync([]DataPoint, configuration.StorageConfig) error
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
	buffer       Buffer
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
		buffer:       NewBuffer(),
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
		return d.buffer.Append(data)
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	d.tickerOnce.Do(func() { // Ensure the ticker only starts once
		ticker := time.NewTicker(5 * time.Second)

		go func() {
			for {
				select {
				case <-ticker.C:
					err := d.doSyncData()
					if err != nil {
						d.logger.Error().Err(err).Msg("error synchronizing data")
					}

				case <-quitChannel:
					ticker.Stop()
					return
				}
			}
		}()
	})

	return nil
}

// Ensure the strategy is available according to the configuration.
func (d *dataStore) getOrRefreshStrategy() {
	strategyName := d.remoteConfig.GetConfiguration().StorageConfig.Stategy
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

	switch strategyName {
	case "senhub":
		d.logger.Info().
			Str("strategy_name", strategyName).
			Msg("Initializing strategy")

		d.strategy = NewSyncStrategySenhub(d.agentConfig, d.logger)
		d.strategy.Start(nil, d.remoteConfig.GetConfiguration().StorageConfig)
		return

	default:
		d.logger.Error().
			Str("strategy_name", strategyName).
			Msg("unknown strategy")
		return
	}
}

func (d *dataStore) doSyncData() error {
	d.getOrRefreshStrategy()

	data := d.buffer.Sync()
	remoteConfig := d.remoteConfig.GetConfiguration().StorageConfig

	d.logger.Info().Any("data", data).Msg("synchronizing data")
	if err := d.strategy.Sync(data, remoteConfig); err != nil {
		d.logger.Error().Err(err).Msg("error synchronizing data")
		d.buffer.AbortSync(data)
		return err
	}

	return nil
}

func (d *dataStore) Shutdown(ctx context.Context) error {
	if d.ticker != nil {
		d.ticker.Stop()
	}

	return d.doSyncData()
}
