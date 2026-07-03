// Package otlp implements the OTLP export strategy for SenHub Agent.
//
// The strategy ships metrics (sourced from the same MetricCache as the
// Prometheus exposition, resolved through the neutral otelmapper package)
// and logs (sourced from a pub/sub log channel populated by syslog and
// event probes) over OTLP to any conformant OTLP receiver — an OTel
// collector or an OTLP-native backend.
//
// The transport is selectable via `protocol: grpc | http`, mirroring
// the two transports defined by the OpenTelemetry protocol spec. gRPC
// is the default. See client.go for the exporter wiring.
//
// Phase 1 (this commit) only wires up configuration parsing, the gRPC
// exporter clients, and the strategy lifecycle. Metrics export lands in
// Phase 2; logs export in Phase 3.
package otlp

import (
	"fmt"
	"net"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/governance"
)

// Default values mirror the OTel SDK defaults wherever one exists, so an
// operator who provides only `endpoint` gets behavior identical to the
// official collector. Where the SDK has no default (signal interval, log
// buffer size), we pick conservative values.
const (
	// No default endpoint on purpose: silently shipping metrics/logs to
	// localhost when an operator forgets to set `endpoint:` is a much
	// worse failure mode than refusing to start. Always require it.
	DefaultCompression = "gzip"
	// DefaultProtocol is the OTLP transport. The OTel spec defines two:
	// "grpc" (OTLP/gRPC) and "http" (OTLP/HTTP protobuf). "grpc" is the
	// historical — and pre-0.2.x only — behaviour, kept as the default
	// so existing deployments are unchanged. "http" is used to push to
	// any OTLP/HTTP receiver directly (e.g. a backend that ingests
	// OTLP/HTTP on its native /opentelemetry endpoints).
	DefaultProtocol = "grpc"
	// DefaultTimeout bounds a single OTLP export call. The OTel SDK uses
	// it as the gRPC context deadline. 60 s is generous enough to absorb
	// batches of 1000+ datapoints from the larger probes (IBM i with
	// its 29 collectors produced batches of ~2000 datapoints during
	// 0.1.93-beta validation; the previous 10 s default timed out
	// consistently, silently grew the export buffer cumulatively, and
	// looked like a probe stall from the operator perspective). Keep
	// it ≤ probe interval so a misconfigured endpoint surfaces quickly
	// without piling more datapoints on the buffer.
	DefaultTimeout            = 60 * time.Second
	DefaultMetricsInterval    = 30 * time.Second
	DefaultMetricsTemporality = "cumulative"
	DefaultLogsBatchSize      = 1000
	DefaultLogsBatchTimeout   = 5 * time.Second
	DefaultLogsBufferSize     = 10000
	DefaultEntitiesInterval   = 60 * time.Second
	DefaultEntitiesBufferSize = 256
	DefaultDependsOnDebounce  = 3
	// DefaultMaxStoreSize caps the OTLP strategy's metric-store
	// cardinality. 50 000 distinct series is comfortable for typical
	// SenHub agent profiles (host + 1-3 vendor probes ≈ 1-5 k series);
	// the cap fires for runaway-cardinality bugs (e.g. a probe emitting
	// per-request unique IDs as labels) before they OOM the agent.
	// Operators expecting >50 k series should bump this in YAML.
	DefaultMaxStoreSize = 50000

	// DefaultMaxActiveSeriesPerProbe is the per-probe cardinality
	// budget. Each probe instance (matched by probe_name) gets at most
	// this many distinct series before the limiter drops new ones with
	// `reason="probe_cardinality"`. Default 10 000 matches a heavy
	// vendor probe (IBM i ≈ 2k series, NetScaler with many vservers
	// ≈ 5k, headroom for spikes). 0 disables the per-probe budget
	// and falls back to MaxStoreSize alone.
	DefaultMaxActiveSeriesPerProbe = 10000

	// DefaultStalenessTTL is how long a stored series may go without a
	// new datapoint before the store evicts it instead of re-exporting
	// it with fresh timestamps forever (#308 zombie series: a probe
	// removed from config or denied by license left its checkpoint-
	// restored series exporting indefinitely, indistinguishable from
	// live data downstream). 10 minutes covers the slowest shipping
	// probe cadences (ibmi 120s, snmp topology sweeps) with margin.
	// 0 disables staleness eviction.
	DefaultStalenessTTL = 10 * time.Minute

	// Memory-limiter defaults. Soft and hard limits are measured
	// against runtime.MemStats.HeapAlloc (Go heap currently allocated,
	// not RSS — see memory_limiter.go for the rationale). The
	// soft/hard split mirrors the OTel Collector memory_limiter
	// processor's `limit_mib` / `spike_limit_mib` pair: soft refuses
	// new series, hard refuses everything and forces a GC. Operators
	// can disable each independently by setting it to 0.
	DefaultMemoryLimitSoftMiB     = 200
	DefaultMemoryLimitHardMiB     = 400
	DefaultMemoryLimitCheckPeriod = 5 * time.Second

	// Persistence defaults. When `persistence.enabled: true`, the LWW
	// store is checkpointed to disk every CheckpointInterval. Restores
	// at boot so dashboards see continuity across agent restarts (the
	// motivating use case: upgrade, OOM kill, OS reboot). Default OFF
	// for back-compat — operators opt in by setting the path.
	DefaultPersistenceInterval = 30 * time.Second

	// DefaultMaxConcurrentExports controls how many OTLP gRPC Export
	// calls can fire in parallel during one push cycle when the
	// strategy splits a large snapshot into per-probe sub-batches.
	// 4 is the sweet spot for the typical "1-5 probes × few-k series"
	// profile — splits the work but doesn't fan out so wide that
	// gRPC connection-level contention overwhelms otelcol-side
	// receivers. Set to 1 to disable splitting (back-compat single
	// gRPC Export per cycle, exactly as 0.1.93-beta pre-tier-2d).
	// Larger fleets can bump.
	DefaultMaxConcurrentExports = 4

	// SplitBatchThreshold gates the parallel-export path. Below this
	// many resolved records, splitting is pure overhead — encoding a
	// dozen series in a single batch is faster than spinning up 4
	// goroutines + 4 protobuf encoders. Above the threshold, the
	// per-probe split pays for itself.
	SplitBatchThreshold       = 100
	DefaultTracesBatchSize    = 512
	DefaultTracesBatchTimeout = 5 * time.Second
	DefaultTracesBufferSize   = 2048
	DefaultTracesSampleRatio  = 1.0
	DefaultRetryInitial       = 5 * time.Second
	DefaultRetryMax           = 30 * time.Second
	DefaultRetryMaxElapsed    = 1 * time.Minute
	DefaultServiceName        = "senhub-agent"
)

