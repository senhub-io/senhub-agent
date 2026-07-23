// Package solr implements the free Apache Solr monitoring probe.
// It uses the Solr native metrics API (GET /admin/metrics?wt=json&group=all)
// to collect JVM, node-level, and per-core metrics.
//
// A jolokia.go client is included in the package for completeness with other
// probes that support both Jolokia and native HTTP; the native Solr API is
// used as the primary (and only active) collection path.
package solr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and license checks.
const ProbeType = "solr"

const (
	defaultEndpoint = "http://localhost:8983"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
)

// solrConfig holds the parsed probe configuration.
type solrConfig struct {
	Endpoint     string
	InstanceName string
	Timeout      time.Duration
	Interval     time.Duration
}

// SolrProbe collects metrics from a local or remote Solr instance.
type SolrProbe struct {
	*types.BaseProbe
	config       solrConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *solrEntitySource
}

// NewSolrProbe constructs the probe. Config errors surface here.
func NewSolrProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.solr")

	cfg := solrConfig{
		Endpoint: defaultEndpoint,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = strings.TrimRight(v, "/")
	}
	// jolokia_url accepted for naming consistency with other probes but
	// the native Solr metrics API is always used as the active path.
	if v, ok := config["jolokia_url"].(string); ok && v != "" {
		// The URL may point to the Solr admin endpoint; strip /solr/admin/metrics suffix
		// if present and use only the base (scheme://host:port) portion.
		if idx := strings.Index(v, "/solr/"); idx > 0 {
			cfg.Endpoint = strings.TrimRight(v[:idx], "/")
		} else {
			cfg.Endpoint = strings.TrimRight(v, "/")
		}
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}
	probe := &SolrProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		client:       httpClient,
		entitySrc:    newSolrEntitySource(cfg.Endpoint, cfg.InstanceName, httpClient),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *SolrProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *SolrProbe) ShouldStart() bool          { return true }
func (p *SolrProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *SolrProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.config.Endpoint).
		Msg("Starting solr probe")
	return nil
}

func (p *SolrProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// solrMetricsResponse is the top-level shape of GET /admin/metrics?wt=json&group=all.
type solrMetricsResponse struct {
	Metrics map[string]json.RawMessage `json:"metrics"`
}

// solrCoresResponse is the shape of GET /admin/cores?action=STATUS&wt=json.
type solrCoresResponse struct {
	Status map[string]json.RawMessage `json:"status"`
}

// Collect fetches metrics from Solr's admin APIs and returns datapoints.
// An unreachable Solr emits senhub.solr.up=0 and returns nil (not an error).
func (p *SolrProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points := p.collect(now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *SolrProbe) collect(now time.Time) []data_store.DataPoint {
	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	commonTags := []tags.Tag{
		{Key: "metric_type", Value: "overview"},
	}

	metricsResp, err := p.fetchMetrics(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.config.Endpoint).Msg("solr metrics fetch failed")
		p.entitySrc.setReachable(false, "")
		return []data_store.DataPoint{
			{Name: "senhub.solr.up", Value: 0, Timestamp: now, Tags: commonTags},
		}
	}
	p.entitySrc.setReachable(true, "")
	p.entitySrc.tryPinClusterIDOrHostPort(ctx)

	var points []data_store.DataPoint
	points = append(points, data_store.DataPoint{
		Name: "senhub.solr.up", Value: 1, Timestamp: now, Tags: commonTags,
	})

	// JVM metrics
	points = append(points, p.buildJVMPoints(metricsResp, now)...)

	// Node metrics
	points = append(points, p.buildNodePoints(metricsResp, now)...)

	// Per-core metrics from /admin/cores?action=STATUS
	coresResp, err := p.fetchCores(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("solr cores fetch failed; per-core metrics skipped")
	} else {
		points = append(points, p.buildCorePoints(metricsResp, coresResp, now)...)
	}

	return points
}

// fetchMetrics calls GET /admin/metrics?wt=json&group=all and returns the parsed response.
func (p *SolrProbe) fetchMetrics(ctx context.Context) (*solrMetricsResponse, error) {
	url := p.config.Endpoint + "/solr/admin/metrics?wt=json&group=all"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building metrics request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("reading metrics response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solr metrics returned HTTP %d", resp.StatusCode)
	}

	var mr solrMetricsResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("parsing metrics response: %w", err)
	}
	return &mr, nil
}

// fetchCores calls GET /admin/cores?action=STATUS&wt=json and returns the parsed response.
func (p *SolrProbe) fetchCores(ctx context.Context) (*solrCoresResponse, error) {
	url := p.config.Endpoint + "/solr/admin/cores?action=STATUS&wt=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building cores request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading cores response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solr cores returned HTTP %d", resp.StatusCode)
	}

	var cr solrCoresResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("parsing cores response: %w", err)
	}
	return &cr, nil
}

// buildJVMPoints extracts JVM heap and thread metrics from metrics.solr.jvm.
func (p *SolrProbe) buildJVMPoints(mr *solrMetricsResponse, now time.Time) []data_store.DataPoint {
	raw, ok := mr.Metrics["solr.jvm"]
	if !ok {
		return nil
	}
	var jvm map[string]interface{}
	if err := json.Unmarshal(raw, &jvm); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("failed to parse solr.jvm metrics")
		return nil
	}

	memTags := []tags.Tag{{Key: "metric_type", Value: "memory"}}
	threadTags := []tags.Tag{{Key: "metric_type", Value: "threads"}}

	var points []data_store.DataPoint

	if heapUsed := extractNestedFloat(jvm, "memory.heap.used"); heapUsed >= 0 {
		points = append(points, data_store.DataPoint{
			Name:      "jvm.memory.heap.used",
			Value:     float64(heapUsed),
			Timestamp: now,
			Tags:      memTags,
		})
	}

	if threadCount := extractNestedFloat(jvm, "threads.count"); threadCount >= 0 {
		points = append(points, data_store.DataPoint{
			Name:      "jvm.threads.count",
			Value:     float64(threadCount),
			Timestamp: now,
			Tags:      threadTags,
		})
	}

	return points
}

