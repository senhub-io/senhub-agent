package otlp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"

	"senhub-agent.go/internal/agent/services/configuration"
)

// ConnectionStep is one stage of a connection test, in the order an
// operator debugs a dead sink: name resolution, TCP, TLS, then one real
// export the receiver has to accept.
type ConnectionStep struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"duration_ms"`
}

// Dialer opens the TCP connection of the first steps. The caller
// supplies one that refuses addresses the agent must not reach.
type Dialer func(ctx context.Context, network, address string) (net.Conn, error)

// ProbeConnection runs the steps against the params as the operator
// typed them, without saving anything. It stops at the first failure;
// the steps run so far are returned with the failing one last.
func ProbeConnection(ctx context.Context, params configuration.StorageConfigParams, dial Dialer) []ConnectionStep {
	var steps []ConnectionStep
	run := func(name string, fn func() (string, error)) bool {
		start := time.Now()
		detail, err := fn()
		step := ConnectionStep{Name: name, Passed: err == nil, Detail: detail, Duration: time.Since(start).Milliseconds()}
		if err != nil {
			step.Error = redactSensitive(err.Error())
		}
		steps = append(steps, step)
		return err == nil
	}

	// The test reaches one address, the one the metrics signal exports
	// to, through the caller's dialer at every step: a signal endpoint
	// override or a fallback must not open a connection the guard has
	// not seen.
	var cfg Config
	var rt resolvedTransport
	if !run("config", func() (string, error) {
		var err error
		cfg, err = ParseConfig(params)
		if err != nil {
			return "", err
		}
		cfg.FallbackEndpoints = nil
		rt = resolveTransport(cfg, cfg.Metrics.SignalTransport)
		return fmt.Sprintf("%s over %s", rt.endpoint, cfg.Protocol), nil
	}) {
		return steps
	}

	host, port, err := net.SplitHostPort(rt.endpoint)
	if err != nil {
		steps = append(steps, ConnectionStep{Name: "dns", Error: fmt.Sprintf("endpoint must be host:port, got %q", rt.endpoint)})
		return steps
	}
	var addrs []net.IPAddr
	if !run("dns", func() (string, error) {
		var err error
		addrs, err = net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return "", err
		}
		names := make([]string, 0, len(addrs))
		for _, a := range addrs {
			names = append(names, a.IP.String())
		}
		return strings.Join(names, ", "), nil
	}) {
		return steps
	}

	var conn net.Conn
	if !run("tcp", func() (string, error) {
		var err error
		conn, err = dial(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			return "", err
		}
		return conn.RemoteAddr().String(), nil
	}) {
		return steps
	}

	if rt.tls.Enabled {
		ok := run("tls", func() (string, error) {
			tlsConf, _, err := buildTLSConfig(rt.tls)
			if err != nil {
				return "", err
			}
			tlsConf = tlsConf.Clone()
			tlsConf.ServerName = host
			tc := tls.Client(conn, tlsConf)
			if deadline, has := ctx.Deadline(); has {
				_ = tc.SetDeadline(deadline)
			}
			if err := tc.HandshakeContext(ctx); err != nil {
				return "", err
			}
			state := tc.ConnectionState()
			detail := tlsVersionName(state.Version)
			if len(state.PeerCertificates) > 0 {
				leaf := state.PeerCertificates[0]
				detail += fmt.Sprintf(", %s, expires %s", leaf.Subject.CommonName, leaf.NotAfter.Format("2006-01-02"))
			}
			_ = tc.Close()
			return detail, nil
		})
		if !ok {
			_ = conn.Close()
			return steps
		}
	} else {
		_ = conn.Close()
	}

	run("export", func() (string, error) {
		exp, err := testMetricExporter(ctx, cfg, rt, dial)
		if err != nil {
			return "", err
		}
		defer func() { _ = exp.Shutdown(context.Background()) }()
		now := time.Now()
		rm := &metricdata.ResourceMetrics{
			Resource: resource.NewSchemaless(attribute.String(resourceKeyServiceName, cfg.Resource.ServiceName)),
			ScopeMetrics: []metricdata.ScopeMetrics{{
				Scope: instrumentation.Scope{Name: scopeName},
				Metrics: []metricdata.Metrics{{
					Name:        "senhub.agent.console.connection_test",
					Description: "One data point sent by the console to check that the receiver accepts an export",
					Data: metricdata.Gauge[float64]{DataPoints: []metricdata.DataPoint[float64]{{
						Time: now, Value: 1,
					}}},
				}},
			}},
		}
		if err := exp.Export(ctx, rm); err != nil {
			return "", err
		}
		return "one metric accepted", nil
	})
	return steps
}

// testMetricExporter builds the metrics exporter the way the strategy
// does, except that every connection goes through the caller's dialer.
func testMetricExporter(ctx context.Context, cfg Config, rt resolvedTransport, dial Dialer) (sdkmetric.Exporter, error) {
	if cfg.Protocol == "http" {
		tlsConf, insec, err := buildTLSConfig(rt.tls)
		if err != nil {
			return nil, err
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConf
		transport.DialContext = dial
		transport.Proxy = nil
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(rt.endpoint),
			otlpmetrichttp.WithTimeout(cfg.Timeout),
			otlpmetrichttp.WithHTTPClient(&http.Client{Transport: transport, Timeout: cfg.Timeout}),
			otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
		}
		if insec {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if cfg.URLPathPrefix != "" {
			opts = append(opts, otlpmetrichttp.WithURLPath(cfg.URLPathPrefix+signalPathMetrics))
		}
		if len(rt.headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(rt.headers))
		}
		if cfg.Compression == "gzip" {
			opts = append(opts, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
		}
		return otlpmetrichttp.New(ctx, opts...)
	}
	creds, insec, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, err
	}
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(rt.endpoint),
		otlpmetricgrpc.WithTimeout(cfg.Timeout),
		otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: false}),
		otlpmetricgrpc.WithDialOption(grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dial(ctx, "tcp", addr)
		})),
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
	return otlpmetricgrpc.New(ctx, opts...)
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("TLS 0x%04x", v)
	}
}