// TLSConfig mirrors the relevant subset of crypto/tls + grpc credentials
// options. Empty fields fall back to system defaults / SDK defaults.
type TLSConfig struct {
	Enabled            bool   // default true; explicitly set false for plaintext localhost
	InsecureSkipVerify bool   // default false; opt-in for self-signed certs in test
	CAFile             string // path to PEM CA bundle for verifying the server
	CertFile           string // path to client cert (mTLS) — optional
	KeyFile            string // path to client key  (mTLS) — optional
}

// RetryConfig mirrors the OTLP SDK retry block one-to-one. The SDK accepts
// `false` on Enabled to skip retries; we surface the same knob.
type RetryConfig struct {
	Enabled         bool
	InitialInterval time.Duration
	MaxInterval     time.Duration
	MaxElapsedTime  time.Duration
}

// SignalTransport holds the per-signal transport overrides. Each
// signal block (metrics/logs/traces) can override the root endpoint,
// headers, and TLS — matching the OTel SDK convention of
// OTEL_EXPORTER_OTLP_{METRICS,LOGS,TRACES}_ENDPOINT (etc.) taking
// precedence over OTEL_EXPORTER_OTLP_ENDPOINT.
//
// All fields are optional. When a field is zero/empty/nil, the signal
// inherits the corresponding root-level value. Headers replace (not
// merge with) root headers when set — mirrors SDK semantics where
// OTEL_EXPORTER_OTLP_METRICS_HEADERS fully overrides
// OTEL_EXPORTER_OTLP_HEADERS rather than being unioned.
type SignalTransport struct {
	Endpoint string
	Headers  map[string]string
	// TLS is a pointer so we can distinguish "not set, inherit root"
	// (nil) from "explicit zero value" (e.g., TLS disabled at signal
	// level even when root has it enabled).
	TLS *TLSConfig
}

// MetricsSignal holds metrics-specific knobs.
type MetricsSignal struct {
	SignalTransport
	Enabled     bool
	Interval    time.Duration
	Temporality string // "cumulative" | "delta"
}

// LogsSignal holds logs-specific knobs.
type LogsSignal struct {
	SignalTransport
	Enabled      bool
	BatchSize    int
	BatchTimeout time.Duration
	BufferSize   int // bounded queue; drop-oldest beyond this
}

