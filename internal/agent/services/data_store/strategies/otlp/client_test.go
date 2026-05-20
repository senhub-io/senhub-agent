package otlp

import (
	"context"
	"testing"
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
	exp, err := buildExporters(context.Background(), testConfigForProtocol("grpc"))
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
	exp, err := buildExporters(context.Background(), testConfigForProtocol("http"))
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

	exp, err := buildExporters(context.Background(), cfg)
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

			exp, err := buildExporters(context.Background(), cfg)
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
