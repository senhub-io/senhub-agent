// Package clickhouse implements the free clickhouse probe: monitors a
// ClickHouse server by scraping its /metrics Prometheus-text endpoint
// (available since ClickHouse 20.1).
//
// ClickHouse exposes three metric families under /metrics:
//   - ClickHouseMetrics_*       instantaneous gauges (active connections, queries, …)
//   - ClickHouseAsyncMetrics_*  background/async gauges (uptime, …)
//   - ClickHouseProfileEvents_* cumulative counters (queries run, bytes written, …)
//
// The probe maps a fixed set of these to OTel-canonical names and always
// emits senhub.clickhouse.up so the pipeline has a health signal even when
// the server is unreachable.
package clickhouse

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in the YAML transformer
// and licence catalogue.
const ProbeType = "clickhouse"

const (
	defaultEndpoint = "http://localhost:8123"
	defaultUsername = "default"
	defaultDatabase = "system"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second

	maxBodyBytes = 8 << 20 // 8 MiB — /metrics can be large on busy clusters
)

// metricMapping links a ClickHouse Prometheus metric name to the OTel-canonical
// internal name the probe emits.
type metricMapping struct {
	clickhouseName string
	internalName   string
	metricType     string // "gauge" or "counter" — overrides the Prom type when set
}

// knownMetrics is the curated set of ClickHouse /metrics series the probe
// collects. Unlisted series are silently ignored.
var knownMetrics = []metricMapping{
	{clickhouseName: "ClickHouseMetrics_Query", internalName: "clickhouse.queries.active", metricType: "gauge"},
	{clickhouseName: "ClickHouseMetrics_Connection", internalName: "clickhouse.connections", metricType: "gauge"},
	{clickhouseName: "ClickHouseMetrics_MemoryTracking", internalName: "clickhouse.memory.used", metricType: "gauge"},
	{clickhouseName: "ClickHouseMetrics_Parts", internalName: "clickhouse.parts.active", metricType: "gauge"},
	{clickhouseName: "ClickHouseMetrics_Merge", internalName: "clickhouse.merges.active", metricType: "gauge"},
	{clickhouseName: "ClickHouseAsyncMetrics_Uptime", internalName: "clickhouse.uptime", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_Query", internalName: "clickhouse.queries.total", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_SelectQuery", internalName: "clickhouse.queries.select", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_InsertQuery", internalName: "clickhouse.queries.insert", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_InsertedRows", internalName: "clickhouse.inserted.rows", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_InsertedBytes", internalName: "clickhouse.inserted.data", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_ReadCompressedBytes", internalName: "clickhouse.read.data", metricType: "counter"},
	{clickhouseName: "ClickHouseProfileEvents_WriteCompressedBytes", internalName: "clickhouse.written.data", metricType: "counter"},
}

// knownIndex maps the ClickHouse Prometheus name to its mapping for O(1) lookup.
var knownIndex map[string]metricMapping

func init() {
	knownIndex = make(map[string]metricMapping, len(knownMetrics))
	for _, m := range knownMetrics {
		knownIndex[m.clickhouseName] = m
	}
}

type probeConfig struct {
	Endpoint     string
	Username     string
	Password     string
	Database     string
	InstanceName string
	Timeout      time.Duration
	Interval     time.Duration
}

// ClickHouseProbe monitors a single ClickHouse server via its /metrics endpoint.
type ClickHouseProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client

	// entitySrc feeds the Toise topology inventory (db.clickhouse entity).
	entitySrc  *clickhouseEntitySource
	unregister func()
}

// NewClickHouseProbe constructs the probe. Config errors surface here.
func NewClickHouseProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.clickhouse")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &ClickHouseProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
	probe.SetProbeType(ProbeType)
	probe.entitySrc = newClickhouseEntitySource(cfg.InstanceName, cfg.Endpoint)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint: defaultEndpoint,
		Username: defaultUsername,
		Database: defaultDatabase,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := config["database"].(string); ok && v != "" {
		cfg.Database = v
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

	return cfg, nil
}

