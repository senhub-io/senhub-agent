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

	// What each strategy reads, so `agent config check` can say that a
	// key is read by nobody instead of reporting the file as valid and
	// letting the agent ignore it (#846). Kept in step with the code by
	// the guard test in this package.
	data_store.RegisterKnownParams("otlp", []string{
		"batch_size", "batch_timeout", "buffer_size", "ca_file", "cert_file",
		"check_interval", "compression", "depends_on_debounce",
		"depends_on_enabled", "depends_on_exclude_cidrs", "enabled",
		"endpoint", "enrichment", "entities", "fallback_endpoints",
		"governance", "hard_mib", "headers", "idle_conn_timeout",
		"initial_interval", "insecure_skip_verify", "interval", "key",
		"key_file", "logs", "logs_queue_max_bytes", "match",
		"max_active_series_per_probe", "max_concurrent_exports",
		"max_elapsed_time", "max_interval", "max_store_size", "memory_limit",
		"metrics", "org_id", "path", "persistence", "probe_name", "probe_type",
		"protocol", "redact_attributes", "relay", "relay_enrichment",
		"relay_tenant_overrides", "resource", "retry", "sample_ratio",
		"signals", "soft_mib", "staleness_ttl", "tags", "temporality",
		"tenant", "timeout", "tls", "traces", "unit", "url_path_prefix",
		"value",
	}, map[string]string{
		// The spelling most OTLP tooling uses for "no TLS". This
		// agent expresses it as a block, and reading the alias as
		// nothing meant an operator asked for plaintext and got a
		// TLS handshake (#846).
		"insecure": "tls: { enabled: false }",
	})

	data_store.RegisterKnownParams("senhub", []string{
		"interval",
	}, nil)

	data_store.RegisterKnownParams("http", []string{
		"active_probe_count", "agentkey", "bind_address", "cert_file",
		"component", "connectivity", "css", "dashboard", "docs", "enabled",
		"endpoint", "endpoints", "exclude_tags", "expose_host_metrics",
		"guide", "html", "include_probe_tags", "instance", "interval", "js",
		"key_file", "le", "lookup_id", "max_cache_size", "metrics",
		"min_tls_version", "nagios_checks_count", "nagios_version", "name",
		"port", "probe", "probe_name", "probe_type", "probes", "prometheus", "protocol",
		"schema", "server_configured", "settings", "tags", "target", "tls",
		"tls_min_version", "total_metrics", "unit", "url",
	}, nil)

	data_store.RegisterKnownParams("prtg", []string{
		"data_retention_period", "interval", "probe_name", "probe_type",
		"server_url",
	}, nil)

	data_store.RegisterKnownParams("event", []string{
		"queue_size", "server_url", "sync_interval",
	}, nil)

	// How to check a configuration without building anything, so
	// `agent config check` refuses what the agent would refuse rather
	// than reporting it valid and letting the strategy be dropped at
	// construction (#848).
	data_store.RegisterParamValidator("otlp", func(params configuration.StorageConfigParams) error {
		if _, err := otlp.ParseConfig(params); err != nil {
			return err
		}
		return otlp.ValidateEntitiesRedactAttributes(params)
	})

	data_store.RegisterParamValidator("senhub", func(params configuration.StorageConfigParams) error {
		_, err := senhub.ParseSyncStrategySenhubParams(params)
		return err
	})

	data_store.RegisterParamValidator("prtg", func(params configuration.StorageConfigParams) error {
		_, err := prtg.ParseSyncStrategyPrtgParams(params)
		return err
	})

	data_store.RegisterParamValidator("http", func(params configuration.StorageConfigParams) error {
		return http.ValidateParams(params)
	})

	data_store.RegisterParamValidator("event", func(params configuration.StorageConfigParams) error {
		_, err := event.ValidateParams(params)
		return err
	})

	http.OutputValidator = data_store.ValidateStrategyParams
}
