package otlp

import (
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func init() {
	transport := func() []spec.ParamSpec {
		return []spec.ParamSpec{
			{Key: "endpoint", Kind: spec.KindString, Advanced: true, Description: "Endpoint for this signal only; the root endpoint otherwise"},
			{Key: "headers", Kind: spec.KindMap, Secret: true, Advanced: true, Description: "Headers for this signal only; they replace the root headers"},
			{Key: "tls", Kind: spec.KindBlock, Advanced: true, Description: "TLS for this signal only; it replaces the root block", Fields: tlsFields()},
		}
	}
	outputspec.Register(outputspec.Output{
		Type: "otlp", DisplayName: "OTLP push", Mode: outputspec.ModePush,
		Summary:  "Pushes metrics, logs, traces and entities to an OpenTelemetry collector or an OTLP-native backend, over gRPC or HTTP.",
		DocsPath: "docs/user-guide/docs/otlp.md",
		Params: []spec.ParamSpec{
			{Key: "endpoint", Kind: spec.KindString, Required: true, Group: "connection", Description: "Collector address as host:port; no default on purpose", Example: "otel-collector.example.com:4317"},
			{Key: "protocol", Kind: spec.KindString, Default: "grpc", Enum: []string{"grpc", "http"}, Essential: true, Group: "connection", Description: "OTLP transport"},
			{Key: "tls", Kind: spec.KindBlock, Group: "connection", Description: "TLS towards the collector; on and verified by default", Fields: tlsFields()},
			{Key: "headers", Kind: spec.KindMap, Secret: true, Essential: true, Group: "auth", Description: "Headers sent with every request; Authorization carries a bearer token; values are kept in the secret store", Example: "Authorization=Bearer …"},
			{Key: "tenant", Kind: spec.KindString, AlsoAccepts: []string{"org_id"}, Group: "auth", Description: "Sent as X-Scope-OrgID on every signal (Mimir, Loki, Tempo, VictoriaMetrics)"},
			{Key: "url_path_prefix", Kind: spec.KindString, Group: "auth", Description: "Base path in front of /v1/metrics and friends, for backends that serve OTLP under a path; HTTP only", Example: "/api/v2/otlp"},
			{Key: "signals", Kind: spec.KindBlock, Group: "signals", Description: "Which signals leave, and how", Fields: []spec.ParamSpec{
				{Key: "metrics", Kind: spec.KindBlock, Fields: append([]spec.ParamSpec{
					{Key: "enabled", Kind: spec.KindBool, Default: true},
					{Key: "interval", Kind: spec.KindDuration, Default: "30s", Description: "Push cadence"},
					{Key: "temporality", Kind: spec.KindString, Default: "cumulative", Enum: []string{"cumulative", "delta"}},
				}, transport()...)},
				{Key: "logs", Kind: spec.KindBlock, Fields: append([]spec.ParamSpec{
					{Key: "enabled", Kind: spec.KindBool, Default: true},
					{Key: "batch_size", Kind: spec.KindInt, Default: 1000},
					{Key: "batch_timeout", Kind: spec.KindDuration, Default: "5s"},
					{Key: "buffer_size", Kind: spec.KindInt, Default: 10000},
				}, transport()...)},
				{Key: "traces", Kind: spec.KindBlock, Description: "Spans relayed from an otlp_receiver probe; nothing to send without one", Fields: append([]spec.ParamSpec{
					{Key: "enabled", Kind: spec.KindBool, Default: false},
					{Key: "batch_size", Kind: spec.KindInt, Default: 512},
					{Key: "batch_timeout", Kind: spec.KindDuration, Default: "5s"},
					{Key: "buffer_size", Kind: spec.KindInt, Default: 2048},
					{Key: "sample_ratio", Kind: spec.KindFloat, Default: 1.0},
					{Key: "relay_enrichment", Kind: spec.KindBool, Default: true, Advanced: true, Description: "Deprecated; use relay.enrichment"},
				}, transport()...)},
				{Key: "entities", Kind: spec.KindBlock, Description: "Entity events for a topology backend such as Toise", Fields: []spec.ParamSpec{
					{Key: "enabled", Kind: spec.KindBool, Default: false},
					{Key: "interval", Kind: spec.KindDuration, Default: "60s"},
					{Key: "buffer_size", Kind: spec.KindInt, Default: 256},
					{Key: "depends_on_enabled", Kind: spec.KindBool, Default: true, Advanced: true, Description: "Emit depends_on relationships from observed connections"},
					{Key: "depends_on_debounce", Kind: spec.KindInt, Default: 3, Advanced: true, Description: "Sweeps a connection must survive before it becomes a relationship"},
					{Key: "depends_on_exclude_cidrs", Kind: spec.KindStringList, Advanced: true, Description: "Peers in these ranges never become relationships"},
					{Key: "redact_attributes", Kind: spec.KindStringList, Advanced: true, Description: "Entity attributes replaced by [REDACTED] before export"},
				}},
			}},
			{Key: "relay", Kind: spec.KindBlock, Group: "signals", Description: "Telemetry forwarded on behalf of applications (otlp_receiver)", Fields: []spec.ParamSpec{
				{Key: "enrichment", Kind: spec.KindBool, Default: true, Description: "Add the agent's host attributes to relayed telemetry, on keys the application left empty"},
			}},
			{Key: "compression", Kind: spec.KindString, Default: "gzip", Enum: []string{"gzip", "none"}, Group: "delivery"},
			{Key: "timeout", Kind: spec.KindDuration, Default: "60s", Group: "delivery", Description: "Budget for one export call"},
			{Key: "retry", Kind: spec.KindBlock, Group: "delivery", Fields: []spec.ParamSpec{
				{Key: "enabled", Kind: spec.KindBool, Default: true},
				{Key: "initial_interval", Kind: spec.KindDuration, Default: "5s"},
				{Key: "max_interval", Kind: spec.KindDuration, Default: "30s"},
				{Key: "max_elapsed_time", Kind: spec.KindDuration, Default: "1m"},
			}},
			{Key: "max_concurrent_exports", Kind: spec.KindInt, Default: 4, Group: "delivery", Description: "Parallel export calls per push cycle; 1 disables the parallel path"},
			{Key: "idle_conn_timeout", Kind: spec.KindDuration, Group: "delivery", Description: "Close an idle HTTP connection after this long, before a load balancer resets it; HTTP only"},
			{Key: "fallback_endpoints", Kind: spec.KindStringList, Group: "delivery", Description: "Standby collectors tried in order while the primary fails"},
			{Key: "resource", Kind: spec.KindMap, Group: "resource", Description: "Resource attributes stamped on everything; service.name defaults to senhub-agent", Example: "deployment.environment=production"},
			{Key: "max_store_size", Kind: spec.KindInt, Default: 50000, Group: "memory", Description: "Distinct series kept between exports; new ones are dropped past it"},
			{Key: "max_active_series_per_probe", Kind: spec.KindInt, Default: 10000, Group: "memory"},
			{Key: "staleness_ttl", Kind: spec.KindDuration, Default: "10m", Group: "memory", Description: "A series not updated for this long is evicted"},
			{Key: "memory_limit", Kind: spec.KindBlock, Group: "memory", Fields: []spec.ParamSpec{
				{Key: "soft_mib", Kind: spec.KindInt, Default: 200},
				{Key: "hard_mib", Kind: spec.KindInt, Default: 400},
				{Key: "check_interval", Kind: spec.KindDuration, Default: "5s"},
			}},
			{Key: "persistence", Kind: spec.KindBlock, Group: "memory", Description: "On-disk checkpoint of the store and dead-letter queue of the logs", Fields: []spec.ParamSpec{
				{Key: "enabled", Kind: spec.KindBool, Default: true},
				{Key: "path", Kind: spec.KindString, Description: "Directory; empty disables the checkpoint", Example: "/var/lib/senhub-agent/otlp"},
				{Key: "interval", Kind: spec.KindDuration, Default: "30s"},
				{Key: "logs_queue_max_bytes", Kind: spec.KindInt, Default: 134217728},
			}},
		},
	})
}

func tlsFields() []spec.ParamSpec {
	return []spec.ParamSpec{
		{Key: "enabled", Kind: spec.KindBool, Default: true, Description: "false sends in clear text"},
		{Key: "insecure_skip_verify", Kind: spec.KindBool, Default: false, Description: "Accept the collector certificate without verifying it"},
		{Key: "ca_file", Kind: spec.KindString, Description: "CA certificate (PEM) the collector is verified against"},
		{Key: "cert_file", Kind: spec.KindString, Description: "Client certificate for mTLS; with key_file"},
		{Key: "key_file", Kind: spec.KindString, Description: "Client key for mTLS; with cert_file"},
	}
}
