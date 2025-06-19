//senhub-agent/internal/agent/services/data_store/data_store.go

// Package data_store implements configurable data routing system
// Responsibilities:
// - Routes datapoints to appropriate strategies
// - Manages strategy lifecycle (creation/destruction)
// - Handles hot configuration updates
package data_store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/prtg"
	"senhub-agent.go/internal/agent/services/data_store/strategies/senhub"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
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
	strategies          []SyncStrategy
	logger              *logger.ModuleLogger
	configProvider      configuration.ConfigurationProvider
	agentConfig         configuration.AgentConfiguration
	transformerRegistry *transformers.TransformerRegistry
}

func NewDataStore(
	agentConfig configuration.AgentConfiguration,
	configProvider configuration.ConfigurationProvider,
	baseLogger *logger.Logger,
) DataStore {
	// Create module-specific logger for data store
	moduleLogger := logger.NewModuleLogger(baseLogger, "data_store")
	moduleLogger.Debug().Msg("Creating new DataStore instance")

	ds := &dataStore{
		logger:              moduleLogger,
		configProvider:      configProvider,
		agentConfig:         agentConfig,
		strategies:          make([]SyncStrategy, 0),
		transformerRegistry: transformers.NewTransformerRegistry(baseLogger),
	}
	moduleLogger.Debug().Msg("DataStore instance created successfully")
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
		d.logger.Debug().Int("datapoints_count", len(data)).Msg("Callback called")

		if len(d.strategies) == 0 {
			d.logger.Warn().Msg("No strategies configured in datastore")
			return nil
		}

		// Apply unit corrections to all datapoints before routing to strategies
		correctedData := d.applyUnitCorrections(data)
		if len(correctedData) != len(data) {
			d.logger.Debug().
				Int("original_count", len(data)).
				Int("corrected_count", len(correctedData)).
				Msg("Unit corrections applied to datapoints")
		}

		for _, strategy := range d.strategies {
			targetStrategies := probe.GetTargetStrategies()
			d.logger.Debug().
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
				d.logger.Debug().
					Str("strategy", strategy.GetStrategyName()).
					Msg("Skipping strategy")
				continue
			}

			d.logger.Debug().
				Str("strategy", strategy.GetStrategyName()).
				Int("datapoints_count", len(correctedData)).
				Msg("Sending data to strategy")

			// Log the first few events for debugging
			if strategy.GetStrategyName() == "event" && len(correctedData) > 0 {
				for i := 0; i < min(3, len(correctedData)); i++ {
					d.logger.Debug().
						Int("event_index", i).
						Str("event_source", getTagValue(correctedData[i].Tags, "event_source")).
						Str("event_id", getTagValue(correctedData[i].Tags, "event_id")).
						Str("message", truncateString(getTagValue(correctedData[i].Tags, "message"), 100)).
						Msg("🔎 EVENT DETAIL - About to send to strategy")
				}
			}

			if err := strategy.AddDataPoints(correctedData); err != nil {
				d.logger.Error().
					Err(err).
					Str("strategy", strategy.GetStrategyName()).
					Msg("Error adding data points to strategy")
			} else {
				d.logger.Info().
					Str("strategy", strategy.GetStrategyName()).
					Int("count", len(correctedData)).
					Msg("✅ Successfully sent datapoints to strategy")
			}
		}
		return nil
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	d.logger.Debug().Msg("Starting DataStore service")
	d.OnConfigRefreshed("initial")
	d.configProvider.OnConfigChanged(d.OnConfigRefreshed)
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
	// Convert map[interface{}]interface{} to map[string]interface{} if needed
	fixedParams := d.convertMapTypes(params)

	paramsBytes, err := json.Marshal(fixedParams)
	if err != nil {
		d.logger.Error().
			Err(err).
			Str("strategy", strategyName).
			Any("params", params).
			Msg("marshaling error: failed to marshal strategy parameters - using empty config")
		paramsBytes = []byte("{}")
	}
	if len(params) == 0 {
		paramsBytes = []byte("{}")
	}

	input := fmt.Sprintf("%s-%s", strategyName, string(paramsBytes))
	hash := sha256.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// convertMapTypes recursively converts map[interface{}]interface{} to map[string]interface{}
// This fixes YAML parsing issues where Go unmarshals maps with interface{} keys
func (d *dataStore) convertMapTypes(input interface{}) interface{} {
	switch v := input.(type) {
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			if keyStr, ok := key.(string); ok {
				result[keyStr] = d.convertMapTypes(value)
			}
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			result[key] = d.convertMapTypes(value)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = d.convertMapTypes(item)
		}
		return result
	default:
		return v
	}
}