func (p *ClickHouseProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ClickHouseProbe) ShouldStart() bool          { return true }
func (p *ClickHouseProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *ClickHouseProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.config.Endpoint).
		Str("username", p.config.Username).
		Msg("Starting clickhouse probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *ClickHouseProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect scrapes the /metrics endpoint and maps the known ClickHouse series
// to OTel-canonical names. A failing scrape is recorded as up=0; Collect
// always returns nil so the framework does not mark the probe unhealthy.
//
// On the first successful scrape, Collect also fetches the server UUID via
// SELECT serverUUID() and pins the db entity id ("clickhouse:<uuid>"). Once
// pinned the id is immutable. When the server does not expose a UUID (pre-
// 21.x or the query fails) the entity source falls back to host:port on the
// next Observe call, which is the documented db degraded fallback.
func (p *ClickHouseProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "instance", Value: p.config.Endpoint},
		{Key: "metric_type", Value: "overview"},
	}

	up := float64(1)
	families, err := p.fetchMetrics()
	if err != nil {
		up = 0
		p.entitySrc.setReachable(false, "")
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.config.Endpoint).Msg("clickhouse scrape failed")
	} else {
		// Try to pin the instance id on the first successful collect.
		// isPinned() is a no-op check when instance_name was already set.
		if !p.entitySrc.isPinned() {
			uuid, uuidErr := p.fetchServerUUID()
			if uuidErr != nil {
				p.moduleLogger.Debug().Err(uuidErr).Msg("clickhouse serverUUID unavailable; falling back to host:port")
			}
			// pinTechID accepts an empty uuid and falls back to host:port — the
			// documented db degraded fallback when no stable tech id is available.
			p.entitySrc.pinTechID(uuid)
		}
		p.entitySrc.setReachable(true, "")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.clickhouse.up", Value: up, Timestamp: now, Tags: baseTags},
	}

	if err == nil {
		for chName, family := range families {
			mapping, ok := knownIndex[chName]
			if !ok {
				continue
			}
			value, ok := extractScalar(family)
			if !ok {
				continue
			}
			points = append(points, data_store.DataPoint{
				Name:      mapping.internalName,
				Value:     float64(value),
				Timestamp: now,
				Tags:      baseTags,
			})
		}
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchMetrics GETs <endpoint>/metrics with Basic Auth if credentials are set.
func (p *ClickHouseProbe) fetchMetrics() (map[string]*dto.MetricFamily, error) {
	metricsURL := p.config.Endpoint + "/metrics"

	req, err := http.NewRequest(http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", metricsURL, err)
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	if p.config.Username != "" || p.config.Password != "" {
		req.SetBasicAuth(p.config.Username, p.config.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", metricsURL, resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("parsing /metrics from %s: %w", p.config.Endpoint, err)
	}
	return families, nil
}

// fetchServerUUID queries SELECT serverUUID() through the ClickHouse HTTP
// interface and returns the raw UUID string (e.g.
// "a1b2c3d4-e5f6-7890-abcd-ef1234567890"). Returns "" + non-nil error when
// the server is unreachable or does not support serverUUID() (pre-21.x).
// The caller pins the result via entitySrc.pinTechID.
func (p *ClickHouseProbe) fetchServerUUID() (string, error) {
	queryURL := p.config.Endpoint + "/"
	req, err := http.NewRequest(http.MethodGet, queryURL, nil)
	if err != nil {
		return "", fmt.Errorf("building serverUUID request: %w", err)
	}
	q := url.Values{}
	q.Set("query", "SELECT serverUUID()")
	req.URL.RawQuery = q.Encode()
	if p.config.Username != "" || p.config.Password != "" {
		req.SetBasicAuth(p.config.Username, p.config.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("serverUUID GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("serverUUID: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", fmt.Errorf("serverUUID read body: %w", err)
	}
	uuid := strings.TrimSpace(string(body))
	if uuid == "" {
		return "", fmt.Errorf("serverUUID returned empty response")
	}
	return uuid, nil
}

// extractScalar returns the single numeric value from a MetricFamily that
// holds exactly one gauge, counter, or untyped series. Returns false when
// the family is empty or has an unsupported type (histogram, summary).
func extractScalar(family *dto.MetricFamily) (float64, bool) {
	metrics := family.GetMetric()
	if len(metrics) == 0 {
		return 0, false
	}
	m := metrics[0]
	switch family.GetType() {
	case dto.MetricType_GAUGE:
		return m.GetGauge().GetValue(), true
	case dto.MetricType_COUNTER:
		return m.GetCounter().GetValue(), true
	case dto.MetricType_UNTYPED:
		return m.GetUntyped().GetValue(), true
	default:
		return 0, false
	}
}
