// Package strategyreg wires the strategy implementations into the data
// store's registry.
//
// It exists so the hub and the strategies do not import each other: the
// hub declares what a strategy is, each strategy package implements it,
// and this package — imported for its side effects by whatever builds an
// agent — introduces them. Without it, either the hub imports every
// concrete sink (the switch this replaced) or a sink imports the hub and
// the graph loops.
package strategyreg

import (
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/data_store/strategies/prtg"
	"senhub-agent.go/internal/agent/services/data_store/strategies/senhub"
)

func init() {
	data_store.RegisterStrategy("senhub", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		return senhub.NewSyncStrategySenhub(deps.AgentConfig, params, deps.Logger).(data_store.SyncStrategy), nil
	})

	data_store.RegisterStrategy("prtg", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		return prtg.NewSyncStrategyPrtg(deps.AgentConfig, params, deps.Logger, deps.Registry), nil
	})

	data_store.RegisterStrategy("event", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		return event.NewEventSyncStrategy(deps.AgentConfig, params, deps.Logger)
	})

	data_store.RegisterStrategy("http", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		return http.NewHTTPSyncStrategy(deps.AgentConfig, params, deps.Logger).(data_store.SyncStrategy), nil
	})

	data_store.RegisterStrategy("otlp", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		return otlp.NewOTLPSyncStrategy(deps.AgentConfig, params, deps.Logger).(data_store.SyncStrategy), nil
	})
}