// buildNodePoints extracts request, error, and cache metrics from metrics.solr.node.
func (p *SolrProbe) buildNodePoints(mr *solrMetricsResponse, now time.Time) []data_store.DataPoint {
	raw, ok := mr.Metrics["solr.node"]
	if !ok {
		return nil
	}
	var node map[string]interface{}
	if err := json.Unmarshal(raw, &node); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("failed to parse solr.node metrics")
		return nil
	}

	reqTags := []tags.Tag{{Key: "metric_type", Value: "requests"}}
	cacheTags := []tags.Tag{{Key: "metric_type", Value: "cache"}}

	var points []data_store.DataPoint

	// Sum request counts and times across all QUERY handlers.
	var totalRequests, totalErrors float64
	var totalTimeMs float64

	for key, val := range node {
		if !strings.HasPrefix(key, "QUERY.") || !strings.HasSuffix(key, ".requestTimes") {
			continue
		}
		rt, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		if count := extractFloat(rt, "count"); count >= 0 {
			totalRequests += count
			if meanMs := extractFloat(rt, "meanMs"); meanMs >= 0 {
				totalTimeMs += meanMs * count
			}
		}
	}

	for key, val := range node {
		if !strings.HasPrefix(key, "QUERY.") || !strings.HasSuffix(key, ".errors") {
			continue
		}
		rt, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		if count := extractFloat(rt, "count"); count >= 0 {
			totalErrors += count
		}
	}

	if totalRequests >= 0 {
		points = append(points,
			data_store.DataPoint{Name: "solr.requests.count", Value: float64(totalRequests), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "solr.requests.time", Value: float64(totalTimeMs), Timestamp: now, Tags: reqTags},
			data_store.DataPoint{Name: "solr.errors.count", Value: float64(totalErrors), Timestamp: now, Tags: reqTags},
		)
	}

	// Cache metrics: CACHE.searcher.queryResultCache.* (the key itself contains dots,
	// so we look up the top-level Solr key directly rather than using extractNestedFloat).
	if cache, ok := node["CACHE.searcher.queryResultCache"]; ok {
		if cacheMap, ok := cache.(map[string]interface{}); ok {
			if hits := extractFloat(cacheMap, "hits"); hits >= 0 {
				points = append(points, data_store.DataPoint{
					Name: "solr.cache.hits", Value: float64(hits), Timestamp: now, Tags: cacheTags,
				})
			}
			if inserts := extractFloat(cacheMap, "inserts"); inserts >= 0 {
				points = append(points, data_store.DataPoint{
					Name: "solr.cache.inserts", Value: float64(inserts), Timestamp: now, Tags: cacheTags,
				})
			}
		}
	}

	return points
}

// buildCorePoints iterates metrics["solr.core.<name>"] and /admin/cores status
// to emit per-core document count and index size.
func (p *SolrProbe) buildCorePoints(mr *solrMetricsResponse, cr *solrCoresResponse, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	for key, raw := range mr.Metrics {
		if !strings.HasPrefix(key, "solr.core.") {
			continue
		}
		coreName := strings.TrimPrefix(key, "solr.core.")

		var coreMetrics map[string]interface{}
		if err := json.Unmarshal(raw, &coreMetrics); err != nil {
			p.moduleLogger.Warn().Err(err).Str("core", coreName).Msg("failed to parse core metrics")
			continue
		}

		coreTags := []tags.Tag{
			{Key: "metric_type", Value: "storage"},
			{Key: "core", Value: coreName},
		}

		if indexSize := extractNestedFloat(coreMetrics, "INDEX.sizeInBytes"); indexSize >= 0 {
			points = append(points, data_store.DataPoint{
				Name: "solr.index.size", Value: float64(indexSize), Timestamp: now, Tags: coreTags,
			})
		}

		// numDocs comes from the /admin/cores?action=STATUS response.
		if cr != nil {
			if coreStatus, ok := cr.Status[coreName]; ok {
				var statusMap map[string]interface{}
				if err := json.Unmarshal(coreStatus, &statusMap); err == nil {
					if numDocs := extractNestedFloat(statusMap, "index.numDocs"); numDocs >= 0 {
						points = append(points, data_store.DataPoint{
							Name: "solr.document.count", Value: float64(numDocs), Timestamp: now, Tags: coreTags,
						})
					}
				}
			}
		}
	}

	return points
}

// extractNestedFloat resolves a dot-separated path in a nested map[string]interface{}
// and returns the numeric value, or -1 if not found / not numeric.
func extractNestedFloat(m map[string]interface{}, path string) float64 {
	parts := strings.SplitN(path, ".", 2)
	val, ok := m[parts[0]]
	if !ok {
		return -1
	}
	if len(parts) == 1 {
		return toFloat(val)
	}
	nested, ok := val.(map[string]interface{})
	if !ok {
		return -1
	}
	return extractNestedFloat(nested, parts[1])
}

// extractFloat reads a named key from a flat map and converts it to float64, or -1.
func extractFloat(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return -1
	}
	return toFloat(v)
}

// toFloat converts a JSON-decoded numeric value (float64 or json.Number) to float64.
func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return -1
		}
		return f
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return -1
}
