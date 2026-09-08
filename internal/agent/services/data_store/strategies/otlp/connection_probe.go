package otlp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

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

	var cfg Config
	if !run("config", func() (string, error) {
		var err error
		cfg, err = ParseConfig(params)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s over %s", cfg.Endpoint, cfg.Protocol), nil
	}) {
		return steps
	}

	host, port, err := net.SplitHostPort(cfg.Endpoint)
	if err != nil {
		steps = append(steps, ConnectionStep{Name: "dns", Error: fmt.Sprintf("endpoint must be host:port, got %q", cfg.Endpoint)})
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

	if cfg.TLS.Enabled {
		ok := run("tls", func() (string, error) {
			tlsConf, _, err := buildTLSConfig(cfg.TLS)
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
		exp, err := buildMetricExporter(ctx, cfg)
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
