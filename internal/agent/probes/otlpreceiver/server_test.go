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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"

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

func TestGRPCReceiver_HistogramPartialSuccess(t *testing.T) {
	cb := &captureCallback{}
	probe, _ := newTestProbe(t, map[string]interface{}{"protocol": "grpc", "address": "127.0.0.1:0"}, cb)
	addr := probe.listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	hist := &metricpb.Metric{
		Name: "h",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{}},
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
	if resp.GetPartialSuccess().GetRejectedDataPoints() != 1 {
		t.Errorf("rejected = %d, want 1", resp.GetPartialSuccess().GetRejectedDataPoints())
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