// OnConfigRefreshed updates strategy configurations based on remote changes.
// It ensures smooth transition between configurations by:
//   - Preserving existing strategy instances when possible
//   - Validating new configurations before applying
//   - Cleaning up unused strategies
func (d *dataStore) OnConfigRefreshed(reason string) {
	d.logger.Debug().Str("reason", reason).Msg("OnConfigRefreshed called")

	newStrategies := make(map[string]SyncStrategy)

	for _, storageConfig := range d.configProvider.GetConfiguration().StorageConfig {
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
	d.logger.Debug().
		Str("strategy", strategyConfig.Name).
		Msg("retrieveOrCreate called")

	searchStrategyId := d.GenerateStrategyId(strategyConfig.Name, strategyConfig.Params)

	// Search for existing strategy with the same name
	for _, strategy := range d.strategies {
		if strategy.GetStrategyName() == strategyConfig.Name {
			// Strategy of same type found, check if parameters have changed
			strategyId := d.GenerateStrategyId(strategy.GetStrategyName(), strategy.GetStrategyParams())
			if strategyId == searchStrategyId {
				d.logger.Debug().Msg("Found existing strategy with same configuration")
				return strategy
			} else {
				// Same name but different parameters - try to update
				d.logger.Info().
					Any("old_params", strategy.GetStrategyParams()).
					Any("new_params", strategyConfig.Params).
					Msg("Strategy configuration changed, attempting update")

				// Try to update the strategy if it supports live updates
				if httpStrategy, ok := strategy.(*http.HTTPSyncStrategy); ok {
					if err := httpStrategy.UpdateConfiguration(strategyConfig.Params); err != nil {
						d.logger.Warn().
							Err(err).
							Msg("Failed to update strategy configuration, will recreate")
						// If update fails, continue to create a new strategy
						break
					} else {
						d.logger.Info().Msg("✅ Strategy configuration updated successfully")
						return strategy
					}
				} else {
					d.logger.Debug().Msg("Strategy does not support live updates, will recreate")
					// Strategy does not support live updates, continue to recreate
					break
				}
			}
		}
	}

	// Create a new strategy
	d.logger.Debug().
		Any("params", strategyConfig.Params).
		Msg("Creating new strategy")

	var strategy SyncStrategy
	switch strategyConfig.Name {
	case "senhub":
		d.logger.Debug().Msg("Initializing senhub strategy")
		strategy = senhub.NewSyncStrategySenhub(d.agentConfig, strategyConfig.Params, d.logger.Logger).(SyncStrategy)
	case "prtg":
		d.logger.Debug().Msg("Initializing prtg strategy")
		strategy = prtg.NewSyncStrategyPrtg(d.agentConfig, strategyConfig.Params, d.logger.Logger)
	case "event":
		d.logger.Debug().Msg("Initializing event strategy")
		strategy = event.NewEventSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger.Logger).(SyncStrategy)
	case "http":
		d.logger.Debug().Msg("Initializing HTTP strategy")
		strategy = http.NewHTTPSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger.Logger).(SyncStrategy)
		d.logger.Debug().Bool("initialized", strategy != nil).Msg("HTTP strategy created")
	default:
		d.logger.Error().
			Any("params", strategyConfig.Params).
			Msg("Unknown strategy")
		return nil
	}

	if strategy == nil {
		d.logger.Error().
			Any("params", strategyConfig.Params).
			Msg("Failed to create strategy")
		return nil
	}

	if err := strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
		d.logger.Error().
			Any("params", strategyConfig.Params).
			Err(err).
			Msg("Invalid strategy configuration")
		return nil
	}

	if err := strategy.Start(); err != nil {
		d.logger.Error().
			Err(err).
			Msg("Failed to start strategy")
		return nil
	}

	d.strategies = append(d.strategies, strategy)
	d.logger.Debug().Msg("Strategy created successfully")
	return strategy
}

// applyUnitCorrections applies unit corrections to datapoints for consistent metrics across all strategies
func (d *dataStore) applyUnitCorrections(datapoints []datapoint.DataPoint) []datapoint.DataPoint {
	if len(datapoints) == 0 {
		return datapoints
	}

	correctedDatapoints := make([]datapoint.DataPoint, len(datapoints))
	correctionCount := 0

	for i, dp := range datapoints {
		// Convert tags from []tags.Tag to map[string]string for transformer
		tags := make(map[string]string)
		for _, tag := range dp.Tags {
			tags[tag.Key] = tag.Value
		}

		// Get probe name from tags to load appropriate transformer
		probeName := tags["probe_name"]
		if probeName == "" {
			// If no probe_name, copy datapoint as-is
			correctedDatapoints[i] = dp
			continue
		}

		// Load transformer for this probe
		transformer, err := d.transformerRegistry.LoadTransformer(probeName, "friendly")
		if err != nil {
			d.logger.Debug().
				Err(err).
				Str("probe_name", probeName).
				Msg("No transformer available for unit correction")
			correctedDatapoints[i] = dp
			continue
		}

		// Try to apply unit corrections if transformer supports them
		correctedValue := dp.Value
		if defTransformer, ok := transformer.(interface {
			ApplyUnitCorrection(string, float64, map[string]string) (float64, bool)
		}); ok {
			// Convert value to float64 for correction calculation
			originalFloat64 := float64(dp.Value)
			if newValue, applied := defTransformer.ApplyUnitCorrection(dp.Name, originalFloat64, tags); applied {
				correctedValue = float32(newValue)
				correctionCount++

				d.logger.Info().
					Str("metric", dp.Name).
					Str("probe", probeName).
					Float64("original_value", originalFloat64).
					Float64("corrected_value", newValue).
					Float64("correction_factor", newValue/originalFloat64).
					Msg("🔧 Unit correction applied to datapoint - ensuring consistent units across all strategies")
			}
		}

		// Create corrected datapoint
		correctedDatapoints[i] = datapoint.DataPoint{
			Name:      dp.Name,
			Value:     correctedValue,
			Timestamp: dp.Timestamp,
			Tags:      dp.Tags, // Keep original tags structure
		}
	}

	if correctionCount > 0 {
		d.logger.Info().
			Int("total_datapoints", len(datapoints)).
			Int("corrections_applied", correctionCount).
			Msg("✅ Unit corrections completed - all strategies will receive corrected metrics")
	}

	return correctedDatapoints
}
