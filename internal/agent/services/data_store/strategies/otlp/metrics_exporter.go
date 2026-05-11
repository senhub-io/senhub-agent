package otlp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// scopeName / scopeVersion identify the Meter that produced these
// metrics. Per the OTel spec, scope is mandatory on every emitted batch
// and identifies the component that authored the instrumentation. We
// use a stable name + the agent build version (or "dev" if unset).
const scopeName = "senhub-agent/otlp-strategy"

// pushMetrics performs one full snapshot → resolve → encode → export
// cycle. Errors from any stage propagate; the caller decides whether
// to log + continue or unwind.
//
// The stable-time argument lets the caller pin the data point timestamp
// for the entire batch (not per-series). This is what Prometheus and
// the OTel spec recommend — the receiver shouldn't see micro-skew
// between series collected within the same scrape.
func pushMetrics(
	ctx context.Context,
	store *metricStore,
	defs otelmapper.DefinitionLookup,
	res *resource.Resource,
	scopeVersion string,
	startTime time.Time,
	now time.Time,
	resolveOpts otelmapper.ResolveOptions,
	extraRecords []otelmapper.OtelRecord,
	export func(context.Context, *metricdata.ResourceMetrics) error,
	missingMappingHandler func(otelmapper.CacheMetric, error),
) (int, error) {
	cms, _ := store.snapshot()
	if len(cms) == 0 && len(extraRecords) == 0 {
		return 0, nil
	}

	// Agent self-metrics (extraRecords) are appended FIRST so they
	// appear in the export batch even when no probe data flowed
	// during this tick — operators get continuity on the agent's
	// own resource graphs even when probes are quiet.
	records := make([]otelmapper.OtelRecord, 0, len(extraRecords)+len(cms))
	records = append(records, extraRecords...)
	for _, cm := range cms {
		def := defs.GetProbeDefinition(cm.ProbeType)
		if def == nil {
			if missingMappingHandler != nil {
				missingMappingHandler(cm, fmt.Errorf("no probe definition for probe_type=%q", cm.ProbeType))
			}
			continue
		}
		recs, err := otelmapper.Resolve(def, cm, resolveOpts)
		if err != nil {
			if missingMappingHandler != nil {
				missingMappingHandler(cm, err)
			}
			continue
		}
		records = append(records, recs...)
	}

	if len(records) == 0 {
		return 0, nil
	}

	rm := buildResourceMetrics(records, res, scopeVersion, startTime, now)
	if err := export(ctx, rm); err != nil {
		return 0, fmt.Errorf("export: %w", err)
	}
	return len(records), nil
}

// buildResourceMetrics groups OtelRecords into the SDK's
// ResourceMetrics shape: one Metric per (name, unit, type) triple, with
// all variants on attributes folded into the metric's data points.
//
// Type → Aggregation mapping (per OTel spec):
//
//	counter        → Sum{Cumulative, Monotonic}
//	updowncounter  → Sum{Cumulative, !Monotonic}
//	gauge          → Gauge
//	histogram      → currently unused by the agent — we don't emit
//	                 histograms today, so this maps to a Gauge as a
//	                 conservative degradation rather than dropping data
//
// Cumulative temporality is the OTel default and what
// VictoriaMetrics/Prometheus expect. Delta is exposed via
// Config.Metrics.Temporality but the SDK's preferred path for our
// straight cumulative counters is the Sum aggregation with
// Cumulative temporality regardless — the receiver can convert to
// delta on its side if needed (vmagent does).
func buildResourceMetrics(
	records []otelmapper.OtelRecord,
	res *resource.Resource,
	scopeVersion string,
	startTime time.Time,
	now time.Time,
) *metricdata.ResourceMetrics {
	type metricKey struct {
		name string
		unit string
		typ  string
	}
	type metricGroup struct {
		key         metricKey
		description string
		points      []otelmapper.OtelRecord
	}

	groups := map[metricKey]*metricGroup{}
	var order []metricKey
	for _, r := range records {
		k := metricKey{name: r.Name, unit: r.Unit, typ: r.Type}
		g, ok := groups[k]
		if !ok {
			g = &metricGroup{key: k, description: r.Description}
			groups[k] = g
			order = append(order, k)
		} else if g.description == "" && r.Description != "" {
			g.description = r.Description
		}
		g.points = append(g.points, r)
	}

	metrics := make([]metricdata.Metrics, 0, len(order))
	for _, k := range order {
		g := groups[k]
		metrics = append(metrics, metricdata.Metrics{
			Name:        g.key.name,
			Description: g.description,
			Unit:        g.key.unit,
			Data:        buildAggregation(g.key.typ, g.points, startTime, now),
		})
	}

	if scopeVersion == "" {
		scopeVersion = "dev"
	}

	return &metricdata.ResourceMetrics{
		Resource: res,
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Scope: instrumentation.Scope{
					Name:    scopeName,
					Version: scopeVersion,
				},
				Metrics: metrics,
			},
		},
	}
}

// buildAggregation produces the SDK Aggregation matching the OTel type.
// Always uses float64 — the agent's internal representation is
// float-typed and our metric semantics don't require int storage.
func buildAggregation(otelType string, points []otelmapper.OtelRecord, startTime, now time.Time) metricdata.Aggregation {
	dps := make([]metricdata.DataPoint[float64], 0, len(points))
	for _, p := range points {
		dps = append(dps, metricdata.DataPoint[float64]{
			Attributes: attributeSet(p.Attributes),
			StartTime:  startTime,
			Time:       now,
			Value:      p.Value,
		})
	}

	switch otelType {
	case "counter":
		return metricdata.Sum[float64]{
			DataPoints:  dps,
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		}
	case "updowncounter":
		return metricdata.Sum[float64]{
			DataPoints:  dps,
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: false,
		}
	default:
		// "gauge" and unknown types both fall through here. Unknown
		// types are emitted as gauges — the receiver sees the data,
		// just without the explicit cumulative semantics. Better than
		// dropping the metric outright and forcing the consumer to
		// debug a missing series.
		return metricdata.Gauge[float64]{DataPoints: dps}
	}
}

// attributeSet converts an OtelRecord's attribute map into the SDK's
// canonical attribute.Set. Sorts keys for stable encoding (the SDK
// canonicalizes internally too, but explicit sort keeps test output
// deterministic regardless of SDK internals).
func attributeSet(m map[string]string) attribute.Set {
	if len(m) == 0 {
		return *attribute.EmptySet()
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kvs := make([]attribute.KeyValue, 0, len(keys))
	for _, k := range keys {
		kvs = append(kvs, attribute.String(k, m[k]))
	}
	return attribute.NewSet(kvs...)
}