// EntitiesSignal holds entity-event emission knobs. Entity events are
// carried on the OTLP log signal (they are log records), so this signal
// reuses the log exporter/transport — it has no endpoint/batch knobs of its
// own. Disabled by default: emitting entity events is a deliberate opt-in
// for an entity-aware backend, not something to switch on for every logs
// consumer.
type EntitiesSignal struct {
	Enabled bool
	// Interval is the heartbeat cadence: every entity/relation is re-emitted
	// each interval (at-least-once, idempotent) and the interval travels on
	// each event as the consumer's liveness backstop.
	Interval   time.Duration
	BufferSize int
	// DependsOnDebounce is how many consecutive scrapes a peer endpoint must
	// persist before its outbound depends_on edge is emitted — the line between
	// a durable dependency and ephemeral flow. The effective latency to surface
	// a dependency is DependsOnDebounce x Interval, so lowering it trades
	// durability for responsiveness. Must be >= 1.
	DependsOnDebounce int
	// DependsOnEnabled gates the outbound dependency source (hostdep). Off by
	// default — mapping a host's outbound connections can be privacy-sensitive,
	// so it is opt-in (#213).
	DependsOnEnabled bool
	// DependsOnExcludeCIDRs drops dependency flows whose peer address falls in
	// any of these ranges (operator privacy filter).
	DependsOnExcludeCIDRs []*net.IPNet
	// Governance is the operator metadata stamped on this host's entity
	// (owner/criticality/location/lifecycle/labels). Empty by default.
	Governance governance.Governance
}

// TracesSignal holds traces-specific knobs. Disabled by default — the
// agent does not auto-instrument itself yet; this block is plumbing
// for explicit span emission by future code or third-party libraries
// that resolve via otel.GetTracerProvider().
type TracesSignal struct {
	SignalTransport
	Enabled      bool
	BatchSize    int
	BatchTimeout time.Duration
	BufferSize   int
	// SampleRatio is the head sampling ratio (0.0 = drop all, 1.0 =
	// keep all). Applied via sdktrace.ParentBased(TraceIDRatioBased).
	SampleRatio float64
}

// ResourceConfig holds the OTel Resource attributes attached to every
// emitted record. service.name and service.instance.id default from the
// agent identity if not set.
type ResourceConfig struct {
	ServiceName     string
	ServiceInstance string
	Environment     string
	// Extra holds free-form additional resource attributes (any keys the
	// operator wants to attach beyond the well-known ones).
	Extra map[string]string
}

// Config is the fully-parsed, validated configuration for the OTLP strategy.
// Populated by ParseConfig; consumed by the strategy and exporter wiring.
type Config struct {
	Endpoint string
	// FallbackEndpoints are standby OTLP ingresses tried in order when the
	// primary Endpoint is failing (#217 resilience layer 2). Empty = no
	// failover (single endpoint). The agent prefers the primary and falls
	// back on a failed export, returning to the primary automatically once
	// it recovers (a per-endpoint cooldown avoids re-probing a dead one
	// every cycle).
	FallbackEndpoints []string
	// Protocol selects the OTLP transport: "grpc" (default) or "http",
	// the two transports defined by the OTel protocol spec. It applies
	// to all three signals — a per-signal override is not supported
	// because mixing transports against one endpoint is a
	// configuration mistake far more often than an intent.
	Protocol    string
	Headers     map[string]string
	TLS         TLSConfig
	Compression string
	Timeout     time.Duration
	Retry       RetryConfig
	Metrics     MetricsSignal
	Logs        LogsSignal
	Entities    EntitiesSignal
	Traces      TracesSignal
	Resource    ResourceConfig
	// MaxStoreSize bounds the OTLP strategy's in-memory metric store
	// (the last-writer-wins cache of every distinct series since the
	// last successful export). Once the store reaches this many series,
	// new series are dropped and `senhub.agent.otlp.dropped` is
	// incremented with `reason="store_cap"`. Existing series continue
	// to update normally — known series are preferred over admitting
	// unbounded new cardinality, which protects the agent from a probe
	// that goes rogue on a high-cardinality label (e.g. unique IDs as
	// tag values). 0 = unbounded (the historical default).
	MaxStoreSize int
	// MaxActiveSeriesPerProbe bounds the number of distinct series a
	// single probe instance (matched by probe_name) can register in the
	// store. When a probe hits its budget, new series are dropped with
	// `senhub.agent.otlp.dropped{reason="probe_cardinality"}` and
	// existing series keep updating. 0 = no per-probe budget; series
	// admission is then governed only by MaxStoreSize. Stacks on top
	// of MaxStoreSize — first whichever fires wins.
	MaxActiveSeriesPerProbe int
	// StalenessTTL evicts series that received no datapoint for this
	// long (see DefaultStalenessTTL). 0 disables eviction.
	StalenessTTL time.Duration
	// MemoryLimit defines the circuit-breaker thresholds for the OTLP
	// strategy's metric store. A background poller reads Go heap
	// pressure every CheckInterval; when soft is hit, new series are
	// dropped (existing keep updating); when hard is hit, all writes
	// are dropped and a GC is forced to try to recover. 0 disables
	// each threshold independently.
	MemoryLimit MemoryLimitConfig
	// Persistence configures the optional on-disk LWW checkpoint that
	// the strategy writes periodically and restores at boot. When
	// disabled (Persistence.Path == ""), the strategy behaves exactly
	// as before — store lives entirely in memory.
	Persistence PersistenceConfig
	// MaxConcurrentExports caps how many OTLP gRPC Export calls fire
	// in parallel during one push cycle. When > 1 (default 4) and
	// the resolved record count exceeds SplitBatchThreshold, the
	// strategy partitions records by probe_name and dispatches each
	// partition to its own goroutine sharing the single gRPC client
	// (HTTP/2 multiplexes streams over one connection). Set to 1 to
	// disable the parallel path entirely (back-compat single-batch
	// export). Lower values mean fewer in-flight goroutines but
	// longer cycles; higher values mean more parallelism but more
	// chance of overwhelming the receiver.
	MaxConcurrentExports int
}

