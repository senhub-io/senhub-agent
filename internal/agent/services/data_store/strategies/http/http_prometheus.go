// senhub-agent/internal/agent/services/data_store/strategies/http/http_prometheus.go
//
// Bridge between the HTTP strategy (owning cache + transformer registry) and
// the self-contained `prometheus` package that produces the text exposition.
package http

import (
	"net/http"

	"senhub-agent.go/internal/agent/services/data_store/strategies/http/prometheus"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// cacheAdapter satisfies prometheus.CacheReader by wrapping the strategy's
// MetricCache. Converts internal.CachedMetric to prometheus.CacheMetric and
// coerces interface{} values to float64.
type cacheAdapter struct {
	cache *MetricCache
}

func (a *cacheAdapter) GetAll() []prometheus.CacheMetric {
	src := a.cache.GetAllMetrics()
	out := make([]prometheus.CacheMetric, 0, len(src))
	for _, m := range src {
		val, ok := coerceToFloat64(m.Value)
		if !ok {
			// Non-numeric values (strings, booleans expressed as strings, etc.)
			// cannot be emitted as Prometheus metrics. Skip silently; the
			// YAML should either provide a lookup or declare skip:true.
			continue
		}
		probeType := m.Tags["probe_type"]
		out = append(out, prometheus.CacheMetric{
			ProbeName:  m.ProbeName,
			ProbeType:  probeType,
			MetricName: m.MetricName,
			Value:      val,
			Unit:       m.Unit,
			Tags:       m.Tags,
		})
	}
	return out
}

// registryAdapter satisfies prometheus.DefinitionLookup.
type registryAdapter struct {
	registry *transformers.TransformerRegistry
}

func (r *registryAdapter) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	return r.registry.GetProbeDefinition(probeType)
}

// coerceToFloat64 extracts a float64 from the typed-opaque cache value.
// Supports the numeric types the probes actually emit.
func coerceToFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// servePrometheusExposition writes the Prometheus text exposition to w by
// reading the shared cache and resolving each entry through the transformer
// registry. Used by both the /api/{key}/prometheus/metrics and /metrics routes.
func (h *HTTPSyncStrategy) servePrometheusExposition(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", prometheus.ContentType)

	reader := &cacheAdapter{cache: h.cache}
	defs := &registryAdapter{registry: h.transformerRegistry}

	count, err := prometheus.WriteExposition(reader, defs, w, func(m prometheus.CacheMetric, err error) {
		h.logger.Debug().
			Err(err).
			Str("probe_name", m.ProbeName).
			Str("probe_type", m.ProbeType).
			Str("metric_name", m.MetricName).
			Msg("Skipping cache entry in Prometheus exposition")
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to write Prometheus exposition")
		// Headers already sent — cannot change status. The partial body is
		// still likely parseable; Prometheus scrapers will report what they got.
		return
	}

	h.logger.Debug().Int("record_count", count).Msg("Prometheus exposition served")
}
