package otlp

import (
	"bytes"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// TestTraceEnricher_PreservesUnknownResourceFields is the M1 regression
// guard: a span from a newer OTel schema carries Resource fields (entity_refs,
// future additions) the pinned proto version does not know — protobuf-go
// keeps them as unknown fields. Enrichment MUST preserve them (the emitter's
// foreign identity), not silently drop them by rebuilding the Resource.
func TestTraceEnricher_PreservesUnknownResourceFields(t *testing.T) {
	e := testEnricher() // active: inserts tenant/site/env

	base := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{stringKV("service.name", "checkout")}}
	raw, err := proto.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	// Append an unknown field #100 (varint 42) — stands in for any
	// newer-schema Resource field the pinned proto type can't name.
	unknown := []byte{0xA0, 0x06, 0x2A}
	raw = append(raw, unknown...)
	var res resourcepb.Resource
	if err := proto.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}

	in := &tracepb.ResourceSpans{
		Resource:   &res,
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "op"}}}},
	}
	out := e.enrich([]*tracepb.ResourceSpans{in})[0]

	outBytes, err := proto.Marshal(out.Resource)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBytes, unknown) {
		t.Error("unknown Resource field dropped by enrichment (M1 regression)")
	}
	if attrMap(out)["tenant"] != "acme" {
		t.Errorf("enrichment tag not inserted: %v", attrMap(out))
	}
}

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
		map[string]string{"tenant": "acme", "site": "paris"},
		"prod",
		"agent-host-id", "agent-host", "agent-instance-1",
	)
}

func TestTraceEnricher_InsertsStandardTags(t *testing.T) {
	e := testEnricher()
	in := rsWithResource(map[string]string{"service.name": "checkout"})
	out := e.enrich([]*tracepb.ResourceSpans{in})[0]

	got := attrMap(out)
	// Emitter identity preserved.
	if got["service.name"] != "checkout" {
		t.Errorf("emitter service.name lost: %v", got)
	}
	// Tenant/site/env inserted — standard / operator keys only, no vendor
	// namespace. The relay identity is under the neutral telemetry.relay.*
	// space (#698), never senhub.*.
	if got["tenant"] != "acme" || got["site"] != "paris" || got["deployment.environment"] != "prod" {
		t.Errorf("default tags not inserted: %v", got)
	}
	for k := range got {
		if strings.HasPrefix(k, "senhub.") {
			t.Errorf("unexpected product-namespaced attribute %q on relayed span", k)
		}
	}
}

// TestTraceEnricher_StampsRelayIdentity: the telemetry.relay.* set is added to
// a relayed span, sourced from this agent's host identity + instance id (#698).
func TestTraceEnricher_StampsRelayIdentity(t *testing.T) {
	e := testEnricher()
	out := e.enrich([]*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.name": "checkout"})})[0]
	got := attrMap(out)
	if got[relayHostIDKey] != "agent-host-id" {
		t.Errorf("%s = %q, want agent-host-id", relayHostIDKey, got[relayHostIDKey])
	}
	if got[relayHostNameKey] != "agent-host" {
		t.Errorf("%s = %q, want agent-host", relayHostNameKey, got[relayHostNameKey])
	}
	if got[relayInstanceIDKey] != "agent-instance-1" {
		t.Errorf("%s = %q, want agent-instance-1", relayInstanceIDKey, got[relayInstanceIDKey])
	}
}

// TestTraceEnricher_RelayFirstRelayWins: if ANY relay key is already present
// (a prior relay stamped it), the whole set is left untouched — no partial mix.
func TestTraceEnricher_RelayFirstRelayWins(t *testing.T) {
	e := testEnricher()
	in := rsWithResource(map[string]string{
		"service.name":   "checkout",
		relayInstanceIDKey: "upstream-agent", // only one of the three present
	})
	out := e.enrich([]*tracepb.ResourceSpans{in})[0]
	got := attrMap(out)
	if got[relayInstanceIDKey] != "upstream-agent" {
		t.Errorf("upstream relay instance overwritten: %q", got[relayInstanceIDKey])
	}
	if _, ok := got[relayHostIDKey]; ok {
		t.Errorf("partial relay set completed — %s must not be added when the set is already partly present", relayHostIDKey)
	}
}

// TestTraceEnricher_ActiveWithRelayOnly: relay identity alone (no global_tags)
// keeps the enricher active and stamps the relay set — the active() fix (#698).
func TestTraceEnricher_ActiveWithRelayOnly(t *testing.T) {
	e := buildTraceEnricher(TracesSignal{RelayEnrichment: true}, nil, "", "agent-host-id", "agent-host", "agent-instance-1")
	if !e.active() {
		t.Fatal("enricher with relay identity but no global_tags must be active")
	}
	out := e.enrich([]*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.name": "checkout"})})[0]
	if attrMap(out)[relayHostIDKey] != "agent-host-id" {
		t.Error("relay identity not stamped when no global_tags configured")
	}
}

// TestTraceEnricher_RelayOmittedWhenHostIDEmpty: a transient host-info failure
// (empty host.id) omits the relay set rather than stamping an empty join key.
func TestTraceEnricher_RelayOmittedWhenHostIDEmpty(t *testing.T) {
	e := buildTraceEnricher(TracesSignal{RelayEnrichment: true}, nil, "", "", "agent-host", "agent-instance-1")
	out := e.enrich([]*tracepb.ResourceSpans{rsWithResource(map[string]string{"service.name": "checkout"})})
	got := attrMap(out[0])
	if _, ok := got[relayHostIDKey]; ok {
		t.Error("relay set stamped with an empty host.id")
	}
	// No tags and no stampable relay id → verbatim pass-through (only the
	// emitter's own service.name remains).
	if len(got) != 1 {
		t.Errorf("expected verbatim span (only service.name), got %v", got)
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
}

func TestTraceEnricher_PerSourceOverride(t *testing.T) {
	e := buildTraceEnricher(
		TracesSignal{
			RelayEnrichment: true,
			RelayTenantOverrides: []TraceTenantOverride{
				{MatchKey: "service.namespace", MatchValue: "client-b", Tags: map[string]string{"tenant": "client-b"}},
			},
		},
		map[string]string{"tenant": "default-tenant"},
		"", "agent-host-id", "agent-host", "agent-instance-1",
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
	e := buildTraceEnricher(TracesSignal{RelayEnrichment: false}, map[string]string{"tenant": "acme"}, "", "agent-host-id", "agent-host", "agent-instance-1")
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
