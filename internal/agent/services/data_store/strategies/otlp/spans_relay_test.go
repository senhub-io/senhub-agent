package otlp

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// captureTracesServer is an in-process OTLP gRPC TracesService that
// records every ResourceSpans batch it receives.
type captureTracesServer struct {
	collectortracepb.UnimplementedTraceServiceServer
	mu     sync.Mutex
	got    []*tracepb.ResourceSpans
	notify chan struct{}
}

func (s *captureTracesServer) Export(
	_ context.Context,
	req *collectortracepb.ExportTraceServiceRequest,
) (*collectortracepb.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	s.got = append(s.got, req.GetResourceSpans()...)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return &collectortracepb.ExportTraceServiceResponse{}, nil
}

func (s *captureTracesServer) firstSpanName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) == 0 {
		return ""
	}
	return s.got[0].GetScopeSpans()[0].GetSpans()[0].GetName()
}

func startTracesGRPCServer(t *testing.T) (*captureTracesServer, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	capSrv := &captureTracesServer{notify: make(chan struct{}, 8)}
	srv := grpc.NewServer()
	collectortracepb.RegisterTraceServiceServer(srv, capSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return capSrv, lis.Addr().String()
}

func relayTestConfig(endpoint, protocol string) Config {
	cfg := defaultConfig()
	cfg.Endpoint = endpoint
	cfg.Protocol = protocol
	cfg.TLS.Enabled = false
	cfg.Traces.Enabled = true
	// Short timer so the relay flushes without filling a 512-span batch.
	cfg.Traces.BatchTimeout = 50 * time.Millisecond
	cfg.Timeout = 2 * time.Second
	return cfg
}

func relaySpanBatch(name string) []*tracepb.ResourceSpans {
	return []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{Name: name}},
		}},
	}}
}

// TestSpansRelay_GRPC proves the full relay round trip: PublishSpans on
// the agentstate channel → relay drain/batch → raw TracesService.Export
// → the mock receiver sees the same span, verbatim.
func TestSpansRelay_GRPC(t *testing.T) {
	capSrv, addr := startTracesGRPCServer(t)

	relay, err := newSpansRelay(relayTestConfig(addr, "grpc"), testModuleLogger(t))
	if err != nil {
		t.Fatalf("newSpansRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishSpans(relaySpanBatch("relayed-span"))

	select {
	case <-capSrv.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("mock TracesService received no export within 5s")
	}
	if got := capSrv.firstSpanName(); got != "relayed-span" {
		t.Errorf("received span name = %q, want relayed-span", got)
	}
}

// TestSpansRelay_GRPC_FlushOnBatchSize confirms the span-count trigger
// flushes before the timer would.
func TestSpansRelay_GRPC_FlushOnBatchSize(t *testing.T) {
	capSrv, addr := startTracesGRPCServer(t)

	cfg := relayTestConfig(addr, "grpc")
	cfg.Traces.BatchSize = 1
	cfg.Traces.BatchTimeout = time.Hour // timer must not be the trigger

	relay, err := newSpansRelay(cfg, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newSpansRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishSpans(relaySpanBatch("batch-size-flush"))

	select {
	case <-capSrv.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("batch-size flush did not fire")
	}
	if got := capSrv.firstSpanName(); got != "batch-size-flush" {
		t.Errorf("received span name = %q, want batch-size-flush", got)
	}
}

// TestSpansRelay_HTTP covers the OTLP/HTTP transport: the marshalled
// ExportTraceServiceRequest lands as a gzipped protobuf POST on the spec
// /v1/traces path.
func TestSpansRelay_HTTP(t *testing.T) {
	type gotReq struct {
		path        string
		contentType string
		encoding    string
		spanName    string
	}
	reqs := make(chan gotReq, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer zr.Close()
			body = zr
		}
		raw, err := io.ReadAll(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req collectortracepb.ExportTraceServiceRequest
		if err := proto.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name := ""
		if rs := req.GetResourceSpans(); len(rs) > 0 {
			name = rs[0].GetScopeSpans()[0].GetSpans()[0].GetName()
		}
		reqs <- gotReq{
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			encoding:    r.Header.Get("Content-Encoding"),
			spanName:    name,
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	relay, err := newSpansRelay(relayTestConfig(endpointOf(srv), "http"), testModuleLogger(t))
	if err != nil {
		t.Fatalf("newSpansRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishSpans(relaySpanBatch("http-relayed-span"))

	select {
	case got := <-reqs:
		if got.path != "/v1/traces" {
			t.Errorf("path = %q, want /v1/traces (spec OTLP/HTTP path)", got.path)
		}
		if got.contentType != "application/x-protobuf" {
			t.Errorf("Content-Type = %q, want application/x-protobuf", got.contentType)
		}
		if got.encoding != "gzip" {
			t.Errorf("Content-Encoding = %q, want gzip (default compression)", got.encoding)
		}
		if got.spanName != "http-relayed-span" {
			t.Errorf("span name = %q, want http-relayed-span", got.spanName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP receiver got no request within 5s")
	}
}

// TestSpansRelay_StopUnsubscribes pins the lifecycle contract: after
// stop, the relay's subscription is gone (SpanSubscriberCount back to
// its pre-start value) and a second stop is a safe no-op.
func TestSpansRelay_StopUnsubscribes(t *testing.T) {
	_, addr := startTracesGRPCServer(t)

	relay, err := newSpansRelay(relayTestConfig(addr, "grpc"), testModuleLogger(t))
	if err != nil {
		t.Fatalf("newSpansRelay: %v", err)
	}

	before := agentstate.SpanSubscriberCount()
	relay.start()
	if got := agentstate.SpanSubscriberCount(); got != before+1 {
		t.Fatalf("subscriber count after start = %d, want %d", got, before+1)
	}
	relay.stop(context.Background())
	if got := agentstate.SpanSubscriberCount(); got != before {
		t.Errorf("subscriber count after stop = %d, want %d", got, before)
	}
	// Idempotent.
	relay.stop(context.Background())
}
