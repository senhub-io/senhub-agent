package otlpreceiver

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	zlog := zerolog.New(os.Stderr)
	return (*logger.Logger)(&zlog)
}

// captureCallback records every batch the probe forwards.
type captureCallback struct {
	mu     sync.Mutex
	points []data_store.DataPoint
}

func (c *captureCallback) fn(points []data_store.DataPoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.points = append(c.points, points...)
	return nil
}

func (c *captureCallback) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.points)
}

func newTestProbe(t *testing.T, cfg map[string]interface{}, cb *captureCallback) (*OTLPReceiverProbe, chan struct{}) {
	t.Helper()
	p, err := NewOTLPReceiverProbe(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewOTLPReceiverProbe: %v", err)
	}
	probe := p.(*OTLPReceiverProbe)
	probe.SetName("otlp_receiver_test")
	probe.SetCallback(cb.fn)

	quit := make(chan struct{})
	if err := probe.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		close(quit)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = probe.OnShutdown(ctx)
	})
	return probe, quit
}

func sampleRequest() *collectormetricspb.ExportMetricsServiceRequest {
	return &collectormetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: wrap(nil, gaugeMetric("ingest.test.gauge", 12.5), sumMetric("ingest.test.sum", 3)),
	}
}

func waitForPoints(t *testing.T, cb *captureCallback, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cb.count() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d points, got %d", want, cb.count())
}

func TestGRPCReceiver_IngestsDatapoints(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "grpc", "address": "127.0.0.1:0"}, cb)

	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	client := collectormetricspb.NewMetricsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Export(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if resp.GetPartialSuccess().GetRejectedDataPoints() != 0 {
		t.Errorf("rejected = %d, want 0", resp.GetPartialSuccess().GetRejectedDataPoints())
	}

	waitForPoints(t, cb, 2)

	// Datapoints must be enriched with probe_name / probe_type.
	cb.mu.Lock()
	defer cb.mu.Unlock()
	for _, p := range cb.points {
		var hasType bool
		for _, tg := range p.Tags {
			if tg.Key == "probe_type" && tg.Value == "otlp_receiver" {
				hasType = true
			}
		}
		if !hasType {
			t.Errorf("datapoint %q missing probe_type=otlp_receiver tag", p.Name)
		}
	}
}

func TestGRPCReceiver_HistogramIngested(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "grpc", "address": "127.0.0.1:0"}, cb)
	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	hist := &metricpb.Metric{
		Name: "h", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{
				Count: 3, Sum: float64Ptr(1.5),
				BucketCounts: []uint64{2, 1}, ExplicitBounds: []float64{1},
			}},
		}},
	}
	req := &collectormetricspb.ExportMetricsServiceRequest{ResourceMetrics: wrap(nil, hist)}

	client := collectormetricspb.NewMetricsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if resp.GetPartialSuccess().GetRejectedDataPoints() != 0 {
		t.Errorf("rejected = %d, want 0 (histogram is now ingested as component series)", resp.GetPartialSuccess().GetRejectedDataPoints())
	}
	// Expanded: h_count, h_sum, h_bucket{le=1}, h_bucket{le=+Inf} = 4 series.
	waitForPoints(t, cb, 4)
}

func TestGRPCReceiver_LogsIngestedAndRelayed(t *testing.T) {
	// Subscribe to the agent log channel so we can observe the relay.
	sub := agentstate.SubscribeLogs(16)
	defer agentstate.UnsubscribeLogs(sub)

	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "127.0.0.1:0",
		"signals": []interface{}{"metrics", "logs"},
	}, cb)
	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	rec := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_WARN,
		SeverityText:   "WARN",
		Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}},
	}
	req := &collectorlogspb.ExportLogsServiceRequest{ResourceLogs: wrapLogs(nil, rec)}

	client := collectorlogspb.NewLogsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, req); err != nil {
		t.Fatalf("Export: %v", err)
	}

	select {
	case got := <-sub:
		if got.Body != "hello" || got.SeverityText != "WARN" {
			t.Errorf("relayed record = %+v, want body=hello severity=WARN", got)
		}
		if got.ProducerProbeType != "otlp_receiver" {
			t.Errorf("producer type = %q, want otlp_receiver", got.ProducerProbeType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the relayed log record")
	}
}

func sampleTraceRequest(spanName string) *collectortracepb.ExportTraceServiceRequest {
	return &collectortracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{Name: spanName}},
			}},
		}},
	}
}

