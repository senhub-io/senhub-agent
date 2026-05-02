// senhub-agent/internal/agent/services/data_store/strategies/http/http_prometheus.go
//
// Bridge between the HTTP strategy (owning cache + transformer registry) and
// the self-contained `prometheus` package that produces the text exposition.
package http

import (
	"net/http"
	"sync"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
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

// prometheusWarnedMetrics keeps track of (probe_type, metric_name) pairs for
// which we've already emitted a "no OTel mapping" warning. Prevents log spam
// since scrapes happen every 15-60s and an unmapped metric would otherwise
// warn on every scrape.
//
// Package-level: an agent has at most one HTTP strategy, and the dedup state
// is conceptually an agent-lifetime cache. Keyed by "probe_type:metric_name".
var prometheusWarnedMetrics sync.Map

// servePrometheusExposition writes the Prometheus text exposition to w by
// reading the shared cache and resolving each entry through the transformer
// registry. Used by both the /api/{key}/prometheus/metrics and /metrics routes.
//
// Policy on unmapped metrics (Q4 revised, 2026-04-21):
//   - If a cache entry has no OTel mapping in its probe's YAML definition,
//     it is skipped (not emitted) and a WARN is logged ONCE per
//     (probe_type, metric_name) for the lifetime of the agent.
//   - The scrape continues normally — missing mapping never blocks the
//     endpoint or prevents the agent from running.
//   - Operators see the warning, fix the YAML, metric appears in next scrape.
func (h *HTTPSyncStrategy) servePrometheusExposition(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", prometheus.ContentType)

	reader := &cacheAdapter{cache: h.cache}
	defs := &registryAdapter{registry: h.transformerRegistry}

	// Build agent self-metrics (uptime, cache, probes, collect errors,
	// HTTP requests per endpoint, build info).
	probesTotal, probesHealthy := agentstate.GetProbeCounts()
	agentRecords := prometheus.BuildAgentRecords(prometheus.AgentMetricsSnapshot{
		StartTime:              h.startTime,
		CacheEntries:           h.cache.GetCacheInfo().TotalMetrics,
		ProbesActive:           h.cache.GetCacheInfo().ProbeCount,
		ProbesTotal:            probesTotal,
		ProbesHealthy:          probesHealthy,
		CollectErrorsTotal:     agentstate.GetCollectErrorsTotal(),
		HTTPRequestsByEndpoint: GetHTTPRequestCounts(),
		BuildVersion:           agentBuildVersion(),
		BuildCommit:            agentBuildCommit(),
	})

	count, err := prometheus.WriteExposition(reader, defs, agentRecords, w, func(m prometheus.CacheMetric, errCb error) {
		key := m.ProbeType + ":" + m.MetricName
		if _, seen := prometheusWarnedMetrics.LoadOrStore(key, struct{}{}); seen {
			return
		}
		h.logger.Warn().
			Err(errCb).
			Str("probe_name", m.ProbeName).
			Str("probe_type", m.ProbeType).
			Str("metric_name", m.MetricName).
			Msg("Metric has no OTel mapping - not exposed in /metrics. Add an otel: block to the probe YAML or otel.skip: true to silence.")
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to write Prometheus exposition")
		return
	}

	h.logger.Debug().Int("record_count", count).Msg("Prometheus exposition served")
}

// agentBuildVersion returns the agent build version from cliArgs.Version
// (set at build time via -ldflags). Falls back to "development" if empty.
func agentBuildVersion() string {
	if v := cliArgs.Version; v != "" {
		return v
	}
	return "development"
}

// agentBuildCommit returns the short commit hash from cliArgs.CommitHash,
// or "" if the build did not embed it.
func agentBuildCommit() string {
	return cliArgs.CommitHash
}

