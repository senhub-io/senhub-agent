package otlp

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func rsWithResource(attrs map[string]string) *tracepb.ResourceSpans {
	kvs := make([]*commonpb.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, stringKV(k, v))
	}
	return &tracepb.ResourceSpans{
		Resource:   &resourcepb.Resource{Attributes: kvs},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "op"}}}},
	}
}

func attrMap(rs *tracepb.ResourceSpans) map[string]string {
	out := map[string]string{}
	for _, kv := range rs.GetResource().GetAttributes() {
		out[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	return out
}

func testEnricher() *traceEnricher {
	return buildTraceEnricher(
		TracesSignal{RelayEnrichment: true},
		map[string]string{"host.id": "H1", "host.name": "box1"},
		map[string]string{"tenant": "acme", "site": "paris"},
		"prod", "agent-123",
	)
}

func TestTraceEnricher_AddsMarkersAndDefaultTags(t *testing.T) {
	e := testEnricher()
	in := rsWithResource(map[string]string{"service.name": "checkout"})
	out := e.enrich([]*tracepb.ResourceSpans{in})[0]

	got := attrMap(out)
	// Emitter identity preserved.
	if got["service.name"] != "checkout" {
		t.Errorf("emitter service.name lost: %v", got)
	}
	// Namespaced markers always added.
	if got["senhub.agent.host.id"] != "H1" || got["senhub.agent.host.name"] != "box1" || got["senhub.agent.instance.id"] != "agent-123" {
		t.Errorf("markers missing: %v", got)
	}
	// Tenant/site/env inserted.
	if got["tenant"] != "acme" || got["site"] != "paris" || got["deployment.environment"] != "prod" {
		t.Errorf("default tags not inserted: %v", got)
	}
}

func TestTraceEnricher_NeverOverwritesEmitterValues(t *testing.T) {
	e := testEnricher()
	// The app already set tenant and its own host.id — both must survive.
	in := rsWithResource(map[string]string{
		"service.name": "checkout",
		"tenant":       "app-owned",
		"host.id":      "app-host",
	})
	out := e.enrich([]*tracepb.ResourceSpans{in})[0]
	got := attrMap(out)

	if got["tenant"] != "app-owned" {
		t.Errorf("insert-if-absent overwrote emitter tenant: %v", got)
	}
	if got["host.id"] != "app-host" {
		t.Errorf("emitter host.id clobbered: %v", got)
	}
	// The agent's own host.id still lands under its reserved namespace.
	if got["senhub.agent.host.id"] != "H1" {
		t.Errorf("agent marker missing: %v", got)
	}
}

func TestTraceEnricher_PerSourceOverride(t *testing.T) {
	e := buildTraceEnricher(
		TracesSignal{
			RelayEnrichment: true,
			RelayTenantOverrides: []TraceTenantOverride{
				{MatchKey: "service.namespace", MatchValue: "client-b", Tags: map[string]string{"tenant": "client-b"}},
			},
		},
		nil,
		map[string]string{"tenant": "default-tenant"},
		"", "",
	)

	// Matching source → override tenant.
	matched := e.enrich([]*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.namespace": "client-b"})})[0]
	if got := attrMap(matched)["tenant"]; got != "client-b" {
		t.Errorf("override not applied: tenant=%q", got)
	}
	// Non-matching source → default tenant.
	other := e.enrich([]*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.namespace": "client-a"})})[0]
	if got := attrMap(other)["tenant"]; got != "default-tenant" {
		t.Errorf("default not applied: tenant=%q", got)
	}
}

func TestTraceEnricher_DisabledIsVerbatimNoCopy(t *testing.T) {
	e := buildTraceEnricher(TracesSignal{RelayEnrichment: false}, map[string]string{"host.id": "H1"}, nil, "", "")
	in := []*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.name": "x"})}
	out := e.enrich(in)
	// Same slice, same backing elements — no allocation, true pass-through.
	if &out[0] != &in[0] {
		t.Errorf("disabled enricher must return the input slice unchanged")
	}
}

func TestTraceEnricher_CopyOnWriteDoesNotMutateInput(t *testing.T) {
	e := testEnricher()
	in := rsWithResource(map[string]string{"service.name": "checkout"})
	beforeLen := len(in.Resource.Attributes)

	out := e.enrich([]*tracepb.ResourceSpans{in})[0]

	if len(in.Resource.Attributes) != beforeLen {
		t.Errorf("input Resource.Attributes mutated: was %d now %d", beforeLen, len(in.Resource.Attributes))
	}
	if out.Resource == in.Resource {
		t.Errorf("Resource must be copied, not shared")
	}
	// Spans are shared (not copied — they dominate the payload).
	if out.ScopeSpans[0] != in.ScopeSpans[0] {
		t.Errorf("ScopeSpans should be shared with the input, not copied")
	}
}
