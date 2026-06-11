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
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
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
	// strategies holds an immutable snapshot of the active strategy
	// set. Probe goroutines Load() it on every datapoint callback;
	// the config watcher builds a NEW slice and Store()s it — readers
	// never observe a partially rebuilt list (#260).
	strategies atomic.Pointer[[]SyncStrategy]
	// refreshMu serializes configuration refreshes (single writer).
	refreshMu           sync.Mutex
	logger              *logger.ModuleLogger
	configProvider      configuration.ConfigurationProvider
	agentConfig         configuration.AgentConfiguration
	transformerRegistry *transformers.TransformerRegistry
}

// activeStrategies returns the current immutable strategy snapshot.
func (d *dataStore) activeStrategies() []SyncStrategy {
	if p := d.strategies.Load(); p != nil {
		return *p
	}
	return nil
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
		transformerRegistry: transformers.NewTransformerRegistry(baseLogger),
	}
	empty := make([]SyncStrategy, 0)
	ds.strategies.Store(&empty)
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

// enrichWithConfiguredTags overlays the agent-level global_tags and each
// probe instance's custom_tags onto every datapoint, in one place for all
// sinks. Priority on a key conflict is custom_tags > global_tags > built-in
// (the tags the probe already emitted). Per-probe custom_tags are matched to
// a datapoint by its probe_name tag. No-op (and no allocation) when neither
// is configured.
func (d *dataStore) enrichWithConfiguredTags(data []datapoint.DataPoint) []datapoint.DataPoint {
	cfg := d.configProvider.GetConfiguration()
	global := tags.MapToTags(cfg.Agent.GlobalTags)

	var customByProbe map[string][]tags.Tag
	for _, p := range cfg.Probes {
		if len(p.CustomTags) == 0 {
			continue
		}
		if customByProbe == nil {
			customByProbe = make(map[string][]tags.Tag)
		}
		customByProbe[p.Name] = tags.MapToTags(p.CustomTags)
	}

	if len(global) == 0 && customByProbe == nil {
		return data
	}

	for i := range data {
		custom := customByProbe[getTagValue(data[i].Tags, "probe_name")]
		if len(global) == 0 && len(custom) == 0 {
			continue
		}
		data[i].Tags = tags.MergeTags(data[i].Tags, global, custom)
	}
	return data
}

func (d *dataStore) GetCallback() AddCallback {
	d.logger.Debug().Msg("GetCallback called")
	return func(data []datapoint.DataPoint, probe StrategyRouter) error {
		d.logger.Debug().Int("datapoints_count", len(data)).Msg("Callback called")

		strategies := d.activeStrategies()
		if len(strategies) == 0 {
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

		// Apply configured tags uniformly before routing to any sink:
		// agent global_tags + per-probe custom_tags, priority
		// custom_tags > global_tags > built-in probe tags.
		correctedData = d.enrichWithConfiguredTags(correctedData)

		for _, strategy := range strategies {
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
	for _, strategy := range d.activeStrategies() {
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

	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	previous := d.activeStrategies()
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

	next := make([]SyncStrategy, 0, len(newStrategies))
	kept := make(map[SyncStrategy]bool, len(newStrategies))
	for _, strategy := range newStrategies {
		next = append(next, strategy)
		kept[strategy] = true
	}
	d.strategies.Store(&next)

	// Shut down strategies dropped or replaced by this refresh —
	// otherwise their listener ports, gRPC connections and scheduler
	// goroutines leak (and a recreated HTTP strategy fails to bind).
	for _, old := range previous {
		if kept[old] {
			continue
		}
		d.logger.Info().
			Str("strategy", old.GetStrategyName()).
			Msg("Shutting down strategy removed by config refresh")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := old.Shutdown(ctx); err != nil {
			d.logger.Error().
				Err(err).
				Str("strategy", old.GetStrategyName()).
				Msg("Failed to shut down removed strategy")
		}
		cancel()
	}
}

func (d *dataStore) retrieveOrCreate(strategyConfig configuration.StorageConfig) SyncStrategy {
	d.logger.Debug().
		Str("strategy", strategyConfig.Name).
		Msg("retrieveOrCreate called")

	searchStrategyId := d.GenerateStrategyId(strategyConfig.Name, strategyConfig.Params)

	// Search for existing strategy with the same name
	for _, strategy := range d.activeStrategies() {
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
		eventStrategy, err := event.NewEventSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger.Logger)
		if err != nil {
			d.logger.Error().
				Err(err).
				Msg("Invalid event strategy configuration, strategy skipped")
			return nil
		}
		strategy = eventStrategy
	case "http":
		d.logger.Debug().Msg("Initializing HTTP strategy")
		strategy = http.NewHTTPSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger.Logger).(SyncStrategy)
		d.logger.Debug().Bool("initialized", strategy != nil).Msg("HTTP strategy created")
	case "otlp":
		d.logger.Debug().Msg("Initializing OTLP strategy")
		strategy = otlp.NewOTLPSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger.Logger).(SyncStrategy)
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
		tagMap := make(map[string]string)
		for _, tag := range dp.Tags {
			tagMap[tag.Key] = tag.Value
		}

		// Get probe name and type from tags to load appropriate transformer
		probeName := tagMap["probe_name"]
		probeType := tagMap["probe_type"]
		if probeName == "" {
			// If no probe_name, copy datapoint as-is
			correctedDatapoints[i] = dp
			continue
		}

		// Fallback: if probe_type is missing, use probe_name (backward compatibility)
		if probeType == "" {
			probeType = probeName
		}

		// Load transformer for this probe
		// IMPORTANT: Use probeType (technical identifier) NOT probeName (display name)
		// This ensures multiple probes of the same type share transformer definitions
		transformer, err := d.transformerRegistry.LoadTransformer(probeType, "friendly")
		if err != nil {
			d.logger.Debug().
				Err(err).
				Str("probe_name", probeName).
				Str("probe_type", probeType).
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
			if newValue, applied := defTransformer.ApplyUnitCorrection(dp.Name, originalFloat64, tagMap); applied {
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

		// Inject the unit tag from the YAML definition when the probe
		// didn't add one. Producers (cpu, memory, …) emit raw DataPoints
		// without a unit tag and rely on the YAML to declare it; without
		// the tag, downstream strategies that consume DataPoints directly
		// (OTLP push, future Zabbix, …) cannot trigger unit-based
		// conversions in otelmapper.Resolve (% → ratio, MB → bytes, …)
		// and emit wrong absolute values.
		//
		// Done once here, in the data_store, so every strategy receives
		// a fully-tagged datapoint. Single source of truth: the YAML.
		enrichedTags := dp.Tags
		if _, hasUnit := tagMap["unit"]; !hasUnit {
			if unitProvider, ok := transformer.(interface {
				GetUnit(metricName string) string
			}); ok {
				if u := unitProvider.GetUnit(dp.Name); u != "" {
					enrichedTags = append([]tags.Tag{}, dp.Tags...)
					enrichedTags = append(enrichedTags, tags.Tag{Key: "unit", Value: u})
				}
			}
		}

		// Create corrected datapoint
		correctedDatapoints[i] = datapoint.DataPoint{
			Name:      dp.Name,
			Value:     correctedValue,
			Timestamp: dp.Timestamp,
			Tags:      enrichedTags,
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
