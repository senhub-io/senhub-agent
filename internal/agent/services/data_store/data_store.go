package data_store

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
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
	GetStrategyParams() map[string]interface{}
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
	strategies   []SyncStrategy
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
		for _, strategy := range d.strategies {
			if err := strategy.AddDataPoints(data); err != nil {
				d.logger.Error().
					Err(err).
					Str("strategy_name", strategy.GetStrategyName()).
					Msg("error adding data points")
			}
		}

		return nil
	}
}

func (d *dataStore) GenerateStrategyId(strategyName string, params configuration.StorageConfigParams) string {
	input := fmt.Sprintf("%s-%v", strategyName, params)
	hash := md5.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

func (d *dataStore) OnConfigRefreshed(string) {
	validStrategyIds := []string{}
	storageConfigs := d.remoteConfig.GetConfiguration().StorageConfig

	for _, storageConfig := range storageConfigs {
		strategy := d.retrieveOrCreate(storageConfig)
		if strategy != nil {
			validStrategyIds = append(validStrategyIds, d.GenerateStrategyId(storageConfig.Name, storageConfig.Params))
		}
	}

	// Stop strategies that are no longer in the Configuration
	for _, strategy := range d.strategies {
		found := false
		for _, validStrategyId := range validStrategyIds {
			strategyId := d.GenerateStrategyId(strategy.GetStrategyName(), strategy.GetStrategyParams())
			if strategyId == validStrategyId {
				found = true
				break
			}
		}

		if !found {
			d.logger.Info().
				Str("strategy_name", strategy.GetStrategyName()).
				Msg("stopping strategy")
			strategy.Shutdown(context.Background())
		}
	}
}

func (d *dataStore) retrieveOrCreate(strategyConfig configuration.StorageConfig) SyncStrategy {
	searchStrategyId := d.GenerateStrategyId(strategyConfig.Name, strategyConfig.Params)

	// Retrieve the strategy if it already exists
	for _, strategy := range d.strategies {
		strategyId := d.GenerateStrategyId(strategy.GetStrategyName(), strategy.GetStrategyParams())
		if strategyId == searchStrategyId {
			return strategy
		}
	}

	// Create a new strategy as it does not exist
	logger := d.logger.With().
		Any("strategy_params", strategyConfig.Params).
		Str("strategy_name", strategyConfig.Name).
		Logger()
	strategy := d.createStrategyForConfig(strategyConfig, logger)
	if strategy == nil {
		return nil
	} else if err := strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
		logger.Error().Err(err).Msg("invalid configuration")
		return nil
	} else {
		strategy.Start()
		d.strategies = append(d.strategies, strategy)
		return strategy
	}
}

func (d *dataStore) createStrategyForConfig(strategyConfig configuration.StorageConfig, logger logger.Logger) SyncStrategy {
	switch strategyConfig.Name {
	case "senhub":
		logger.Info().Msg("Initializing strategy")
		strategy := NewSyncStrategySenhub(d.agentConfig, strategyConfig.Params, &logger)
		return strategy

	case "prtg":
		logger.Info().Msg("Initializing strategy")
		strategy := NewSyncStrategyPrtg(d.agentConfig, strategyConfig.Params, &logger)
		return strategy

	default:
		logger.Error().Msg("unknown strategy")
		return nil
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	d.OnConfigRefreshed("initial")
	d.remoteConfig.OnConfigChanged(d.OnConfigRefreshed)
	return nil
}

func (d *dataStore) Shutdown(ctx context.Context) error {
	errs := []error{}
	for _, strategy := range d.strategies {
		err := strategy.Shutdown(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors shutting down strategies: %v", errs)
	}
	return nil
}
