
//senhub-agent/internal/agent/services/data_store/data_store.go

// Package data_store implements configurable data routing system
// Responsibilities:
// - Routes datapoints to appropriate strategies
// - Manages strategy lifecycle (creation/destruction)
// - Handles hot configuration updates
package data_store

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// Data store is responsible for storing and synchronizing data to the server.

type AddCallback func([]datapoint.DataPoint, StrategyRouter) error

// SyncStrategy defines the interface for implementing different data synchronization backends.
// Each strategy must handle its own data buffering and synchronization logic.
type SyncStrategy interface {
	// GetStrategyName returns the unique identifier of the strategy
	GetStrategyName() string

	// GetStrategyParams returns the current configuration parameters
	GetStrategyParams() map[string]interface{}

	// ValidateConfigParams verifies if provided configuration is valid
	ValidateConfigParams(configuration.StorageConfigParams) error

	// Start initiates the strategy's background processes
	Start() error

	// AddDataPoints queues data points for synchronization
	AddDataPoints([]datapoint.DataPoint) error

	// Shutdown gracefully stops the strategy
	Shutdown(context.Context) error
}

// DataStore coordinates data collection and routing between probes and sync strategies
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
}

func NewDataStore(
	agentConfig configuration.AgentConfiguration,
	remoteConfig *configuration.RemoteConfiguration,
	logger *logger.Logger,
) DataStore {
	localLogger := logger.With().Str("service", "DataStore").Logger()
	localLogger.Debug().Msg("Creating new DataStore instance")

	ds := &dataStore{
		logger:       &localLogger,
		remoteConfig: remoteConfig,
		agentConfig:  agentConfig,
		strategies:   make([]SyncStrategy, 0),
	}
	localLogger.Debug().Msg("DataStore instance created successfully")
	return ds
}

func (d *dataStore) GetName() string {
	return "DataStore"
}

// Helper functions for logging

