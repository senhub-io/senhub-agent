package otlp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// exporters bundles the SDK exporters created from a Config. Each field
// is an interface type so the same struct holds either the gRPC or the
// HTTP exporter, selected by Config.Protocol:
//
//   - metric: otlpmetricgrpc.Exporter | otlpmetrichttp.Exporter
//   - log:    otlploggrpc.Exporter   | otlploghttp.Exporter
//   - trace:  *otlptrace.Exporter wrapping a grpc or http otlptrace.Client
//
// They are constructed lazily — the transport dial does NOT happen at
// construction time; the first export call is what actually opens the
// socket. buildExporters therefore never blocks on network and never
// returns a connection error — that surfaces on the first export
// attempt and is handled by the retry policy.
type exporters struct {
	metric sdkmetric.Exporter
	log    sdklog.Exporter
	trace  *otlptrace.Exporter
}

// buildExporters constructs the OTel SDK exporters based on cfg.
// Any of the three can be nil if its signal is disabled.
func buildExporters(ctx context.Context, cfg Config) (*exporters, error) {
	exp := &exporters{}

	if cfg.Metrics.Enabled {
		me, err := buildMetricExporter(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("metric exporter: %w", err)
		}
		exp.metric = me
	}

	if cfg.Logs.Enabled {
		le, err := buildLogExporter(ctx, cfg)
		if err != nil {
			// Roll back metric exporter on log exporter failure so we
			// don't leak a half-built bundle on the caller.
			if exp.metric != nil {
				_ = exp.metric.Shutdown(ctx)
			}
			return nil, fmt.Errorf("log exporter: %w", err)
		}
		exp.log = le
	}

	if cfg.Traces.Enabled {
		te, err := buildTraceExporter(ctx, cfg)
		if err != nil {
			if exp.metric != nil {
				_ = exp.metric.Shutdown(ctx)
			}
			if exp.log != nil {
				_ = exp.log.Shutdown(ctx)
			}
			return nil, fmt.Errorf("trace exporter: %w", err)
		}
		exp.trace = te
	}

	return exp, nil
}

// resolvedTransport bundles the effective per-signal transport values
// (endpoint/headers/TLS) after applying root → signal override
// resolution. Centralizing the resolution here keeps the three signal
// builders symmetric.
type resolvedTransport struct {
	endpoint string
	headers  map[string]string
	tls      TLSConfig
}

func resolveTransport(cfg Config, sig SignalTransport) resolvedTransport {
	return resolvedTransport{
		endpoint: sig.ResolveEndpoint(cfg.Endpoint),
		headers:  sig.ResolveHeaders(cfg.Headers),
		tls:      sig.ResolveTLS(cfg.TLS),
	}
}

// ── Metrics ──────────────────────────────────────────────────────────

func buildMetricExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	if cfg.Protocol == "http" {
		return buildMetricExporterHTTP(ctx, cfg)
	}
	return buildMetricExporterGRPC(ctx, cfg)
}

func buildMetricExporterGRPC(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Metrics.SignalTransport)
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(rt.endpoint),
		otlpmetricgrpc.WithTimeout(cfg.Timeout),
	}
	creds, insec, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(creds))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlpmetricgrpc.WithCompressor("gzip"))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: false}))
	}
	return otlpmetricgrpc.New(ctx, opts...)
}

func buildMetricExporterHTTP(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Metrics.SignalTransport)
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(rt.endpoint),
		otlpmetrichttp.WithTimeout(cfg.Timeout),
	}
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	} else {
		opts = append(opts, otlpmetrichttp.WithTLSClientConfig(tlsConf))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}))
	}
	return otlpmetrichttp.New(ctx, opts...)
}

// ── Logs ─────────────────────────────────────────────────────────────

func buildLogExporter(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	if cfg.Protocol == "http" {
		return buildLogExporterHTTP(ctx, cfg)
	}
	return buildLogExporterGRPC(ctx, cfg)
}

func buildLogExporterGRPC(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Logs.SignalTransport)
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(rt.endpoint),
		otlploggrpc.WithTimeout(cfg.Timeout),
	}
	creds, insec, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlploggrpc.WithInsecure())
	} else {
		opts = append(opts, otlploggrpc.WithTLSCredentials(creds))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlploggrpc.WithCompressor("gzip"))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlploggrpc.WithRetry(otlploggrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlploggrpc.WithRetry(otlploggrpc.RetryConfig{Enabled: false}))
	}
	return otlploggrpc.New(ctx, opts...)
}

