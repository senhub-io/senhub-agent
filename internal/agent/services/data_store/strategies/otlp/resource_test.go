package otlp

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestBuildResource_AllFields(t *testing.T) {
	cfg := ResourceConfig{
		ServiceName:     "agent-paris",
		ServiceInstance: "abc12345",
		Environment:     "prod",
		Extra: map[string]string{
			"k8s.cluster.name": "edge-01",
		},
	}
	res := buildResource(cfg, "1.2.3")
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	want := map[string]string{
		resourceKeyServiceName:     "agent-paris",
		resourceKeyServiceInstance: "abc12345",
		resourceKeyServiceVersion:  "1.2.3",
		resourceKeyEnvironment:     "prod",
		"k8s.cluster.name":         "edge-01",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attr[%q]=%q, want %q", k, got[k], v)
		}
	}
}

func TestBuildResource_OmitsEmpty(t *testing.T) {
	// Empty fields must NOT produce empty-string attributes (those would
	// be valid OTel resource keys and would mislead the consumer).
	cfg := ResourceConfig{
		ServiceName: "x",
	}
	res := buildResource(cfg, "")
	for _, kv := range res.Attributes() {
		if kv.Value.Type() == attribute.STRING && kv.Value.AsString() == "" {
			t.Errorf("empty-string attribute leaked: %s", kv.Key)
		}
	}
	// Only service.name should be set.
	if got := len(res.Attributes()); got != 1 {
		t.Errorf("attrs len=%d, want 1", got)
	}
}
