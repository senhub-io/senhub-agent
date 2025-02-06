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
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
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
	ticker       *time.Ticker
	tickerOnce   sync.Once
}

func NewDataStore(
	agentConfig configuration.AgentConfiguration,
	remoteConfig *configuration.RemoteConfiguration,
	logger *logger.Logger,
) DataStore {
	fmt.Printf("[DEBUG] Creating new DataStore instance\n")
	localLogger := logger.With().Str("service", "DataStore").Logger()

	ds := &dataStore{
		logger:       &localLogger,
		remoteConfig: remoteConfig,
		agentConfig:  agentConfig,
		strategies:   make([]SyncStrategy, 0),
	}
	fmt.Printf("[DEBUG] DataStore instance created successfully\n")
	return ds
}

func (d *dataStore) GetName() string {
	return "DataStore"
}

func (d *dataStore) GetCallback() AddCallback {
	fmt.Printf("[DEBUG] GetCallback called\n")
	return func(data []datapoint.DataPoint, probe StrategyRouter) error {
		fmt.Printf("[DEBUG] Callback executed with %d datapoints\n", len(data))

		if len(d.strategies) == 0 {
			fmt.Printf("[WARNING] No strategies configured in datastore\n")
			return nil
		}

		for _, strategy := range d.strategies {
			targetStrategies := probe.GetTargetStrategies()
			fmt.Printf("[DEBUG] Target strategies: %v\n", targetStrategies)

			shouldSendToStrategy := false
			for _, target := range targetStrategies {
				if target == strategy.GetStrategyName() {
					shouldSendToStrategy = true
					break
				}
			}

			if !shouldSendToStrategy {
				fmt.Printf("[DEBUG] Skipping strategy %s\n", strategy.GetStrategyName())
				continue
			}

			fmt.Printf("[DEBUG] Sending %d datapoints to strategy %s\n",
				len(data), strategy.GetStrategyName())

			if err := strategy.AddDataPoints(data); err != nil {
				fmt.Printf("[ERROR] Error adding data points to strategy %s: %v\n",
					strategy.GetStrategyName(), err)
			} else {
				fmt.Printf("[DEBUG] Successfully sent datapoints to strategy %s\n",
					strategy.GetStrategyName())
			}
		}
		return nil
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	fmt.Printf("[DEBUG] DataStore Start called\n")
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
	fmt.Printf("[DEBUG] OnConfigRefreshed called with reason: %s\n", reason)

	newStrategies := make(map[string]SyncStrategy)

	for _, storageConfig := range d.remoteConfig.GetConfiguration().StorageConfig {
		strategy := d.retrieveOrCreate(storageConfig)
		if strategy != nil {
			newStrategies[strategy.GetStrategyName()] = strategy
			fmt.Printf("[DEBUG] Strategy active: %s\n", strategy.GetStrategyName())
		}
	}

	d.strategies = make([]SyncStrategy, 0, len(newStrategies))
	for _, strategy := range newStrategies {
		d.strategies = append(d.strategies, strategy)
	}
}

func (d *dataStore) retrieveOrCreate(strategyConfig configuration.StorageConfig) SyncStrategy {
	fmt.Printf("[DEBUG] retrieveOrCreate called for strategy: %s\n", strategyConfig.Name)
	searchStrategyId := d.GenerateStrategyId(strategyConfig.Name, strategyConfig.Params)

	// Recherche d'une stratégie existante
	for _, strategy := range d.strategies {
		strategyId := d.GenerateStrategyId(strategy.GetStrategyName(), strategy.GetStrategyParams())
		if strategyId == searchStrategyId {
			fmt.Printf("[DEBUG] Found existing strategy: %s\n", strategy.GetStrategyName())
			return strategy
		}
	}

	// Création d'une nouvelle stratégie
	fmt.Printf("[DEBUG] Creating new strategy: %s\n", strategyConfig.Name)

	var strategy SyncStrategy
	switch strategyConfig.Name {
	case "senhub":
		fmt.Printf("[INFO] Initializing senhub strategy\n")
		strategy = NewSyncStrategySenhub(d.agentConfig, strategyConfig.Params, d.logger)
	case "prtg":
		fmt.Printf("[INFO] Initializing prtg strategy\n")
		strategy = NewSyncStrategyPrtg(d.agentConfig, strategyConfig.Params, d.logger)
	case "event":
		fmt.Printf("[INFO] Initializing event strategy\n")
		strategy = NewEventSyncStrategy(d.agentConfig, strategyConfig.Params, d.logger)
	default:
		fmt.Printf("[ERROR] Unknown strategy: %s\n", strategyConfig.Name)
		return nil
	}

	if strategy == nil {
		fmt.Printf("[ERROR] Failed to create strategy: %s\n", strategyConfig.Name)
		return nil
	}

	if err := strategy.ValidateConfigParams(strategyConfig.Params); err != nil {
		fmt.Printf("[ERROR] Invalid configuration for strategy %s: %v\n",
			strategyConfig.Name, err)
		return nil
	}

	if err := strategy.Start(); err != nil {
		fmt.Printf("[ERROR] Failed to start strategy %s: %v\n",
			strategyConfig.Name, err)
		return nil
	}

	d.strategies = append(d.strategies, strategy)
	fmt.Printf("[INFO] Successfully started new strategy: %s\n", strategy.GetStrategyName())
	return strategy
}