// PersistenceConfig opts into the on-disk LWW checkpoint. The agent
// writes the metric store snapshot to Path/snapshot.json every
// Interval and restores it at boot. Survives routine restarts,
// upgrades, OOM kills, and OS reboots — at the cost of one disk
// write per Interval. Atomic write via .tmp + rename.
type PersistenceConfig struct {
	Path     string        // empty = disabled (no checkpoint)
	Interval time.Duration // save cadence; falls back to default if 0
	// LogsQueueMaxBytes caps the on-disk dead-letter queue for the logs
	// signal (#217). 0 = built-in default (128 MiB). Beyond it the oldest
	// batches are evicted (reason="logs_queue_full").
	LogsQueueMaxBytes int64
}

// MemoryLimitConfig configures the OTLP strategy's heap pressure
// circuit breaker. Modelled on the OTel Collector memory_limiter
// processor (limit_mib + spike_limit_mib), but applied per-strategy
// rather than agent-wide because the OTLP metric store is the only
// unbounded heap consumer in the data path.
type MemoryLimitConfig struct {
	SoftMiB       int           // 0 disables
	HardMiB       int           // 0 disables
	CheckInterval time.Duration // poll cadence; falls back to default if 0
}

// ResolveEndpoint returns the endpoint to use for this signal: the
// signal-specific override when set, else the root endpoint.
func (t SignalTransport) ResolveEndpoint(rootEndpoint string) string {
	if t.Endpoint != "" {
		return t.Endpoint
	}
	return rootEndpoint
}

// ResolveHeaders returns the headers to use for this signal. When the
// signal sets its own headers, they FULLY replace the root headers
// (matches OTel SDK env-var semantics). Returns rootHeaders otherwise.
func (t SignalTransport) ResolveHeaders(rootHeaders map[string]string) map[string]string {
	if t.Headers != nil {
		return t.Headers
	}
	return rootHeaders
}

// ResolveTLS returns the TLS config to use for this signal. When the
// signal has its own TLS block (non-nil pointer), it fully overrides
// the root TLS. Returns rootTLS otherwise.
func (t SignalTransport) ResolveTLS(rootTLS TLSConfig) TLSConfig {
	if t.TLS != nil {
		return *t.TLS
	}
	return rootTLS
}

// defaultConfig returns a Config with all defaults applied.
func defaultConfig() Config {
	return Config{
		Headers: map[string]string{},
		TLS: TLSConfig{
			Enabled: true,
		},
		Protocol:    DefaultProtocol,
		Compression: DefaultCompression,
		Timeout:     DefaultTimeout,
		Retry: RetryConfig{
			Enabled:         true,
			InitialInterval: DefaultRetryInitial,
			MaxInterval:     DefaultRetryMax,
			MaxElapsedTime:  DefaultRetryMaxElapsed,
		},
		Metrics: MetricsSignal{
			Enabled:     true,
			Interval:    DefaultMetricsInterval,
			Temporality: DefaultMetricsTemporality,
		},
		Logs: LogsSignal{
			Enabled:      true,
			BatchSize:    DefaultLogsBatchSize,
			BatchTimeout: DefaultLogsBatchTimeout,
			BufferSize:   DefaultLogsBufferSize,
		},
		Entities: EntitiesSignal{
			Enabled:           false,
			Interval:          DefaultEntitiesInterval,
			BufferSize:        DefaultEntitiesBufferSize,
			DependsOnDebounce: DefaultDependsOnDebounce,
		},
		Traces: TracesSignal{
			// Disabled by default — opt-in plumbing. Operators
			// enable explicitly when they want span export.
			Enabled:      false,
			BatchSize:    DefaultTracesBatchSize,
			BatchTimeout: DefaultTracesBatchTimeout,
			BufferSize:   DefaultTracesBufferSize,
			SampleRatio:  DefaultTracesSampleRatio,
		},
		Resource: ResourceConfig{
			ServiceName: DefaultServiceName,
			Extra:       map[string]string{},
		},
		MaxStoreSize:            DefaultMaxStoreSize,
		MaxActiveSeriesPerProbe: DefaultMaxActiveSeriesPerProbe,
		StalenessTTL:            DefaultStalenessTTL,
		MemoryLimit: MemoryLimitConfig{
			SoftMiB:       DefaultMemoryLimitSoftMiB,
			HardMiB:       DefaultMemoryLimitHardMiB,
			CheckInterval: DefaultMemoryLimitCheckPeriod,
		},
		Persistence: PersistenceConfig{
			// Path empty by default = disabled. Operators opt in by
			// setting otlp.persistence.path in YAML.
			Path:     "",
			Interval: DefaultPersistenceInterval,
		},
		MaxConcurrentExports: DefaultMaxConcurrentExports,
	}
}

