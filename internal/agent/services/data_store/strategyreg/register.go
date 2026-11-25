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
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/event"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/data_store/strategies/prtg"
	"senhub-agent.go/internal/agent/services/data_store/strategies/senhub"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix"
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

	data_store.RegisterStrategy("zabbix", func(params configuration.StorageConfigParams, deps data_store.StrategyDeps) (data_store.SyncStrategy, error) {
		var defs otelmapper.DefinitionLookup
		if deps.Registry != nil {
			defs = deps.Registry
		}
		return zabbix.New(params, deps.Logger, defs), nil
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
		"key_file", "logs", "logs_queue_max_age", "logs_queue_max_bytes", "match",
		"max_active_series_per_probe", "max_concurrent_exports",
		"max_elapsed_time", "max_export_bytes", "max_interval", "max_store_size", "memory_limit",
		"metrics", "metrics_queue_max_age", "metrics_queue_max_bytes", "org_id", "path", "persistence", "probe_name", "probe_type",
		"protocol", "redact_attributes", "relay", "relay_enrichment",
		"relay_tenant_overrides", "resource", "retry", "sample_ratio",
		"signals", "soft_mib", "staleness_ttl", "tags", "temporality",
		"tenant", "timeout", "tls", "traces", "traces_queue_max_age", "traces_queue_max_bytes", "unit", "url_path_prefix",
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
		"active_probe_count", "admin_key", "allow_exec", "agentkey", "bind_address", "cert_file", "check",
		"check_result_channel",
		"component", "connectivity", "console", "css", "dashboard", "docs", "enabled",
		"endpoint", "endpoints", "exclude_tags", "expose_host_metrics",
		"guide", "html", "include_probe_tags", "instance", "interval", "js",
		"key_file", "le", "lookup_id", "max_cache_size", "metrics",
		"min_tls_version", "nagios_checks_count", "nagios_version", "name",
		"port", "probe", "probe_name", "probe_type", "probes", "prometheus", "protocol",
		"prtg", "read_only", "schema", "server_configured", "settings", "tags", "target", "tls",
		"tls_min_version", "total_metrics", "unit", "url",
	}, nil)

	// The sub-blocks the http strategy reads key by key
	// (ConfigurationManager.parseParams).
	data_store.RegisterKnownBlockParams("http", "prtg", []string{"check_result_channel"})
	data_store.RegisterKnownBlockParams("http", "prometheus", []string{"include_probe_tags", "expose_host_metrics"})
	// cipher_suites is written by config init (configuration.TLSConfig)
	// even though nothing reads it; a generated file must not warn.
	data_store.RegisterKnownBlockParams("http", "tls", []string{"enabled", "min_tls_version", "cert_file", "key_file", "cipher_suites"})

	data_store.RegisterKnownParams("prtg", []string{
		"data_retention_period", "interval", "probe_name", "probe_type",
		"server_url",
	}, nil)

	data_store.RegisterKnownParams("event", []string{
		"backlog_max_age", "backlog_max_bytes", "backlog_path", "queue_size",
		"server_url", "sync_interval",
	}, nil)

	data_store.RegisterKnownParams("zabbix", []string{
		"advertise", "allow", "bind_address", "ca_file", "cert_file", "enabled",
		"heartbeat_interval", "host_metadata", "hostname",
		"insecure_skip_verify", "interval", "key_file", "key_prefix", "passive",
		"port", "psk_file", "psk_identity", "refresh_interval", "server",
		"server_name", "timeout", "tls",
	}, map[string]string{
		// The spellings a Zabbix agent configuration uses, so an
		// operator transcribing zabbix_agentd.conf is told where the
		// setting moved instead of having it ignored.
		"tlspskidentity": "tls: { psk_identity: ... }",
		"tlspskfile":     "tls: { psk_file: ... }",
	})

	// The sub-blocks otlp reads key by key (config.go). Each list is
	// exactly what the named parser reads; the guard test in this
	// package fails when either side drifts. Blocks whose contents are
	// chosen by the operator (headers, resource, match, the
	// relay_tenant_overrides cards) are deliberately not declared: any
	// key inside them is the operator's to pick.
	data_store.RegisterKnownBlockParams("otlp", "tls", []string{
		"enabled", "insecure_skip_verify", "ca_file", "cert_file", "key_file",
	})
	data_store.RegisterKnownBlockParams("otlp", "retry", []string{
		"enabled", "initial_interval", "max_interval", "max_elapsed_time",
	})
	data_store.RegisterKnownBlockParams("otlp", "persistence", []string{
		"enabled", "path", "interval",
		"logs_queue_max_bytes", "logs_queue_max_age",
		"metrics_queue_max_bytes", "metrics_queue_max_age",
		"traces_queue_max_bytes", "traces_queue_max_age",
	})
	data_store.RegisterKnownBlockParams("otlp", "memory_limit", []string{
		"soft_mib", "hard_mib", "check_interval",
	})
	data_store.RegisterKnownBlockParams("otlp", "relay", []string{
		"enrichment",
	})
	// Per-signal blocks: what parseSignals reads directly, plus the
	// transport keys (endpoint, headers, tls) parseSignalTransport reads
	// out of the same map. entities.governance is read as a block handed
	// to governance.Parse, whose keys belong to that package.
	data_store.RegisterKnownBlockParams("otlp", "signals.metrics", []string{
		"enabled", "interval", "temporality", "endpoint", "headers", "tls",
	})
	data_store.RegisterKnownBlockParams("otlp", "signals.logs", []string{
		"enabled", "batch_size", "batch_timeout", "buffer_size",
		"max_export_bytes", "endpoint", "headers", "tls",
	})
	data_store.RegisterKnownBlockParams("otlp", "signals.traces", []string{
		"enabled", "batch_size", "batch_timeout", "buffer_size",
		"sample_ratio", "relay_enrichment", "relay_tenant_overrides",
		"endpoint", "headers", "tls",
	})
	data_store.RegisterKnownBlockParams("otlp", "signals.entities", []string{
		"enabled", "interval", "buffer_size", "depends_on_debounce",
		"depends_on_enabled", "depends_on_exclude_cidrs",
		"redact_attributes", "governance",
	})
	// A signal-level tls block is parsed by the same parseTLS.
	data_store.RegisterKnownBlockParams("otlp", "signals.metrics.tls", []string{
		"enabled", "insecure_skip_verify", "ca_file", "cert_file", "key_file",
	})
	data_store.RegisterKnownBlockParams("otlp", "signals.logs.tls", []string{
		"enabled", "insecure_skip_verify", "ca_file", "cert_file", "key_file",
	})
	data_store.RegisterKnownBlockParams("otlp", "signals.traces.tls", []string{
		"enabled", "insecure_skip_verify", "ca_file", "cert_file", "key_file",
	})

	// The sub-blocks zabbix reads key by key (config.go).
	data_store.RegisterKnownBlockParams("zabbix", "tls", []string{
		"enabled", "insecure_skip_verify", "ca_file", "cert_file",
		"key_file", "server_name", "psk_identity", "psk_file",
	})
	data_store.RegisterKnownBlockParams("zabbix", "passive", []string{
		"enabled", "bind_address", "port", "allow", "advertise", "tls",
	})
	data_store.RegisterKnownBlockParams("zabbix", "passive.tls", []string{
		"enabled", "cert_file", "key_file", "ca_file",
		"psk_identity", "psk_file",
	})

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

	data_store.RegisterParamValidator("zabbix", func(params configuration.StorageConfigParams) error {
		_, err := zabbix.ParseConfig(params)
		return err
	})

	http.OutputValidator = data_store.ValidateStrategyParams
}
