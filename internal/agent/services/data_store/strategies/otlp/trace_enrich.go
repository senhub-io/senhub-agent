package otlp

import (
	"google.golang.org/protobuf/proto"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// traceEnricher adds the agent's correlation context to RELAYED spans so a
// backend can pivot from a third-party app's trace to the infrastructure
// telemetry of the same tenant (#294). Relayed spans carry the EMITTING
// APP's Resource — a foreign identity that must be preserved — so
// enrichment is strictly merge-not-overwrite: tenant/site/environment tags
// are inserted ONLY when the app did not set that key (emitter value always
// wins). The default set is the agent's global_tags; a per-source override
// swaps it when an incoming span's Resource matches a rule (the
// shared-gateway case).
//
// It also stamps a vendor-neutral RELAY-IDENTITY set so a backend can answer
// "which agent relayed this span" and join it to the host node on the infra
// graph (#698):
//
//	telemetry.relay.host.id  — the relaying agent's host.id, char-identical to
//	                           its host entity identity (gopsutil HostID, NOT
//	                           the operator-overridable Resource value, so the
//	                           strict join on the consumer holds).
//	telemetry.relay.host.name — the relaying host name.
//	telemetry.relay.instance.id — the relaying agent's service.instance.id,
//	                           the same value it sets as a Resource attribute
//	                           on its own entity emissions (the per-producer
//	                           reference key).
//
// These keys are generic (any collector/gateway carries the same fact), so
// they live in the neutral telemetry.* space, aligned with the topology
// consumer (Toise) and semconv#759 — NOT a senhub.* product name. Provisional
// pending the SIG, with a migration path. The set is atomic and FIRST-RELAY-
// WINS: it is inserted ONLY when NONE of the three keys is already present, so
// a downstream gateway cannot mix its own instance.id with an upstream relay's
// host.id and destroy the join.
//
// The app's own service.name / service.instance.id / host.* are never
// touched. This mirrors the OpenTelemetry Collector's resourceprocessor
// `action: insert` semantics.
type traceEnricher struct {
	enabled bool
	// defaultTags are inserted only when the key is absent (tenant/site/env).
	defaultTags map[string]string
	// overrides swap defaultTags for spans whose Resource matches a rule.
	overrides []traceTenantOverride
	// relay identity of THIS agent, stamped as the telemetry.relay.* set.
	relayHostID     string
	relayHostName   string
	relayInstanceID string
}

const (
	relayHostIDKey     = "telemetry.relay.host.id"
	relayHostNameKey   = "telemetry.relay.host.name"
	relayInstanceIDKey = "telemetry.relay.instance.id"
)

// traceTenantOverride replaces the default insert-if-absent tag set for
// relayed spans whose Resource attribute matchKey equals matchValue —
// e.g. route each end-client's traffic to its own tenant when one agent
// relays for several clients.
type traceTenantOverride struct {
	matchKey   string
	matchValue string
	tags       map[string]string
}

// active reports whether the enricher would add anything. A disabled enricher
// is a no-op and the relay forwards spans verbatim. Once enabled it is active
// whenever it has a relay identity to stamp (the common case — the agent's
// instance id is always known) OR any insert-if-absent tags, so relay
// identity is added even when no global_tags are configured.
func (e *traceEnricher) active() bool {
	if e == nil || !e.enabled {
		return false
	}
	return e.relayInstanceID != "" || len(e.defaultTags) > 0 || len(e.overrides) > 0
}

// relayKeysToAdd returns the telemetry.relay.* set for one span, or nil. The
// set is atomic and first-relay-wins: nothing is stamped if ANY of the three
// keys is already present (a downstream relay must not overwrite an upstream
// one), or if this agent's host.id/instance.id are unavailable this cycle.
func (e *traceEnricher) relayKeysToAdd(present map[string]bool) []*commonpb.KeyValue {
	if e.relayHostID == "" || e.relayInstanceID == "" {
		return nil
	}
	if present[relayHostIDKey] || present[relayHostNameKey] || present[relayInstanceIDKey] {
		return nil
	}
	kvs := []*commonpb.KeyValue{
		stringKV(relayHostIDKey, e.relayHostID),
		stringKV(relayInstanceIDKey, e.relayInstanceID),
	}
	if e.relayHostName != "" {
		kvs = append(kvs, stringKV(relayHostNameKey, e.relayHostName))
	}
	return kvs
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
	if orig.GetResource() != nil {
		existing = orig.Resource.Attributes
	}

	present := make(map[string]bool, len(existing))
	for _, kv := range existing {
		present[kv.GetKey()] = true
	}

	tags := e.resolveTags(present, existing)

	var toAdd []*commonpb.KeyValue
	for k, v := range tags {
		if !present[k] {
			toAdd = append(toAdd, stringKV(k, v))
		}
	}
	toAdd = append(toAdd, e.relayKeysToAdd(present)...)
	if len(toAdd) == 0 {
		return orig // nothing to add — forward the batch untouched
	}

	// Copy-on-write on the Resource ONLY (ScopeSpans, which dominate the
	// payload, are shared and never mutated). proto.Clone deep-copies the
	// whole Resource — Attributes AND everything else the emitter set
	// (entity_refs, schema-versioned/unknown proto fields) — so the app's
	// foreign identity is preserved; we then only APPEND the insert-if-absent
	// tags. Rebuilding the Resource by hand (Attributes only) silently
	// dropped entity_refs + unknown fields (audit M1).
	var newRes *resourcepb.Resource
	if orig.GetResource() != nil {
		newRes = proto.Clone(orig.Resource).(*resourcepb.Resource)
	} else {
		newRes = &resourcepb.Resource{}
	}
	newRes.Attributes = append(newRes.Attributes, toAdd...)

	return &tracepb.ResourceSpans{
		Resource:   newRes,
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
// config. globalTags + environment become the default insert-if-absent set;
// per-source overrides come from the traces config. relayHostID/relayHostName/
// relayInstanceID are THIS agent's identity for the telemetry.relay.* set —
// relayHostID/Name MUST come from the host identity (gopsutil), NOT the
// operator-overridable Resource, and relayInstanceID from the agent's
// service.instance.id (see #698).
func buildTraceEnricher(cfg TracesSignal, globalTags map[string]string, environment, relayHostID, relayHostName, relayInstanceID string) *traceEnricher {
	if !cfg.RelayEnrichment {
		return &traceEnricher{enabled: false}
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
		enabled:         true,
		defaultTags:     defaultTags,
		overrides:       overrides,
		relayHostID:     relayHostID,
		relayHostName:   relayHostName,
		relayInstanceID: relayInstanceID,
	}
}
