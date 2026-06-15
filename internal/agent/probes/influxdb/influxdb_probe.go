// Package influxdb implements the free influxdb probe: availability and
// performance metrics for InfluxDB 2.x via the standard /health,
// /metrics (Prometheus text format), and /api/v2/buckets endpoints.
//
// The /metrics endpoint exposes Go runtime and InfluxDB engine metrics
// in Prometheus text format; we parse that directly rather than bundling
// a Prometheus client library dependency.
package influxdb

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// the licence catalogue.
const ProbeType = "influxdb"

// probeConfig holds the validated configuration for one probe instance.
type probeConfig struct {
	Endpoint     string
	Token        string
	Org          string
	InstanceName string
	Timeout      time.Duration
	Interval     time.Duration
}

// InfluxDBProbe monitors a single InfluxDB 2.x instance.
type InfluxDBProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *influxdbEntitySource
	unregister   func()
}

const (
	defaultEndpoint = "http://localhost:8086"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
)

// NewInfluxDBProbe constructs the probe. Configuration errors are
// returned immediately so the agent can report them at startup.
func NewInfluxDBProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.influxdb")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &InfluxDBProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		entitySrc: newInfluxdbEntitySource(cfg),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint: defaultEndpoint,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = strings.TrimRight(v, "/")
	}
	if v, ok := config["token"].(string); ok {
		cfg.Token = v
	}
	if v, ok := config["org"].(string); ok {
		cfg.Org = v
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *InfluxDBProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *InfluxDBProbe) ShouldStart() bool          { return true }
func (p *InfluxDBProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *InfluxDBProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting influxdb probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *InfluxDBProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect scrapes /health, /metrics, and /api/v2/buckets. A failure on
// any individual endpoint is recorded as a metric (up=0 or missing
// value), never returned as a collection error.
func (p *InfluxDBProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "metric_type", Value: "availability"},
	}

	var points []data_store.DataPoint

	// --- /health ---
	up, version := p.checkHealth()
	p.entitySrc.setReachable(up, version)
	upVal := float64(0)
	if up {
		upVal = 1
	}
	healthTags := append(baseTags[:len(baseTags):len(baseTags)], tags.Tag{Key: "metric_type", Value: "availability"})
	if version != "" {
		healthTags = append(healthTags, tags.Tag{Key: "version", Value: version})
	}
	points = append(points, data_store.DataPoint{
		Name:      "senhub.influxdb.up",
		Value:     upVal,
		Timestamp: now,
		Tags:      healthTags,
	})

	// --- /metrics (Prometheus text) ---
	promMetrics := p.scrapePrometheusMetrics()
	metricsTags := []tags.Tag{{Key: "metric_type", Value: "performance"}}

	promMap := map[string]struct {
		outName string
		tags    []tags.Tag
	}{
		"storage_reads_total":                    {outName: "influxdb.storage.reads", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "io"})},
		"boltdb_reads_total":                     {outName: "influxdb.storage.reads", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "io"})},
		"storage_writes_total":                   {outName: "influxdb.storage.writes", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "io"})},
		"boltdb_writes_total":                    {outName: "influxdb.storage.writes", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "io"})},
		"http_query_request_bytes_total":         {outName: "influxdb.query.requests", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "query"})},
		"query_requests_total":                   {outName: "influxdb.query.requests", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "query"})},
		"task_scheduler_currently_running_tasks": {outName: "influxdb.tasks.runs.active", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "tasks"})},
		"task_scheduler_total_runs_complete":     {outName: "influxdb.tasks.runs.complete", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "tasks"})},
		"task_scheduler_total_runs_failed":       {outName: "influxdb.tasks.runs.failed", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "tasks"})},
		"go_goroutines":                          {outName: "go.goroutines", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "runtime"})},
		"go_memstats_heap_inuse_bytes":           {outName: "go.memory.heap.used", tags: append(metricsTags, tags.Tag{Key: "metric_type", Value: "runtime"})},
	}

	// Track which output names have already been emitted to avoid
	// doubling up when both primary and fallback metric names exist.
	emitted := map[string]bool{}
	for promName, out := range promMap {
		if emitted[out.outName] {
			continue
		}
		if val, ok := promMetrics[promName]; ok {
			points = append(points, data_store.DataPoint{
				Name:      out.outName,
				Value:     float64(val),
				Timestamp: now,
				Tags:      out.tags,
			})
			emitted[out.outName] = true
		}
	}

	// --- /api/v2/buckets ---
	if p.cfg.Token != "" {
		bucketCount, err := p.countBuckets()
		if err != nil {
			p.moduleLogger.Warn().Err(err).Msg("influxdb: failed to count buckets")
		} else {
			points = append(points, data_store.DataPoint{
				Name:      "influxdb.buckets",
				Value:     float64(bucketCount),
				Timestamp: now,
				Tags:      []tags.Tag{{Key: "metric_type", Value: "storage"}},
			})
		}
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// checkHealth calls GET /health and returns (up, version).
func (p *InfluxDBProbe) checkHealth() (bool, string) {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint+"/health", nil)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("influxdb: building health request")
		return false, ""
	}
	p.addAuthHeader(req)

	resp, err := p.client.Do(req)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("influxdb: health check failed")
		return false, ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var hr healthResponse
	if jsonErr := json.Unmarshal(body, &hr); jsonErr != nil {
		p.moduleLogger.Warn().Err(jsonErr).Msg("influxdb: parsing health response")
		return false, ""
	}
	return hr.Status == "pass", hr.Version
}

