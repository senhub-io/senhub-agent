package otlp

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/probesdk/tags"
)

func relayMetricBatch(serviceName, metricName string) []*metricpb.ResourceMetrics {
	return []*metricpb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			stringKV("service.name", serviceName),
		}},
		ScopeMetrics: []*metricpb.ScopeMetrics{{
			Metrics: []*metricpb.Metric{{
				Name: metricName,
				Data: &metricpb.Metric_Gauge{Gauge: &metricpb.Gauge{
					DataPoints: []*metricpb.NumberDataPoint{{
						TimeUnixNano: uint64(time.Now().UnixNano()),
						Value:        &metricpb.NumberDataPoint_AsDouble{AsDouble: 1},
					}},
				}},
			}},
		}},
	}}
}

func captureMetricsHTTP(t *testing.T) (chan *collectormetricspb.ExportMetricsServiceRequest, string) {
	t.Helper()
	reqs := make(chan *collectormetricspb.ExportMetricsServiceRequest, 8)
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
		var req collectormetricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(raw, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		reqs <- &req
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return reqs, endpointOf(srv)
}

// TestMetricsRelay_PreservesEmitterResource is the point of the metric
// relay: an ingested point must leave under the EMITTER's Resource, not
// re-encoded under the agent's, so a reserved identity key does not carry
// two different values in one export (#767).
func TestMetricsRelay_PreservesEmitterResource(t *testing.T) {
	reqs, addr := captureMetricsHTTP(t)

	cfg := relayTestConfig(addr, "http")
	cfg.Metrics.Interval = 200 * time.Millisecond
	relay, err := newMetricsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newMetricsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishMetricBatches(relayMetricBatch("checkout-api", "app.requests"))

	select {
	case got := <-reqs:
		rm := got.GetResourceMetrics()
		if len(rm) != 1 {
			t.Fatalf("resource metrics = %d, want 1", len(rm))
		}
		if name := resourceAttrValue(rm[0].GetResource().GetAttributes(), "service.name"); name != "checkout-api" {
			t.Errorf("service.name = %q, want checkout-api (the emitter's, not the agent's)", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP metrics endpoint got no request within 5s")
	}
}

// TestMetricsRelay_CountsRelayedPoints pins the success-side self-metric.
func TestMetricsRelay_CountsRelayedPoints(t *testing.T) {
	reqs, addr := captureMetricsHTTP(t)

	before := agentstate.GetOTLPMetricsRelayedTotal()

	cfg := relayTestConfig(addr, "http")
	cfg.Metrics.Interval = 200 * time.Millisecond
	relay, err := newMetricsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newMetricsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishMetricBatches(relayMetricBatch("checkout-api", "app.requests"))

	select {
	case <-reqs:
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP metrics endpoint got no request within 5s")
	}

	deadline := time.Now().Add(2 * time.Second)
	var got uint64
	for time.Now().Before(deadline) {
		got = agentstate.GetOTLPMetricsRelayedTotal() - before
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != 1 {
		t.Errorf("relayed point delta = %d, want 1", got)
	}
}

// TestAddDataPoints_SkipsReceiverPointsWhenRelaying is the duplication
// guard: with a metric relay active, a receiver-produced datapoint must
// NOT also enter this strategy's store, or the same measurement ships
// twice — the second time under the agent's Resource, which is exactly
// the ambiguity the relay removes.
func TestAddDataPoints_SkipsReceiverPointsWhenRelaying(t *testing.T) {
	s := &OTLPSyncStrategy{store: newMetricStore()}

	receiverPoint := datapoint.DataPoint{
		Name:      "app.requests",
		Timestamp: time.Now(),
		Value:     1,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "otlp_receiver"},
			{Key: "probe_type", Value: "otlp_receiver"},
		},
	}
	probePoint := datapoint.DataPoint{
		Name:      "system.cpu.utilization",
		Timestamp: time.Now(),
		Value:     0.5,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu"},
			{Key: "probe_type", Value: "cpu"},
		},
	}

	// No relay yet: the store is the only route, so nothing may be skipped.
	if err := s.AddDataPoints([]datapoint.DataPoint{receiverPoint, probePoint}); err != nil {
		t.Fatalf("AddDataPoints: %v", err)
	}
	if got := s.store.size(); got != 2 {
		t.Fatalf("store size without a relay = %d, want 2 (nothing must be dropped)", got)
	}

	// Relay active: receiver points travel it instead.
	s2 := &OTLPSyncStrategy{store: newMetricStore(), metricsRelay: &metricsRelay{}}
	if err := s2.AddDataPoints([]datapoint.DataPoint{receiverPoint, probePoint}); err != nil {
		t.Fatalf("AddDataPoints: %v", err)
	}
	if got := s2.store.size(); got != 1 {
		t.Errorf("store size with a relay = %d, want 1 (the receiver point must not be stored too)", got)
	}
}