func buildLogExporterHTTP(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Logs.SignalTransport)
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(rt.endpoint),
		otlploghttp.WithTimeout(cfg.Timeout),
	}
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlploghttp.WithInsecure())
	} else {
		opts = append(opts, otlploghttp.WithTLSClientConfig(tlsConf))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlploghttp.WithCompression(otlploghttp.GzipCompression))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlploghttp.WithRetry(otlploghttp.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
	}
	return otlploghttp.New(ctx, opts...)
}

// ── Traces ───────────────────────────────────────────────────────────

func buildTraceExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	if cfg.Protocol == "http" {
		return buildTraceExporterHTTP(ctx, cfg)
	}
	return buildTraceExporterGRPC(ctx, cfg)
}

func buildTraceExporterGRPC(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Traces.SignalTransport)
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(rt.endpoint),
		otlptracegrpc.WithTimeout(cfg.Timeout),
	}
	creds, insec, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(creds))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlptracegrpc.WithCompressor("gzip"))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}))
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

func buildTraceExporterHTTP(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	rt := resolveTransport(cfg, cfg.Traces.SignalTransport)
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(rt.endpoint),
		otlptracehttp.WithTimeout(cfg.Timeout),
	}
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlptracehttp.WithInsecure())
	} else {
		opts = append(opts, otlptracehttp.WithTLSClientConfig(tlsConf))
	}
	if len(rt.headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(rt.headers))
	}
	if cfg.Compression == "gzip" {
		opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	if cfg.Retry.Enabled {
		opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	} else {
		opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	}
	return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
}

// ── TLS ──────────────────────────────────────────────────────────────

// buildTLSConfig builds the *tls.Config for a signal. The bool result
// is true when the operator explicitly disabled TLS (plaintext mode);
// in that case the *tls.Config is nil and the caller must use the
// transport's WithInsecure() option instead.
//
// The OTLP/HTTP exporters consume the *tls.Config directly via
// WithTLSClientConfig; the gRPC exporters wrap it in transport
// credentials — see tlsCredentials.
func buildTLSConfig(tlsCfg TLSConfig) (*tls.Config, bool, error) {
	if !tlsCfg.Enabled {
		return nil, true, nil
	}

	conf := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: tlsCfg.InsecureSkipVerify, // #nosec G402 - operator-controlled, off by default
	}

	if tlsCfg.CAFile != "" {
		caPEM, err := os.ReadFile(tlsCfg.CAFile)
		if err != nil {
			return nil, false, fmt.Errorf("read ca_file %q: %w", tlsCfg.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, false, fmt.Errorf("ca_file %q: no certificates parsed", tlsCfg.CAFile)
		}
		conf.RootCAs = pool
	}

	if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			return nil, false, fmt.Errorf("load mTLS keypair: %w", err)
		}
		conf.Certificates = []tls.Certificate{cert}
	}

	return conf, false, nil
}

// tlsCredentials returns gRPC transport credentials. The bool result is
// true when the operator explicitly disabled TLS (plaintext mode); in
// that case the credentials value is ignored and the caller must use
// WithInsecure() instead.
func tlsCredentials(tlsCfg TLSConfig) (credentials.TransportCredentials, bool, error) {
	conf, insec, err := buildTLSConfig(tlsCfg)
	if err != nil {
		return nil, false, err
	}
	if insec {
		return insecure.NewCredentials(), true, nil
	}
	return credentials.NewTLS(conf), false, nil
}

// shutdown closes every exporter with the provided context. Errors from
// each are accumulated; we never short-circuit so that one signal's
// failure does not prevent the others from being torn down.
func (e *exporters) shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	var errs []error
	if e.metric != nil {
		if err := e.metric.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("metric exporter shutdown: %w", err))
		}
	}
	if e.log != nil {
		if err := e.log.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("log exporter shutdown: %w", err))
		}
	}
	if e.trace != nil {
		if err := e.trace.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("trace exporter shutdown: %w", err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("multiple shutdown errors: %v", errs)
}
