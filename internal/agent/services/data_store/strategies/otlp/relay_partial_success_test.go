package otlp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// The relays speak the collector API directly rather than through an SDK
// exporter, so nothing turns a partial_success into an error for them.
// They discarded the response outright — a rejection of relayed
// third-party telemetry was therefore invisible on the producing host
// AND on the relaying one (#819).

func countedRejections(signal string) uint64 {
	for _, row := range agentstate.GetExportRejected() {
		if row.Signal == signal {
			return row.Count
		}
	}
	return 0
}

// ── gRPC ─────────────────────────────────────────────────────────────

type rejectingRelayLogsServer struct {
	collectorlogspb.UnimplementedLogsServiceServer
}

func (s *rejectingRelayLogsServer) Export(
	_ context.Context,
	_ *collectorlogspb.ExportLogsServiceRequest,
) (*collectorlogspb.ExportLogsServiceResponse, error) {
	return &collectorlogspb.ExportLogsServiceResponse{
		PartialSuccess: &collectorlogspb.ExportLogsPartialSuccess{
			RejectedLogRecords: 4,
			ErrorMessage:       "attribute value type not supported",
		},
	}, nil
}

type rejectingRelayTracesServer struct {
	collectortracepb.UnimplementedTraceServiceServer
}

func (s *rejectingRelayTracesServer) Export(
	_ context.Context,
	_ *collectortracepb.ExportTraceServiceRequest,
) (*collectortracepb.ExportTraceServiceResponse, error) {
	return &collectortracepb.ExportTraceServiceResponse{
		PartialSuccess: &collectortracepb.ExportTracePartialSuccess{
			RejectedSpans: 2,
			ErrorMessage:  "trace id malformed",
		},
	}, nil
}

func startRejectingRelayServer(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// TestRelayGRPCReportsWhatTheCollectorRefused: the forward succeeds —
// the batch was delivered — and the four records the collector kept out
// are counted rather than lost in a discarded response.
func TestRelayGRPCReportsWhatTheCollectorRefused(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	addr := startRejectingRelayServer(t, func(s *grpc.Server) {
		collectorlogspb.RegisterLogsServiceServer(s, &rejectingRelayLogsServer{})
	})

	var buf syncBuffer
	fwd, err := newGRPCLogBatchForwarder(
		relayTestConfig(addr, "grpc"),
		newPartialSuccessReporter(bufferModuleLogger(&buf), "relay"),
	)
	if err != nil {
		t.Fatalf("newGRPCLogBatchForwarder: %v", err)
	}
	t.Cleanup(func() { _ = fwd.close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fwd.forward(ctx, relayLogBatch("app", "relayed record")); err != nil {
		t.Fatalf("forward: %v — a partly refused batch was still delivered", err)
	}

	if got := countedRejections("logs"); got != 4 {
		t.Errorf("export.rejected{signal=logs} = %d, want 4", got)
	}
	if out := buf.String(); !strings.Contains(out, "attribute value type not supported") {
		t.Errorf("the collector's reason was not reported:\n%s", out)
	}
}

// TestRelayGRPCReportsRefusedSpans pins the signal label: the counter is
// what an operator alerts on, and "traces" is what tells them which
// pipeline to look at.
func TestRelayGRPCReportsRefusedSpans(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	addr := startRejectingRelayServer(t, func(s *grpc.Server) {
		collectortracepb.RegisterTraceServiceServer(s, &rejectingRelayTracesServer{})
	})

	var buf syncBuffer
	fwd, err := newGRPCSpanForwarder(
		relayTestConfig(addr, "grpc"),
		newPartialSuccessReporter(bufferModuleLogger(&buf), "relay"),
	)
	if err != nil {
		t.Fatalf("newGRPCSpanForwarder: %v", err)
	}
	t.Cleanup(func() { _ = fwd.close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fwd.forward(ctx, []*tracepb.ResourceSpans{{
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "relayed"}}}},
	}}); err != nil {
		t.Fatalf("forward: %v", err)
	}

	if got := countedRejections("traces"); got != 2 {
		t.Errorf("export.rejected{signal=traces} = %d, want 2", got)
	}
}

// ── HTTP ─────────────────────────────────────────────────────────────

// TestRelayHTTPReadsThePartialSuccessBody is the transport where the
// answer is a body rather than a field, and where it was most plainly
// thrown away: the response went to io.Discard.
func TestRelayHTTPReadsThePartialSuccessBody(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	answer, err := proto.Marshal(&collectorlogspb.ExportLogsServiceResponse{
		PartialSuccess: &collectorlogspb.ExportLogsPartialSuccess{
			RejectedLogRecords: 7,
			ErrorMessage:       "tenant unknown",
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(answer)
	}))
	t.Cleanup(srv.Close)

	var buf syncBuffer
	fwd, err := newHTTPLogBatchForwarder(
		relayTestConfig(endpointOf(srv), "http"),
		newPartialSuccessReporter(bufferModuleLogger(&buf), "relay"),
	)
	if err != nil {
		t.Fatalf("newHTTPLogBatchForwarder: %v", err)
	}
	t.Cleanup(func() { _ = fwd.close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fwd.forward(ctx, relayLogBatch("app", "relayed record")); err != nil {
		t.Fatalf("forward: %v", err)
	}

	if got := countedRejections("logs"); got != 7 {
		t.Errorf("export.rejected{signal=logs} = %d, want 7", got)
	}
	if out := buf.String(); !strings.Contains(out, "tenant unknown") {
		t.Errorf("the collector's reason was not reported:\n%s", out)
	}
}

// TestRelayHTTPAcceptsAnEmptyAnswer: a collector that accepts everything
// answers with an empty body, and that must stay silent — a counter that
// moves on every successful relay is worse than no counter.
func TestRelayHTTPAcceptsAnEmptyAnswer(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	var buf syncBuffer
	fwd, err := newHTTPLogBatchForwarder(
		relayTestConfig(endpointOf(srv), "http"),
		newPartialSuccessReporter(bufferModuleLogger(&buf), "relay"),
	)
	if err != nil {
		t.Fatalf("newHTTPLogBatchForwarder: %v", err)
	}
	t.Cleanup(func() { _ = fwd.close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fwd.forward(ctx, relayLogBatch("app", "relayed record")); err != nil {
		t.Fatalf("forward: %v", err)
	}

	if got := countedRejections("logs"); got != 0 {
		t.Errorf("a fully accepted batch counted %d rejected records", got)
	}
	if out := buf.String(); out != "" {
		t.Errorf("a fully accepted batch logged:\n%s", out)
	}
}
