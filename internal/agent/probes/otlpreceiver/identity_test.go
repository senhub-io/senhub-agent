package otlpreceiver

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func TestIngest_ReceiverOwnsProbeIdentity(t *testing.T) {
	cb := &captureCallback{}
	p, err := NewOTLPReceiverProbe(map[string]interface{}{"protocol": "grpc", "address": "127.0.0.1:0"}, testLogger())
	if err != nil {
		t.Fatalf("NewOTLPReceiverProbe: %v", err)
	}
	probe := p.(*OTLPReceiverProbe)
	probe.SetName("otlp-relay")
	probe.SetCallback(cb.fn)

	src := data_store.DataPoint{
		Name:      "system.memory.limit",
		Timestamp: time.Now(),
		Value:     1,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "memory"},
			{Key: "probe_type", Value: "memory"},
			{Key: "host", Value: "src-host"},
		},
	}
	if err := probe.ingest([]data_store.DataPoint{src}, 0); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if len(src.Tags) != 3 {
		t.Fatalf("input point mutated: %v", src.Tags)
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if len(cb.points) != 1 {
		t.Fatalf("forwarded %d points, want 1", len(cb.points))
	}
	counts := map[string]int{}
	vals := map[string]string{}
	for _, tg := range cb.points[0].Tags {
		counts[tg.Key]++
		vals[tg.Key] = tg.Value
	}
	if counts["probe_name"] != 1 || vals["probe_name"] != "otlp-relay" {
		t.Errorf("probe_name = %d x %q, want 1 x otlp-relay", counts["probe_name"], vals["probe_name"])
	}
	if counts["probe_type"] != 1 || vals["probe_type"] != "otlp_receiver" {
		t.Errorf("probe_type = %d x %q, want 1 x otlp_receiver", counts["probe_type"], vals["probe_type"])
	}
	if vals["host"] != "src-host" {
		t.Errorf("host tag lost: %q", vals["host"])
	}
}
