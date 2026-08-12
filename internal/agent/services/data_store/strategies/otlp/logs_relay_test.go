package otlp

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// relayLogBatch builds a one-record ResourceLogs batch carrying the
// emitting application's own Resource — the identity the relay exists to
// preserve.
func relayLogBatch(serviceName, body string) []*logspb.ResourceLogs {
	return []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			stringKV("service.name", serviceName),
		}},
		ScopeLogs: []*logspb.ScopeLogs{{
			LogRecords: []*logspb.LogRecord{{
				TimeUnixNano: uint64(time.Now().UnixNano()),
				Body: &commonpb.AnyValue{
					Value: &commonpb.AnyValue_StringValue{StringValue: body},
				},
			}},
		}},
	}}
}

// captureLogsHTTP starts a mock OTLP/HTTP logs endpoint and returns the
// channel of decoded requests plus its endpoint.
func captureLogsHTTP(t *testing.T) (chan *collectorlogspb.ExportLogsServiceRequest, string) {
	t.Helper()
	reqs := make(chan *collectorlogspb.ExportLogsServiceRequest, 8)
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
		var req collectorlogspb.ExportLogsServiceRequest
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

// TestLogsRelay_PreservesEmitterServiceName is the whole point of the log
// relay: an application's service.name must arrive as the application set
// it. Re-emitting ingested logs through the agent's SDK pipeline stamped
// the agent's Resource over it, which made applications indistinguishable
// downstream (#765).
func TestLogsRelay_PreservesEmitterServiceName(t *testing.T) {
	reqs, addr := captureLogsHTTP(t)

	cfg := relayTestConfig(addr, "http")
	cfg.Logs.BatchSize = 1
	relay, err := newLogsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newLogsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishLogBatches(relayLogBatch("checkout-api", "hello"))

	select {
	case got := <-reqs:
		rl := got.GetResourceLogs()
		if len(rl) != 1 {
			t.Fatalf("resource logs = %d, want 1", len(rl))
		}
		if name := resourceAttrValue(rl[0].GetResource().GetAttributes(), "service.name"); name != "checkout-api" {
			t.Errorf("service.name = %q, want checkout-api (the emitter's, not the agent's)", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP logs endpoint got no request within 5s")
	}
}

// TestLogsRelay_EnrichmentIsAdditive pins the contract that agent context
// is ADDED and never substituted: the emitter keeps every key it set, and
// only absent keys are filled in.
func TestLogsRelay_EnrichmentIsAdditive(t *testing.T) {
	reqs, addr := captureLogsHTTP(t)

	cfg := relayTestConfig(addr, "http")
	cfg.Logs.BatchSize = 1
	enricher := &relayEnricher{
		enabled:         true,
		defaultTags:     map[string]string{"tenant": "acme", "service.name": "agent-should-not-win"},
		relayHostID:     "host-1",
		relayInstanceID: "agent-1",
	}
	relay, err := newLogsRelay(cfg, enricher, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newLogsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishLogBatches(relayLogBatch("checkout-api", "hello"))

	select {
	case got := <-reqs:
		attrs := got.GetResourceLogs()[0].GetResource().GetAttributes()
		if name := resourceAttrValue(attrs, "service.name"); name != "checkout-api" {
			t.Errorf("service.name = %q — the emitter's value must win over the agent's", name)
		}
		if tenant := resourceAttrValue(attrs, "tenant"); tenant != "acme" {
			t.Errorf("tenant = %q, want acme (absent key must be filled in)", tenant)
		}
		if relayed := resourceAttrValue(attrs, relayHostIDKey); relayed != "host-1" {
			t.Errorf("%s = %q, want host-1", relayHostIDKey, relayed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP logs endpoint got no request within 5s")
	}
}

// TestLogsRelay_CountsRelayedRecords pins the success-side self-metric,
// asserted as a delta so prior process-wide state does not matter.
func TestLogsRelay_CountsRelayedRecords(t *testing.T) {
	reqs, addr := captureLogsHTTP(t)

	before := agentstate.GetOTLPLogsRelayedTotal()

	cfg := relayTestConfig(addr, "http")
	cfg.Logs.BatchSize = 1
	relay, err := newLogsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newLogsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishLogBatches(relayLogBatch("checkout-api", "hello"))

	select {
	case <-reqs:
	case <-time.After(5 * time.Second):
		t.Fatal("mock OTLP/HTTP logs endpoint got no request within 5s")
	}

	deadline := time.Now().Add(2 * time.Second)
	var got uint64
	for time.Now().Before(deadline) {
		got = agentstate.GetOTLPLogsRelayedTotal() - before
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != 1 {
		t.Errorf("relayed record delta = %d, want 1", got)
	}
}

// TestLogsRelay_FailedExportDoesNotCount keeps the counter honest: a batch
// the collector refused must not be reported as relayed.
func TestLogsRelay_FailedExportDoesNotCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	before := agentstate.GetOTLPLogsRelayedTotal()

	cfg := relayTestConfig(endpointOf(srv), "http")
	cfg.Logs.BatchSize = 1
	relay, err := newLogsRelay(cfg, nil, testModuleLogger(t))
	if err != nil {
		t.Fatalf("newLogsRelay: %v", err)
	}
	relay.start()
	defer relay.stop(context.Background())

	agentstate.PublishLogBatches(relayLogBatch("checkout-api", "hello"))

	time.Sleep(500 * time.Millisecond)
	if got := agentstate.GetOTLPLogsRelayedTotal() - before; got != 0 {
		t.Errorf("relayed record delta = %d after a refused export, want 0", got)
	}
}