// ParseConfig parses a StorageConfigParams map (raw YAML) into a typed
// Config and validates it. Returns the first error encountered.
func ParseConfig(params configuration.StorageConfigParams) (Config, error) {
	cfg := defaultConfig()

	if v, ok := params["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = expandEnv(v)
	}

	if raw, ok := params["fallback_endpoints"].([]interface{}); ok {
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				return cfg, fmt.Errorf("fallback_endpoints entries must be strings")
			}
			if s = expandEnv(strings.TrimSpace(s)); s != "" {
				cfg.FallbackEndpoints = append(cfg.FallbackEndpoints, s)
			}
		}
	}

	if v, ok := params["protocol"].(string); ok && v != "" {
		cfg.Protocol = v
	}
	// `http/protobuf` is the value the OTel spec env var
	// (OTEL_EXPORTER_OTLP_PROTOCOL) uses; accept it as an alias and
	// normalize to the file-config-ergonomic `http`. `http/json` is
	// not supported — the SDK exporters we wire emit protobuf.
	if cfg.Protocol == "http/protobuf" {
		cfg.Protocol = "http"
	}
	switch cfg.Protocol {
	case "grpc", "http":
	default:
		return cfg, fmt.Errorf("protocol must be 'grpc' or 'http' (alias 'http/protobuf'), got %q", cfg.Protocol)
	}

	if v, ok := params["compression"].(string); ok && v != "" {
		cfg.Compression = v
	}
	switch cfg.Compression {
	case "gzip", "none":
	default:
		return cfg, fmt.Errorf("compression must be 'gzip' or 'none', got %q", cfg.Compression)
	}

	if v, ok := params["timeout"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid timeout: %w", err)
		}
		cfg.Timeout = d
	}

	if v, ok := readInt(params["max_store_size"]); ok {
		if v < 0 {
			return cfg, fmt.Errorf("max_store_size must be >= 0 (0 = unbounded), got %d", v)
		}
		cfg.MaxStoreSize = v
	}

	if v, ok := readInt(params["max_active_series_per_probe"]); ok {
		if v < 0 {
			return cfg, fmt.Errorf("max_active_series_per_probe must be >= 0 (0 = unbounded), got %d", v)
		}
		cfg.MaxActiveSeriesPerProbe = v
	}

	if raw, ok := params["staleness_ttl"]; ok {
		str, isStr := raw.(string)
		if !isStr {
			return cfg, fmt.Errorf("staleness_ttl must be a duration string (e.g. \"10m\"), got %T", raw)
		}
		d, err := time.ParseDuration(str)
		if err != nil {
			return cfg, fmt.Errorf("staleness_ttl: %w", err)
		}
		if d < 0 {
			return cfg, fmt.Errorf("staleness_ttl must be >= 0 (0 disables eviction), got %s", d)
		}
		cfg.StalenessTTL = d
	}

	if err := parseMemoryLimit(params["memory_limit"], &cfg.MemoryLimit); err != nil {
		return cfg, fmt.Errorf("memory_limit: %w", err)
	}

	if err := parsePersistence(params["persistence"], &cfg.Persistence); err != nil {
		return cfg, fmt.Errorf("persistence: %w", err)
	}

	if v, ok := readInt(params["max_concurrent_exports"]); ok {
		if v < 1 {
			return cfg, fmt.Errorf("max_concurrent_exports must be >= 1 (1 = no parallelism), got %d", v)
		}
		if v > 64 {
			return cfg, fmt.Errorf("max_concurrent_exports must be <= 64 to bound goroutine fan-out, got %d", v)
		}
		cfg.MaxConcurrentExports = v
	}

	if hdrs := readStringMap(params["headers"]); hdrs != nil {
		// Expand ${env:VAR} references so bearer tokens / API keys can
		// be loaded from environment (systemd EnvironmentFile on Linux,
		// service env vars on Windows) instead of living in the config
		// file in plaintext. Same syntax as the OTel collector.
		cfg.Headers = expandEnvMap(hdrs)
	}

	if err := parseTLS(params["tls"], &cfg.TLS); err != nil {
		return cfg, fmt.Errorf("tls: %w", err)
	}
	if err := parseRetry(params["retry"], &cfg.Retry); err != nil {
		return cfg, fmt.Errorf("retry: %w", err)
	}
	if err := parseSignals(params["signals"], &cfg.Metrics, &cfg.Logs, &cfg.Traces, &cfg.Entities); err != nil {
		return cfg, fmt.Errorf("signals: %w", err)
	}
	if err := parseResource(params["resource"], &cfg.Resource); err != nil {
		return cfg, fmt.Errorf("resource: %w", err)
	}

	// Endpoint validation runs last so per-signal overrides can satisfy
	// the requirement when the root is omitted. The root endpoint is
	// only mandatory when at least one enabled signal lacks its own.
	if err := validateEndpoints(cfg); err != nil {
		return cfg, err
	}

	if err := validateAuthHeaders(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// validateAuthHeaders fails fast when an operator EXPLICITLY sets an
// Authorization header that resolves to a blank or scheme-only value.
// A configured-but-empty bearer token is a silent-data-loss trap: the
// operator intends an authenticated export, the credential resolves to
// "" (e.g. an unset ${env:VAR} with no default), and the receiver
// rejects every batch — indistinguishable from a healthy sink until
// someone notices the data never arrived. Authentication stays OPTIONAL
// for unauthenticated local OTLP: the check only fires when the header
// is present, so omitting it entirely is still valid.
//
// Each enabled signal is checked against its RESOLVED headers (its own
// override, else the inherited root) so a blank root header only errors
// when a live signal actually relies on it.
func validateAuthHeaders(cfg Config) error {
	signals := []struct {
		name    string
		enabled bool
		headers map[string]string
	}{
		{"signals.metrics", cfg.Metrics.Enabled, cfg.Metrics.ResolveHeaders(cfg.Headers)},
		{"signals.logs", cfg.Logs.Enabled, cfg.Logs.ResolveHeaders(cfg.Headers)},
		{"signals.traces", cfg.Traces.Enabled, cfg.Traces.ResolveHeaders(cfg.Headers)},
	}
	for _, s := range signals {
		if !s.enabled {
			continue
		}
		if err := checkAuthHeader(s.headers, s.name); err != nil {
			return err
		}
	}
	return nil
}

// checkAuthHeader rejects a present-but-blank Authorization header. The
// name is matched case-insensitively (HTTP header names are), and an
// empty value or a bare auth scheme with no credential after it (e.g.
// "Bearer" or "Bearer   ", which is what "Bearer ${env:UNSET}" resolves
// to) is rejected. A real token — with or without a scheme — passes.
func checkAuthHeader(headers map[string]string, where string) error {
	for k, v := range headers {
		if !strings.EqualFold(k, "authorization") {
			continue
		}
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return fmt.Errorf("%s: Authorization header is set but empty — provide a token or remove the header for unauthenticated export", where)
		}
		if fields := strings.Fields(trimmed); len(fields) == 1 {
			switch strings.ToLower(fields[0]) {
			case "bearer", "basic":
				return fmt.Errorf("%s: Authorization header has scheme %q but no credential — the token resolved to empty (check ${env:}/${secret:}/${file:} references)", where, fields[0])
			}
		}
	}
	return nil
}

