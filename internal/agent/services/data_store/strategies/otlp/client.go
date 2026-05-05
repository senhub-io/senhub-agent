package otlp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// exporters bundles the SDK exporters created from a Config. They are
// constructed lazily — the gRPC dial does NOT happen at construction
// time (the SDK uses lazy connection); the first export call is what
// actually opens the TCP socket.
//
// This means buildExporters never blocks on network and never returns a
// connection error — that gets surfaced on the first export attempt and
// is handled by the retry policy.
type exporters struct {
	metric *otlpmetricgrpc.Exporter
	log    *otlploggrpc.Exporter
}

// buildExporters constructs the OTel SDK exporters based on cfg.
// Either or both can be nil if the corresponding signal is disabled.
//
// All gRPC dialing is lazy — the exporters do not open a connection
// until their first Export call. This is intentional: it lets the agent
// start cleanly even when the OTLP collector is unreachable, with
// failures surfacing as retried export attempts rather than blocking
// the whole startup sequence.
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

	return exp, nil
}

func buildMetricExporter(ctx context.Context, cfg Config) (*otlpmetricgrpc.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithTimeout(cfg.Timeout),
	}
	creds, insec, err := tlsCredentials(cfg.TLS)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(creds))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
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

func buildLogExporter(ctx context.Context, cfg Config) (*otlploggrpc.Exporter, error) {
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(cfg.Endpoint),
		otlploggrpc.WithTimeout(cfg.Timeout),
	}
	creds, insec, err := tlsCredentials(cfg.TLS)
	if err != nil {
		return nil, err
	}
	if insec {
		opts = append(opts, otlploggrpc.WithInsecure())
	} else {
		opts = append(opts, otlploggrpc.WithTLSCredentials(creds))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(cfg.Headers))
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

// tlsCredentials returns gRPC transport credentials. The bool result is
// true when the operator explicitly disabled TLS (plaintext mode); in
// that case the credentials value is ignored and the caller must use
// WithInsecure() instead. Returning the bool out-of-band rather than a
// nil credentials value lets the caller distinguish "no TLS" from "TLS
// build failed but we masked it as nil".
func tlsCredentials(tlsCfg TLSConfig) (credentials.TransportCredentials, bool, error) {
	if !tlsCfg.Enabled {
		return insecure.NewCredentials(), true, nil
	}

	conf := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: tlsCfg.InsecureSkipVerify,
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

	return credentials.NewTLS(conf), false, nil
}

// shutdown closes both exporters with the provided context. Errors from
// each exporter are accumulated; we never short-circuit so that one
// signal's failure does not prevent the other from being torn down.
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
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("multiple shutdown errors: %v", errs)
}
