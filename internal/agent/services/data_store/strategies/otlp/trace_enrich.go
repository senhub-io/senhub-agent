package otlp

import (
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// traceEnricher adds the agent's correlation context to RELAYED spans so a
// backend can pivot from a third-party app's trace to the infrastructure
// telemetry of the same tenant/host (#294). Relayed spans carry the
// EMITTING APP's Resource — a foreign identity that must be preserved — so
// enrichment is strictly merge-not-overwrite:
//
//   - senhub.agent.* markers are added under a reserved namespace that can
//     never collide with the app's own keys ("relayed by which agent").
//   - Tenant/site/environment tags are inserted ONLY when the app did not
//     set that key (emitter value always wins). The default set is the
//     agent's global_tags; a per-source override swaps it when an incoming
//     span's Resource matches a rule (the shared-gateway case).
//
// The app's own service.name / service.instance.id / host.* are never
// touched. This mirrors the OpenTelemetry Collector's k8sattributes
// (namespaced additions) + resourceprocessor `action: insert` semantics.
type traceEnricher struct {
	enabled bool
	// markers are added to every relayed Resource (reserved namespace).
	markers []*commonpb.KeyValue
	// defaultTags are inserted only when the key is absent (tenant/site/env).
	defaultTags map[string]string
	// overrides swap defaultTags for spans whose Resource matches a rule.
	overrides []traceTenantOverride
}

// traceTenantOverride replaces the default insert-if-absent tag set for
// relayed spans whose Resource attribute matchKey equals matchValue —
// e.g. route each end-client's traffic to its own tenant when one agent
// relays for several clients.
type traceTenantOverride struct {
	matchKey   string
	matchValue string
	tags       map[string]string
}

// active reports whether the enricher would add anything. A disabled or
// empty enricher is a no-op and the relay forwards spans verbatim.
func (e *traceEnricher) active() bool {
	return e != nil && e.enabled && (len(e.markers) > 0 || len(e.defaultTags) > 0 || len(e.overrides) > 0)
}

// enrich returns a batch with each ResourceSpans' Resource augmented,
// copy-on-write. The ScopeSpans (the spans themselves, which dominate the
// payload) are shared with the input, never copied or mutated — only the
// Resource is rebuilt. Returns the input unchanged when inactive.
func (e *traceEnricher) enrich(rs []*tracepb.ResourceSpans) []*tracepb.ResourceSpans {
	if !e.active() {
		return rs
	}
	out := make([]*tracepb.ResourceSpans, len(rs))
	for i, orig := range rs {
		out[i] = e.enrichOne(orig)
	}
	return out
}

func (e *traceEnricher) enrichOne(orig *tracepb.ResourceSpans) *tracepb.ResourceSpans {
	var existing []*commonpb.KeyValue
	var dropped uint32
	if orig.GetResource() != nil {
		existing = orig.Resource.Attributes
		dropped = orig.Resource.DroppedAttributesCount
	}

	present := make(map[string]bool, len(existing))
	for _, kv := range existing {
		present[kv.GetKey()] = true
	}

	tags := e.resolveTags(present, existing)

	merged := make([]*commonpb.KeyValue, 0, len(existing)+len(tags)+len(e.markers))
	merged = append(merged, existing...) // emitter attrs preserved verbatim
	for k, v := range tags {
		if !present[k] {
			merged = append(merged, stringKV(k, v))
		}
	}
	merged = append(merged, e.markers...) // reserved namespace, always added

	// Shallow-copy the ResourceSpans; keep ScopeSpans shared (spans are the
	// bulk of the payload and are not mutated), rebuild only the Resource.
	return &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{
			Attributes:             merged,
			DroppedAttributesCount: dropped,
		},
		ScopeSpans: orig.ScopeSpans,
		SchemaUrl:  orig.SchemaUrl,
	}
}

// resolveTags picks the insert-if-absent tag set for one Resource: the
// first override whose match key/value is present on the span, else the
// default set.
func (e *traceEnricher) resolveTags(present map[string]bool, attrs []*commonpb.KeyValue) map[string]string {
	for _, o := range e.overrides {
		if present[o.matchKey] && resourceAttrValue(attrs, o.matchKey) == o.matchValue {
			return o.tags
		}
	}
	return e.defaultTags
}

// resourceAttrValue returns the string value of a resource attribute, or ""
// when absent or non-string.
func resourceAttrValue(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func stringKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   k,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}},
	}
}

// buildTraceEnricher assembles the enricher from the strategy's resolved
// identity + config. hostAttrs holds host.id/host.name; instanceID is the
// agent's service.instance.id (may be empty when entity emission is off).
// globalTags + environment become the default insert-if-absent set.
func buildTraceEnricher(cfg TracesSignal, hostAttrs, globalTags map[string]string, environment, instanceID string) *traceEnricher {
	if !cfg.RelayEnrichment {
		return &traceEnricher{enabled: false}
	}

	var markers []*commonpb.KeyValue
	if id := hostAttrs["host.id"]; id != "" {
		markers = append(markers, stringKV("senhub.agent.host.id", id))
	}
	if name := hostAttrs["host.name"]; name != "" {
		markers = append(markers, stringKV("senhub.agent.host.name", name))
	}
	if instanceID != "" {
		markers = append(markers, stringKV("senhub.agent.instance.id", instanceID))
	}

	defaultTags := make(map[string]string, len(globalTags)+1)
	for k, v := range globalTags {
		defaultTags[k] = v
	}
	if environment != "" {
		// Only fill the standard key from the agent's own environment when
		// the operator didn't already carry it in global_tags.
		if _, ok := defaultTags["deployment.environment"]; !ok {
			defaultTags["deployment.environment"] = environment
		}
	}

	overrides := make([]traceTenantOverride, 0, len(cfg.RelayTenantOverrides))
	for _, o := range cfg.RelayTenantOverrides {
		tags := make(map[string]string, len(o.Tags))
		for k, v := range o.Tags {
			tags[k] = v
		}
		overrides = append(overrides, traceTenantOverride{
			matchKey:   o.MatchKey,
			matchValue: o.MatchValue,
			tags:       tags,
		})
	}

	return &traceEnricher{
		enabled:     true,
		markers:     markers,
		defaultTags: defaultTags,
		overrides:   overrides,
	}
}
