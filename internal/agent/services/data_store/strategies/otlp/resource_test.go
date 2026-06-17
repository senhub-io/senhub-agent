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
	res := buildResource(cfg, "1.2.3", nil, nil)
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
	res := buildResource(cfg, "", nil, nil)
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

func TestBuildResource_GlobalTagsOnResource(t *testing.T) {
	// global_tags must land on the Resource (issue #202).
	cfg := ResourceConfig{
		ServiceName: "agent-x",
		Extra:       map[string]string{"k8s.cluster.name": "edge-01"},
	}
	res := buildResource(cfg, "1.0.0", nil, map[string]string{
		"site":   "paris",
		"tenant": "acme",
	})
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["site"] != "paris" || got["tenant"] != "acme" {
		t.Errorf("global_tags missing from Resource: %v", got)
	}
	if got["k8s.cluster.name"] != "edge-01" {
		t.Errorf("explicit resource Extra lost: %v", got)
	}
}

func TestBuildResource_ExtraWinsOverGlobalTag(t *testing.T) {
	// A same-named key declared in the explicit resource: block overrides
	// the global_tag value.
	cfg := ResourceConfig{Extra: map[string]string{"site": "explicit"}}
	res := buildResource(cfg, "", nil, map[string]string{"site": "from-global"})
	for _, kv := range res.Attributes() {
		if string(kv.Key) == "site" && kv.Value.AsString() != "explicit" {
			t.Errorf("site=%q, want explicit (resource Extra must win)", kv.Value.AsString())
		}
	}
}

func TestBuildResource_HostAttrsForCorrelation(t *testing.T) {
	// host.*/os.* land on the resource so telemetry carries the same host.id as
	// the host entity (entity↔telemetry join).
	cfg := ResourceConfig{ServiceName: "agent-x"}
	hostAttrs := map[string]string{
		"host.id":   "h-001",
		"host.name": "web-1",
		"os.type":   "linux",
	}
	res := buildResource(cfg, "1.0.0", hostAttrs, nil)
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["host.id"] != "h-001" || got["host.name"] != "web-1" || got["os.type"] != "linux" {
		t.Errorf("host resource attrs missing: %v", got)
	}
}

func TestBuildResource_OperatorOverridesHostAttr(t *testing.T) {
	// An operator global_tag / Extra of the same key wins over the detected host
	// attribute (host attrs are lowest precedence).
	cfg := ResourceConfig{Extra: map[string]string{"host.name": "operator-name"}}
	res := buildResource(cfg, "", map[string]string{"host.name": "detected"}, nil)
	for _, kv := range res.Attributes() {
		if string(kv.Key) == "host.name" && kv.Value.AsString() != "operator-name" {
			t.Errorf("host.name=%q, want operator-name (Extra must win)", kv.Value.AsString())
		}
	}
}
