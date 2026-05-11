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
	DefaultCompression        = "gzip"
	DefaultTimeout            = 10 * time.Second
	DefaultMetricsInterval    = 30 * time.Second
	DefaultMetricsTemporality = "cumulative"
	DefaultLogsBatchSize      = 1000
	DefaultLogsBatchTimeout   = 5 * time.Second
	DefaultLogsBufferSize     = 10000
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

// MetricsSignal holds metrics-specific knobs.
type MetricsSignal struct {
	Enabled     bool
	Interval    time.Duration
	Temporality string // "cumulative" | "delta"
}

// LogsSignal holds logs-specific knobs.
type LogsSignal struct {
	Enabled      bool
	BatchSize    int
	BatchTimeout time.Duration
	BufferSize   int // bounded queue; drop-oldest beyond this
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
	Resource    ResourceConfig
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
		Resource: ResourceConfig{
			ServiceName: DefaultServiceName,
			Extra:       map[string]string{},
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
	if cfg.Endpoint == "" {
		return cfg, fmt.Errorf("endpoint is required")
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
	if err := parseSignals(params["signals"], &cfg.Metrics, &cfg.Logs); err != nil {
		return cfg, fmt.Errorf("signals: %w", err)
	}
	if err := parseResource(params["resource"], &cfg.Resource); err != nil {
		return cfg, fmt.Errorf("resource: %w", err)
	}

	return cfg, nil
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

func parseSignals(raw interface{}, metrics *MetricsSignal, logs *LogsSignal) error {
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
	}
	return nil
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
