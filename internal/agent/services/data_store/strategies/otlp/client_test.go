package otlp

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

// buildExporters dials lazily — constructing the exporters never opens
// a socket — so these tests can build against an unreachable endpoint
// and still assert the exporter objects come back non-nil. They guard
// the grpc⇄http transport selection added for the VM/VL/VT native
// ingestion path.

func testConfigForProtocol(proto string) Config {
	cfg := defaultConfig()
	cfg.Endpoint = "otlp.example.invalid:4318"
	cfg.Protocol = proto
	// Plaintext so the builder doesn't try to load a CA off disk.
	cfg.TLS.Enabled = false
	cfg.Metrics.Enabled = true
	cfg.Logs.Enabled = true
	cfg.Traces.Enabled = true
	return cfg
}

func TestBuildExporters_GRPC(t *testing.T) {
	exp, err := buildExporters(context.Background(), testConfigForProtocol("grpc"), testModuleLogger(t))
	if err != nil {
		t.Fatalf("buildExporters(grpc) error: %v", err)
	}
	defer func() { _ = exp.shutdown(context.Background()) }()

	if exp.metric == nil || exp.log == nil || exp.trace == nil {
		t.Errorf("grpc: expected all three exporters non-nil, got metric=%v log=%v trace=%v",
			exp.metric != nil, exp.log != nil, exp.trace != nil)
	}
}

func TestBuildExporters_HTTP(t *testing.T) {
	exp, err := buildExporters(context.Background(), testConfigForProtocol("http"), testModuleLogger(t))
	if err != nil {
		t.Fatalf("buildExporters(http) error: %v", err)
	}
	defer func() { _ = exp.shutdown(context.Background()) }()

	if exp.metric == nil || exp.log == nil || exp.trace == nil {
		t.Errorf("http: expected all three exporters non-nil, got metric=%v log=%v trace=%v",
			exp.metric != nil, exp.log != nil, exp.trace != nil)
	}
}

// TestBuildExporters_HTTPWithTLS exercises the http TLS path (it goes
// through buildTLSConfig → WithTLSClientConfig, distinct from the grpc
// WithTLSCredentials path).
func TestBuildExporters_HTTPWithTLS(t *testing.T) {
	cfg := testConfigForProtocol("http")
	cfg.TLS.Enabled = true
	cfg.TLS.InsecureSkipVerify = true // no CA file needed

	exp, err := buildExporters(context.Background(), cfg, testModuleLogger(t))
	if err != nil {
		t.Fatalf("buildExporters(http+tls) error: %v", err)
	}
	defer func() { _ = exp.shutdown(context.Background()) }()

	if exp.metric == nil || exp.log == nil || exp.trace == nil {
		t.Error("http+tls: expected all three exporters non-nil")
	}
}

// TestBuildExporters_DisabledSignals confirms a disabled signal yields
// a nil exporter rather than a built one, on both transports.
func TestBuildExporters_DisabledSignals(t *testing.T) {
	for _, proto := range []string{"grpc", "http"} {
		t.Run(proto, func(t *testing.T) {
			cfg := testConfigForProtocol(proto)
			cfg.Logs.Enabled = false
			cfg.Traces.Enabled = false

			exp, err := buildExporters(context.Background(), cfg, testModuleLogger(t))
			if err != nil {
				t.Fatalf("buildExporters error: %v", err)
			}
			defer func() { _ = exp.shutdown(context.Background()) }()

			if exp.metric == nil {
				t.Error("metric exporter should be built (signal enabled)")
			}
			if exp.log != nil {
				t.Error("log exporter should be nil (signal disabled)")
			}
			if exp.trace != nil {
				t.Error("trace exporter should be nil (signal disabled)")
			}
		})
	}
}

// TestNewHTTPClient_KeepsStandardTransportBehaviour is the guard on the
// footgun of #829: WithHTTPClient takes precedence over WithTLSClientConfig,
// WithTimeout and WithProxy, so a hand-rolled client silently drops
// whatever it forgets. Cloning http.DefaultTransport keeps proxy support
// and the standard dial/handshake timeouts; only the two fields we mean
// to change may differ.
func TestNewHTTPClient_KeepsStandardTransportBehaviour(t *testing.T) {
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "ingest.example.com"}
	client := newHTTPClient(tlsConf, 60*time.Second, 45*time.Second)

	if client.Timeout != 60*time.Second {
		t.Errorf("client timeout=%s, want 60s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T, want *http.Transport", client.Transport)
	}
	if transport.IdleConnTimeout != 45*time.Second {
		t.Errorf("IdleConnTimeout=%s, want 45s", transport.IdleConnTimeout)
	}
	if transport.TLSClientConfig != tlsConf {
		t.Error("TLS configuration was not carried onto the custom transport")
	}
	if transport.Proxy == nil {
		t.Error("proxy support dropped: HTTP_PROXY / HTTPS_PROXY would be ignored")
	}

	std := http.DefaultTransport.(*http.Transport)
	if transport.TLSHandshakeTimeout != std.TLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout=%s, want the standard %s", transport.TLSHandshakeTimeout, std.TLSHandshakeTimeout)
	}
	if transport.MaxIdleConns != std.MaxIdleConns {
		t.Errorf("MaxIdleConns=%d, want the standard %d", transport.MaxIdleConns, std.MaxIdleConns)
	}
	if transport.ExpectContinueTimeout != std.ExpectContinueTimeout {
		t.Errorf("ExpectContinueTimeout=%s, want the standard %s", transport.ExpectContinueTimeout, std.ExpectContinueTimeout)
	}
}