func TestGRPCReceiver_TracesIngestedAndRelayed(t *testing.T) {
	// Subscribe to the agent span channel so we can observe the relay.
	sub := agentstate.SubscribeSpans(16)
	defer agentstate.UnsubscribeSpans(sub)

	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "127.0.0.1:0",
		"signals": []interface{}{"traces"},
	}, cb)
	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	client := collectortracepb.NewTraceServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, sampleTraceRequest("relay-me")); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Spans are relayed verbatim (raw proto, no internal model): assert
	// the ResourceSpans batch and the span name survive unchanged.
	select {
	case got := <-sub:
		if len(got) != 1 {
			t.Fatalf("relayed batch has %d ResourceSpans, want 1", len(got))
		}
		if name := got[0].GetScopeSpans()[0].GetSpans()[0].GetName(); name != "relay-me" {
			t.Errorf("relayed span name = %q, want relay-me", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the relayed span batch")
	}
}

func TestHTTPReceiver_TracesIngestedAndRelayed(t *testing.T) {
	sub := agentstate.SubscribeSpans(16)
	defer agentstate.UnsubscribeSpans(sub)

	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "http", "address": "127.0.0.1:0",
		"signals": []interface{}{"traces"},
	}, cb)
	addr := probe.listener.Addr().String()

	body, err := proto.Marshal(sampleTraceRequest("relay-me-http"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp, err := http.Post("http://"+addr+httpTracesPath, "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case got := <-sub:
		if name := got[0].GetScopeSpans()[0].GetSpans()[0].GetName(); name != "relay-me-http" {
			t.Errorf("relayed span name = %q, want relay-me-http", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the relayed span batch")
	}
}

func TestGRPCReceiver_UnsetMetricPartialSuccess(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "grpc", "address": "127.0.0.1:0"}, cb)
	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	req := &collectormetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: wrap(nil, &metricpb.Metric{Name: "mystery"}),
	}
	client := collectormetricspb.NewMetricsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if resp.GetPartialSuccess().GetRejectedDataPoints() != 1 {
		t.Errorf("rejected = %d, want 1 (unset metric data type)", resp.GetPartialSuccess().GetRejectedDataPoints())
	}
}

func TestHTTPReceiver_IngestsDatapoints(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	addr := probe.listener.Addr().String()

	body, err := proto.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp, err := http.Post("http://"+addr+defaultHTTPPath, "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	waitForPoints(t, cb, 2)
}

func TestHTTPReceiver_RejectsBadContentType(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "http", "address": "127.0.0.1:0"}, cb)
	addr := probe.listener.Addr().String()

	resp, err := http.Post("http://"+addr+defaultHTTPPath, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", resp.StatusCode)
	}
}

// ---- ingress guard integration (#278 lot 2) ----

func grpcExport(t *testing.T, addr string, md map[string]string) error {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for k, v := range md {
		ctx = metadata.AppendToOutgoingContext(ctx, k, v)
	}
	_, err = collectormetricspb.NewMetricsServiceClient(conn).Export(ctx, sampleRequest())
	return err
}

func TestGRPCReceiver_GuardBearerToken(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "127.0.0.1:0", "bearer_token": "s3cret",
	}, cb)
	addr := probe.listener.Addr().String()

	if err := grpcExport(t, addr, nil); status.Code(err) != codes.Unauthenticated {
		t.Errorf("no token: code = %v, want Unauthenticated", status.Code(err))
	}
	if err := grpcExport(t, addr, map[string]string{"authorization": "Bearer wrong"}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("wrong token: code = %v, want Unauthenticated", status.Code(err))
	}
	if err := grpcExport(t, addr, map[string]string{"authorization": "Bearer s3cret"}); err != nil {
		t.Errorf("valid token: %v", err)
	}
	waitForPoints(t, cb, 2)
}

func TestGRPCReceiver_GuardCIDR(t *testing.T) {
	cb := &captureCallback{}
	// Loopback client against a 192.0.2.0/24-only allow-list: denied.
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "127.0.0.1:0",
		"allowed_cidrs": []interface{}{"192.0.2.0/24"},
	}, cb)
	addr := probe.listener.Addr().String()

	if err := grpcExport(t, addr, nil); status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", status.Code(err))
	}
	if got := cb.count(); got != 0 {
		t.Errorf("points ingested despite CIDR denial: %d", got)
	}
}

func TestGRPCReceiver_GuardRateLimit(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "grpc", "address": "127.0.0.1:0",
		"rate_limit_rps": 0.001, "rate_limit_burst": 2,
	}, cb)
	addr := probe.listener.Addr().String()

	for i := 0; i < 2; i++ {
		if err := grpcExport(t, addr, nil); err != nil {
			t.Fatalf("request %d within burst: %v", i, err)
		}
	}
	if err := grpcExport(t, addr, nil); status.Code(err) != codes.ResourceExhausted {
		t.Errorf("code = %v, want ResourceExhausted", status.Code(err))
	}
}

func TestHTTPReceiver_GuardStatusCodes(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{
		"protocol": "http", "address": "127.0.0.1:0",
		"bearer_token":   "s3cret",
		"rate_limit_rps": 0.001, "rate_limit_burst": 1,
	}, cb)
	addr := probe.listener.Addr().String()

	body, err := proto.Marshal(sampleRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	post := func(token string) int {
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+defaultHTTPPath, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := post(""); code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", code)
	}
	if code := post("wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", code)
	}
	if code := post("s3cret"); code != http.StatusOK {
		t.Errorf("valid token: status = %d, want 200", code)
	}
	// Burst of 1 is spent; the next authenticated request is throttled.
	if code := post("s3cret"); code != http.StatusTooManyRequests {
		t.Errorf("over rate: status = %d, want 429", code)
	}
	waitForPoints(t, cb, 2)
}