// validateEndpoints ensures that every enabled signal has a reachable
// endpoint — either via its own SignalTransport.Endpoint or via the
// root cfg.Endpoint fallback.
func validateEndpoints(cfg Config) error {
	if cfg.Metrics.Enabled && cfg.Metrics.ResolveEndpoint(cfg.Endpoint) == "" {
		return fmt.Errorf("endpoint is required (root endpoint missing and signals.metrics.endpoint not set)")
	}
	if cfg.Logs.Enabled && cfg.Logs.ResolveEndpoint(cfg.Endpoint) == "" {
		return fmt.Errorf("endpoint is required (root endpoint missing and signals.logs.endpoint not set)")
	}
	if cfg.Traces.Enabled && cfg.Traces.ResolveEndpoint(cfg.Endpoint) == "" {
		return fmt.Errorf("endpoint is required (root endpoint missing and signals.traces.endpoint not set)")
	}
	return nil
}

func parseTLS(raw interface{}, out *TLSConfig) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	if v, ok := m["enabled"].(bool); ok {
		out.Enabled = v
	}
	if v, ok := m["insecure_skip_verify"].(bool); ok {
		out.InsecureSkipVerify = v
	}
	if v, ok := m["ca_file"].(string); ok {
		out.CAFile = v
	}
	if v, ok := m["cert_file"].(string); ok {
		out.CertFile = v
	}
	if v, ok := m["key_file"].(string); ok {
		out.KeyFile = v
	}
	if (out.CertFile == "") != (out.KeyFile == "") {
		return fmt.Errorf("cert_file and key_file must be set together (mTLS)")
	}
	return nil
}

