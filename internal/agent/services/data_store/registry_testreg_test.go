package data_store

import (
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/data_store/strategies/prtg"
	"senhub-agent.go/internal/agent/services/data_store/strategies/senhub"
)

// The strategies register themselves from `strategyreg`, which imports
// this package — so a test living IN this package cannot import it back.
// The registrations are repeated here instead, deliberately identical, so
// the hub's tests exercise the real strategies rather than fakes.
//
// A strategy added to strategyreg and forgotten here shows up as a test
// that cannot build its own configuration, which is a loud failure; the
// shipped set is pinned by strategyreg's own test.
func init() {
	RegisterStrategy("senhub", func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error) {
		return senhub.NewSyncStrategySenhub(deps.AgentConfig, params, deps.Logger).(SyncStrategy), nil
	})
	RegisterStrategy("prtg", func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error) {
		return prtg.NewSyncStrategyPrtg(deps.AgentConfig, params, deps.Logger, deps.Registry), nil
	})
	RegisterStrategy("event", func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error) {
		return event.NewEventSyncStrategy(deps.AgentConfig, params, deps.Logger)
	})
	RegisterStrategy("http", func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error) {
		return http.NewHTTPSyncStrategy(deps.AgentConfig, params, deps.Logger).(SyncStrategy), nil
	})
	RegisterStrategy("otlp", func(params configuration.StorageConfigParams, deps StrategyDeps) (SyncStrategy, error) {
		return otlp.NewOTLPSyncStrategy(deps.AgentConfig, params, deps.Logger).(SyncStrategy), nil
	})
}