// getTagValue retrieves a tag value by key
func getTagValue(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateString truncates a string to the given maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (d *dataStore) GetCallback() AddCallback {
	d.logger.Debug().Msg("GetCallback called")
	return func(data []datapoint.DataPoint, probe StrategyRouter) error {
		localLogger := d.logger.With().Str("function", "addDataPoint").Logger()
		localLogger.Debug().Int("datapoints_count", len(data)).Msg("Callback called")

		if len(d.strategies) == 0 {
			localLogger.Warn().Msg("No strategies configured in datastore")
			return nil
		}

		for _, strategy := range d.strategies {
			targetStrategies := probe.GetTargetStrategies()
			localLogger.Debug().
				Strs("target_strategies", targetStrategies).
				Msg("Target strategies")

			shouldSendToStrategy := false
			for _, target := range targetStrategies {
				if target == strategy.GetStrategyName() {
					shouldSendToStrategy = true
					break
				}
			}

			if !shouldSendToStrategy {
				// Skip strategies that are not in the target list
				localLogger.Debug().
					Str("strategy", strategy.GetStrategyName()).
					Msg("Skipping strategy")
				continue
			}

			localLogger.Debug().
				Str("strategy", strategy.GetStrategyName()).
				Int("datapoints_count", len(data)).
				Msg("Sending data to strategy")

			// Log the first few events for debugging
			if strategy.GetStrategyName() == "event" && len(data) > 0 {
				for i := 0; i < min(3, len(data)); i++ {
					localLogger.Debug().
						Int("event_index", i).
						Str("event_source", getTagValue(data[i].Tags, "event_source")).
						Str("event_id", getTagValue(data[i].Tags, "event_id")).
						Str("message", truncateString(getTagValue(data[i].Tags, "message"), 100)).
						Msg("🔎 EVENT DETAIL - About to send to strategy")
				}
			}

			if err := strategy.AddDataPoints(data); err != nil {
				localLogger.Error().
					Err(err).
					Str("strategy", strategy.GetStrategyName()).
					Msg("Error adding data points to strategy")
			} else {
				localLogger.Info().
					Str("strategy", strategy.GetStrategyName()).
					Int("count", len(data)).
					Msg("✅ Successfully sent datapoints to strategy")
			}
		}
		return nil
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	d.logger.Debug().Msg("Starting DataStore service")
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

func (d *dataStore) GenerateStrategyId(strategyName string, params configuration.StorageConfigParams) string {
	paramsBytes, err := json.Marshal(params)
	if err != nil || len(params) == 0 {
		paramsBytes = []byte("{}")
	}

	input := fmt.Sprintf("%s-%s", strategyName, string(paramsBytes))
	hash := md5.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// OnConfigRefreshed updates strategy configurations based on remote changes.
// It ensures smooth transition between configurations by:
//   - Preserving existing strategy instances when possible
//   - Validating new configurations before applying
//   - Cleaning up unused strategies
func (d *dataStore) OnConfigRefreshed(reason string) {
	d.logger.Debug().Str("reason", reason).Msg("OnConfigRefreshed called")

	newStrategies := make(map[string]SyncStrategy)

	for _, storageConfig := range d.remoteConfig.GetConfiguration().StorageConfig {
		strategy := d.retrieveOrCreate(storageConfig)
		if strategy != nil {
			newStrategies[strategy.GetStrategyName()] = strategy
			d.logger.Debug().
				Str("strategy", strategy.GetStrategyName()).
				Msg("Strategy active")
		}
	}

	d.strategies = make([]SyncStrategy, 0, len(newStrategies))
	for _, strategy := range newStrategies {
		d.strategies = append(d.strategies, strategy)
	}
}

func (d *dataStore) retrieveOrCreate(strategyConfig configuration.StorageConfig) SyncStrategy {
	localLogger := d.logger.With().
		Str("function", "retrieveOrCreate").
		Str("strategy", strategyConfig.Name).
		Logger()

	localLogger.Debug().Msg("retrieveOrCreate called")

	searchStrategyId := d.GenerateStrategyId(strategyConfig.Name, strategyConfig.Params)

	// Recherche d'une stratégie existante
	for _, strategy := range d.strategies {
		strategyId := d.GenerateStrategyId(strategy.GetStrategyName(), strategy.GetStrategyParams())
		if strategyId == searchStrategyId {
			localLogger.Debug().Msg("Found existing strategy")
			return strategy
		}
	}

	// Création d'une nouvelle stratégie
	localLogger.Debug().
		Any("params", strategyConfig.Params).
		Msg("Creating new strategy")

	var strategy SyncStrategy
	switch strategyConfig.Name {
	case "senhub":
		localLogger.Debug().Msg("Initializing senhub strategy")
		strategy = NewSyncStrategySenhub(d.agentConfig, strategyConfig.Params, d.logger)
	case "prtg":
		localLogger.Debug().Msg("Initializing prtg strategy")
		strategy = NewSyncStrategyPrtg(d.agentConfig, strategyConfig.Params, d.logger)
	case "event":
		localLogger.Debug().Msg("Initializing event strategy")
		strategy = NewEventSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger)
	default:
		localLogger.Error().
			Any("params", strategyConfig.Params).
			Msg("Unknown strategy")
		return nil
	}

	if strategy == nil {
		localLogger.Error().
			Any("params", strategyConfig.Params).
			Msg("Failed to create strategy")
		return nil
	}

	if err := strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
		localLogger.Error().
			Any("params", strategyConfig.Params).
			Err(err).
			Msg("Invalid strategy configuration")
		return nil
	}

	if err := strategy.Start(); err != nil {
		localLogger.Error().
			Err(err).
			Msg("Failed to start strategy")
		return nil
	}

	d.strategies = append(d.strategies, strategy)
	localLogger.Debug().Msg("Strategy created successfully")
	return strategy
}