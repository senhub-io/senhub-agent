package otlp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// capturedRequest records what an OTLP/HTTP receiver saw.
type capturedRequest struct {
	method      string
	path        string
	contentType string
	encoding    string
	bodyLen     int
}

// newOTLPHTTPTestServer returns an httptest.Server that mimics a
// conformant OTLP/HTTP receiver: it accepts POST on the standard
// signal paths and replies 200 with an empty (valid) protobuf
// response. Every request is pushed onto the returned channel.
func newOTLPHTTPTestServer(t *testing.T) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	reqs := make(chan capturedRequest, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs <- capturedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			encoding:    r.Header.Get("Content-Encoding"),
			bodyLen:     len(body),
		}
		// 200 + empty body is a valid empty ExportXxxServiceResponse.
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, reqs
}

// endpointOf strips the scheme from an httptest URL so it can be fed
// to the OTLP exporters' WithEndpoint (which wants host:port).
func endpointOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func httpE2EConfig(srv *httptest.Server) Config {
	cfg := defaultConfig()
	cfg.Protocol = "http"
	cfg.Endpoint = endpointOf(srv)
	cfg.TLS.Enabled = false   // httptest server is plain HTTP
	cfg.Retry.Enabled = false // fail fast in tests
	cfg.Compression = "gzip"  // exercise the gzip path
	return cfg
}

// TestOTLPHTTP_MetricExport_E2E proves an OTLP/HTTP metric export
// completes a full round trip: serialize → gzip → POST /v1/metrics →
// 200 → response parsed. Export returning nil is the decisive signal;
// the captured request confirms the wire shape is spec-conformant.
func TestOTLPHTTP_MetricExport_E2E(t *testing.T) {
	srv, reqs := newOTLPHTTPTestServer(t)
	cfg := httpE2EConfig(srv)

	exp, err := buildMetricExporter(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildMetricExporter(http): %v", err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	rm := buildResourceMetrics(
		[]otelmapper.OtelRecord{
			{Name: "system.cpu.time", Unit: "s", Type: "counter", Value: 3},
			{Name: "system.memory.usage", Unit: "By", Type: "gauge", Value: 100},
		},
		resource.NewSchemaless(),
		"1.0",
		time.Unix(1699999000, 0),
		time.Unix(1700000000, 0),
	)

	if err := exp.Export(context.Background(), rm); err != nil {
		t.Fatalf("metric Export over OTLP/HTTP failed: %v", err)
	}

	select {
	case got := <-reqs:
		if got.method != http.MethodPost {
			t.Errorf("method = %q, want POST", got.method)
		}
		if got.path != "/v1/metrics" {
			t.Errorf("path = %q, want /v1/metrics (spec OTLP/HTTP path)", got.path)
		}
		if got.contentType != "application/x-protobuf" {
			t.Errorf("Content-Type = %q, want application/x-protobuf", got.contentType)
		}
		if got.encoding != "gzip" {
			t.Errorf("Content-Encoding = %q, want gzip", got.encoding)
		}
		if got.bodyLen == 0 {
			t.Error("request body is empty — nothing was serialized")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OTLP/HTTP receiver got no request within 5s")
	}
}

// TestOTLPHTTP_LogExport_E2E does the same for the logs signal.
func TestOTLPHTTP_LogExport_E2E(t *testing.T) {
	srv, reqs := newOTLPHTTPTestServer(t)
	cfg := httpE2EConfig(srv)

	exp, err := buildLogExporter(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildLogExporter(http): %v", err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	var rec sdklog.Record
	rec.SetTimestamp(time.Now())
	rec.SetBody(log.StringValue("e2e log line"))

	if err := exp.Export(context.Background(), []sdklog.Record{rec}); err != nil {
		t.Fatalf("log Export over OTLP/HTTP failed: %v", err)
	}

	select {
	case got := <-reqs:
		if got.method != http.MethodPost {
			t.Errorf("method = %q, want POST", got.method)
		}
		if got.path != "/v1/logs" {
			t.Errorf("path = %q, want /v1/logs", got.path)
		}
		if got.contentType != "application/x-protobuf" {
			t.Errorf("Content-Type = %q, want application/x-protobuf", got.contentType)
		}
		if got.bodyLen == 0 {
			t.Error("request body is empty — nothing was serialized")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OTLP/HTTP receiver got no request within 5s")
	}
}
