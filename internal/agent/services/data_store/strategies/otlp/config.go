// Package otlp implements the OTLP/gRPC export strategy for SenHub Agent.
//
// The strategy ships metrics (sourced from the same MetricCache as the
// Prometheus exposition, resolved through the neutral otelmapper package)
// and logs (sourced from a pub/sub log channel populated by syslog and
// event probes) over OTLP/gRPC to an OTel collector or a compatible
// backend (vmagent, victoria-metrics, otelcol-contrib, …).
//
// Phase 1 (this commit) only wires up configuration parsing, the gRPC
// exporter clients, and the strategy lifecycle. Metrics export lands in
// Phase 2; logs export in Phase 3.
package otlp

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
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
	Endpoint    string
	Headers     map[string]string
	TLS         TLSConfig
	Compression string
	Timeout     time.Duration
	Retry       RetryConfig
	Metrics     MetricsSignal
	Logs        LogsSignal
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
	// MemoryLimit defines the circuit-breaker thresholds for the OTLP
	// strategy's metric store. A background poller reads Go heap
	// pressure every CheckInterval; when soft is hit, new series are
	// dropped (existing keep updating); when hard is hit, all writes
	// are dropped and a GC is forced to try to recover. 0 disables
	// each threshold independently.
	MemoryLimit MemoryLimitConfig
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
		MemoryLimit: MemoryLimitConfig{
			SoftMiB:       DefaultMemoryLimitSoftMiB,
			HardMiB:       DefaultMemoryLimitHardMiB,
			CheckInterval: DefaultMemoryLimitCheckPeriod,
		},
	}
}

// ParseConfig parses a StorageConfigParams map (raw YAML) into a typed
// Config and validates it. Returns the first error encountered.
func ParseConfig(params configuration.StorageConfigParams) (Config, error) {
	cfg := defaultConfig()

	if v, ok := params["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = expandEnv(v)
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

	if err := parseMemoryLimit(params["memory_limit"], &cfg.MemoryLimit); err != nil {
		return cfg, fmt.Errorf("memory_limit: %w", err)
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
	if err := parseSignals(params["signals"], &cfg.Metrics, &cfg.Logs, &cfg.Traces); err != nil {
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

	return cfg, nil
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

func parseSignals(raw interface{}, metrics *MetricsSignal, logs *LogsSignal, traces *TracesSignal) error {
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