// scrapePrometheusMetrics calls GET /metrics and returns a map of
// metric_name → value for the metrics we care about. Label sets are
// ignored (we take the first occurrence of each bare name).
func (p *InfluxDBProbe) scrapePrometheusMetrics() map[string]float64 {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint+"/metrics", nil)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("influxdb: building metrics request")
		return nil
	}
	p.addAuthHeader(req)

	resp, err := p.client.Do(req)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("influxdb: metrics scrape failed")
		return nil
	}
	defer resp.Body.Close()

	return parsePrometheusText(resp.Body)
}

// parsePrometheusText reads Prometheus text exposition format and
// returns a flat map of metric_name → first-seen value. Lines starting
// with '#' (HELP / TYPE) are skipped.
func parsePrometheusText(r io.Reader) map[string]float64 {
	result := map[string]float64{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: metric_name[{labels}] value [timestamp]
		name, value, ok := parsePromLine(line)
		if !ok {
			continue
		}
		if _, seen := result[name]; !seen {
			result[name] = value
		}
	}
	return result
}

// parsePromLine extracts the bare metric name and its float value from
// a single Prometheus exposition line.
func parsePromLine(line string) (name string, value float64, ok bool) {
	// Strip label set if present: everything up to the first '{'
	bare := line
	if idx := strings.IndexByte(line, '{'); idx >= 0 {
		end := strings.IndexByte(line, '}')
		if end < 0 || end < idx {
			return "", 0, false
		}
		bare = line[:idx] + line[end+1:]
	}
	fields := strings.Fields(bare)
	if len(fields) < 2 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return "", 0, false
	}
	return fields[0], v, true
}

// bucketsResponse is the relevant slice of GET /api/v2/buckets.
type bucketsResponse struct {
	Buckets []struct{} `json:"buckets"`
}

// countBuckets calls GET /api/v2/buckets and returns the bucket count.
func (p *InfluxDBProbe) countBuckets() (int, error) {
	url := fmt.Sprintf("%s/api/v2/buckets", p.cfg.Endpoint)
	if p.cfg.Org != "" {
		url += "?org=" + p.cfg.Org
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("building buckets request: %w", err)
	}
	p.addAuthHeader(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("listing buckets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("buckets API returned HTTP %d", resp.StatusCode)
	}

	var br bucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return 0, fmt.Errorf("decoding buckets response: %w", err)
	}
	return len(br.Buckets), nil
}

// addAuthHeader adds the InfluxDB 2.x Token bearer header when a token
// is configured.
func (p *InfluxDBProbe) addAuthHeader(req *http.Request) {
	if p.cfg.Token != "" {
		req.Header.Set("Authorization", "Token "+p.cfg.Token)
	}
}