func parseRetry(raw interface{}, out *RetryConfig) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	if v, ok := m["enabled"].(bool); ok {
		out.Enabled = v
	}
	if v, ok := m["initial_interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("initial_interval: %w", err)
		}
		out.InitialInterval = d
	}
	if v, ok := m["max_interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("max_interval: %w", err)
		}
		out.MaxInterval = d
	}
	if v, ok := m["max_elapsed_time"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("max_elapsed_time: %w", err)
		}
		out.MaxElapsedTime = d
	}
	return nil
}

func parsePersistence(raw interface{}, out *PersistenceConfig) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	// The "enabled" key is convenience sugar: enabled:false clears the
	// path even if set, enabled:true requires a path to be meaningful.
	enabled := true
	if v, ok := m["enabled"].(bool); ok {
		enabled = v
	}
	if v, ok := m["path"].(string); ok {
		out.Path = v
	}
	if !enabled {
		out.Path = ""
	}
	if v, ok := m["interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("interval: %w", err)
		}
		if d < time.Second {
			return fmt.Errorf("interval must be >= 1s, got %v", d)
		}
		out.Interval = d
	}
	if v, ok := readInt(m["logs_queue_max_bytes"]); ok {
		if v < 0 {
			return fmt.Errorf("logs_queue_max_bytes must be >= 0, got %d", v)
		}
		out.LogsQueueMaxBytes = int64(v)
	}
	return nil
}

func parseMemoryLimit(raw interface{}, out *MemoryLimitConfig) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	if v, ok := readInt(m["soft_mib"]); ok {
		if v < 0 {
			return fmt.Errorf("soft_mib must be >= 0 (0 = disabled), got %d", v)
		}
		out.SoftMiB = v
	}
	if v, ok := readInt(m["hard_mib"]); ok {
		if v < 0 {
			return fmt.Errorf("hard_mib must be >= 0 (0 = disabled), got %d", v)
		}
		out.HardMiB = v
	}
	if v, ok := m["check_interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("check_interval: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("check_interval must be > 0, got %v", d)
		}
		out.CheckInterval = d
	}
	if out.SoftMiB > 0 && out.HardMiB > 0 && out.SoftMiB >= out.HardMiB {
		return fmt.Errorf("soft_mib (%d) must be < hard_mib (%d)", out.SoftMiB, out.HardMiB)
	}
	return nil
}

func parseSignals(raw interface{}, metrics *MetricsSignal, logs *LogsSignal, traces *TracesSignal, entities *EntitiesSignal) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}

	if mm := readStringKeyedMap(m["metrics"]); mm != nil {
		if v, ok := mm["enabled"].(bool); ok {
			metrics.Enabled = v
		}
		if v, ok := mm["interval"].(string); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("metrics.interval: %w", err)
			}
			metrics.Interval = d
		}
		if v, ok := mm["temporality"].(string); ok && v != "" {
			switch v {
			case "cumulative", "delta":
				metrics.Temporality = v
			default:
				return fmt.Errorf("metrics.temporality must be 'cumulative' or 'delta', got %q", v)
			}
		}
		if err := parseSignalTransport("metrics", mm, &metrics.SignalTransport); err != nil {
			return err
		}
	}

	if lm := readStringKeyedMap(m["logs"]); lm != nil {
		if v, ok := lm["enabled"].(bool); ok {
			logs.Enabled = v
		}
		if v, ok := readInt(lm["batch_size"]); ok {
			logs.BatchSize = v
		}
		if v, ok := lm["batch_timeout"].(string); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("logs.batch_timeout: %w", err)
			}
			logs.BatchTimeout = d
		}
		if v, ok := readInt(lm["buffer_size"]); ok {
			logs.BufferSize = v
		}
		if err := parseSignalTransport("logs", lm, &logs.SignalTransport); err != nil {
			return err
		}
	}

	if em := readStringKeyedMap(m["entities"]); em != nil {
		if v, ok := em["enabled"].(bool); ok {
			entities.Enabled = v
		}
		if v, ok := em["interval"].(string); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("entities.interval: %w", err)
			}
			entities.Interval = d
		}
		if v, ok := readInt(em["buffer_size"]); ok {
			entities.BufferSize = v
		}
		if v, ok := readInt(em["depends_on_debounce"]); ok {
			if v < 1 {
				return fmt.Errorf("entities.depends_on_debounce must be >= 1, got %d", v)
			}
			entities.DependsOnDebounce = v
		}
		if v, ok := em["depends_on_enabled"].(bool); ok {
			entities.DependsOnEnabled = v
		}
		if raw, ok := em["depends_on_exclude_cidrs"]; ok {
			cidrs, err := parseCIDRStrings(raw)
			if err != nil {
				return fmt.Errorf("entities.depends_on_exclude_cidrs: %w", err)
			}
			entities.DependsOnExcludeCIDRs = cidrs
		}
		if gov, err := governance.Parse(em["governance"]); err != nil {
			return fmt.Errorf("entities.governance: %w", err)
		} else {
			entities.Governance = gov
		}
	}

	if tm := readStringKeyedMap(m["traces"]); tm != nil {
		if v, ok := tm["enabled"].(bool); ok {
			traces.Enabled = v
		}
		if v, ok := readInt(tm["batch_size"]); ok {
			traces.BatchSize = v
		}
		if v, ok := tm["batch_timeout"].(string); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("traces.batch_timeout: %w", err)
			}
			traces.BatchTimeout = d
		}
		if v, ok := readInt(tm["buffer_size"]); ok {
			traces.BufferSize = v
		}
		if v, ok := readFloat(tm["sample_ratio"]); ok {
			if v < 0 || v > 1 {
				return fmt.Errorf("traces.sample_ratio must be between 0.0 and 1.0, got %v", v)
			}
			traces.SampleRatio = v
		}
		if err := parseSignalTransport("traces", tm, &traces.SignalTransport); err != nil {
			return err
		}
	}
	return nil
}

// parseSignalTransport reads the optional endpoint/headers/tls fields
// inside a signals.<name> block and assigns them to the SignalTransport.
// signalName is used purely for error context.
func parseSignalTransport(signalName string, m map[string]interface{}, out *SignalTransport) error {
	if v, ok := m["endpoint"].(string); ok && v != "" {
		out.Endpoint = expandEnv(v)
	}
	if hdrs := readStringMap(m["headers"]); hdrs != nil {
		out.Headers = expandEnvMap(hdrs)
	}
	if tlsRaw, present := m["tls"]; present && tlsRaw != nil {
		// Start from the same defaults as the root TLS (Enabled=true)
		// so an operator who writes `tls: {}` at the signal level gets
		// TLS-on rather than accidentally falling back to plaintext.
		tlsCfg := TLSConfig{Enabled: true}
		if err := parseTLS(tlsRaw, &tlsCfg); err != nil {
			return fmt.Errorf("%s.tls: %w", signalName, err)
		}
		out.TLS = &tlsCfg
	}
	return nil
}

// readFloat accepts int / int64 / float64 (Go-native or JSON decodes).
func readFloat(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func parseResource(raw interface{}, out *ResourceConfig) error {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("resource.%s must be a string", k)
		}
		s = expandEnv(s)
		switch k {
		case "service.name":
			out.ServiceName = s
		case "service.instance.id":
			out.ServiceInstance = s
		case "deployment.environment":
			out.Environment = s
		default:
			out.Extra[k] = s
		}
	}
	return nil
}

// readStringKeyedMap accepts both map[string]interface{} (the JSON case)
// and map[interface{}]interface{} (the YAML-with-string-keys case) and
// normalizes to the former.
func readStringKeyedMap(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			if ks, ok := k.(string); ok {
				out[ks] = val
			}
		}
		return out
	}
	return nil
}

// readStringMap converts a map[string|interface{}]interface{} into a
// flat map[string]string, dropping any non-string values.
func readStringMap(raw interface{}) map[string]string {
	m := readStringKeyedMap(raw)
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// readInt accepts both int (Go-native YAML decode) and float64 (JSON
// decode default) and returns the value as int.
func readInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// parseCIDRStrings parses a YAML list of CIDR strings into IPNets, rejecting a
// non-list or a malformed entry.
func parseCIDRStrings(raw interface{}) ([]*net.IPNet, error) {
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be a list of CIDR strings")
	}
	out := make([]*net.IPNet, 0, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entry %d is not a string", i)
		}
		_, n, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("entry %d %q: %w", i, s, err)
		}
		out = append(out, n)
	}
	return out, nil
}
