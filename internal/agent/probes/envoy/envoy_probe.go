// Package envoy implements the free envoy probe: scrapes the Envoy
// admin interface (/stats?format=prometheus) to expose downstream
// listener and upstream cluster metrics as OTel-first datapoints.
//
// Envoy exposes hundreds of metrics; this probe focuses on the
// operationally critical signals: server health, listener downstream
// connections/requests, and per-cluster upstream connections/requests.
// The Prometheus-format endpoint is preferred over the colon-separated
// text format because it carries type information and labels.
//
// Cluster-scoped metrics (envoy.cluster.*) carry a "cluster" tag
// derived from the envoy_cluster_name label so that multi-cluster
// deployments produce separate series per cluster.
package envoy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// ProbeType is the stable technical identifier used in YAML config and
// the licence catalogue.
const ProbeType = "envoy"

const (
	defaultEndpoint = "http://localhost:9901"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 30 * time.Second

	// maxBodyBytes caps the stats response to avoid OOM on very large
	// Envoy deployments (tens of thousands of clusters).
	maxBodyBytes = 32 << 20
)

// envoyConfig holds the parsed probe configuration.
type envoyConfig struct {
	Endpoint     string
	Timeout      time.Duration
	Interval     time.Duration
	InstanceName string
}

// EnvoyProbe implements the envoy probe.
type EnvoyProbe struct {
	*types.BaseProbe
	config       envoyConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client

	// collectFunc is the production fetch; overridable in tests.
	collectFunc func() ([]data_store.DataPoint, error)

	entitySrc  *envoyEntitySource
	unregister func()
}

// NewEnvoyProbe is the probe constructor registered in init().
func NewEnvoyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.envoy")

	cfg := envoyConfig{
		Endpoint: defaultEndpoint,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = strings.TrimRight(v, "/")
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

	client := &http.Client{Timeout: cfg.Timeout}

	p := &EnvoyProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		client:       client,
	}
	p.SetProbeType(ProbeType)
	p.collectFunc = p.fetchAndParse

	// Parse addr+port from the endpoint URL for descriptive attributes.
	addr, portStr := endpointHostPort(cfg.Endpoint)
	portInt, _ := strconv.ParseInt(portStr, 10, 64)
	p.entitySrc = newEnvoyEntitySource(cfg.InstanceName, cfg.Endpoint, addr, portInt, client, nil)

	p.SetEntitySource(p.entitySrc)
	return p, nil
}

// endpointHostPort extracts the host and port from an Envoy admin endpoint URL.
// Falls back to the raw endpoint when parsing fails.
func endpointHostPort(endpoint string) (host, port string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint, ""
	}
	h := u.Hostname()
	p := u.Port()
	if h == "" {
		h = endpoint
	}
	return h, p
}

func (p *EnvoyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *EnvoyProbe) ShouldStart() bool          { return true }
func (p *EnvoyProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *EnvoyProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.config.Endpoint).
		Msg("Starting envoy probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *EnvoyProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches Envoy stats and returns OTel-first datapoints.
// If the admin interface is unreachable, senhub.envoy.up=0 is emitted
// and Collect returns nil (a transient error is never a fatal collection
// failure for monitoring probes).
func (p *EnvoyProbe) Collect() ([]data_store.DataPoint, error) {
	points, err := p.collectFunc()
	if err != nil {
		p.moduleLogger.Warn().
			Err(err).
			Str("endpoint", p.config.Endpoint).
			Msg("envoy admin unreachable")
		p.entitySrc.setReachable(false)
		now := time.Now()
		upPoint := data_store.DataPoint{
			Name:      "senhub.envoy.up",
			Value:     0,
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "overview"},
			},
		}
		enriched := p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{upPoint}, p.GetName())
		return enriched, nil
	}
	p.entitySrc.setReachable(true)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchAndParse is the production implementation: GET /stats?format=prometheus
// and parse the Prometheus text exposition.
func (p *EnvoyProbe) fetchAndParse() ([]data_store.DataPoint, error) {
	url := p.config.Endpoint + "/stats?format=prometheus"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil && len(families) == 0 {
		return nil, fmt.Errorf("parsing Envoy stats from %s: %w", url, err)
	}
	// expfmt may return a non-nil error alongside partial results when it
	// encounters unknown types; we keep what was successfully parsed.

	now := time.Now()
	return p.buildDatapoints(families, now), nil
}

// buildDatapoints converts a parsed metric family map into the probe's
// internal datapoint set.
func (p *EnvoyProbe) buildDatapoints(families map[string]*dto.MetricFamily, ts time.Time) []data_store.DataPoint {
	// Helper: first gauge value from a family (no labels filter).
	scalar := func(name string) (float64, bool) {
		f, ok := families[name]
		if !ok || len(f.GetMetric()) == 0 {
			return 0, false
		}
		m := f.GetMetric()[0]
		switch f.GetType() {
		case dto.MetricType_GAUGE:
			return m.GetGauge().GetValue(), true
		case dto.MetricType_COUNTER:
			return m.GetCounter().GetValue(), true
		case dto.MetricType_UNTYPED:
			return m.GetUntyped().GetValue(), true
		}
		return 0, false
	}

	// Helper: sum all time-series values from a family (ignoring labels).
	sumAll := func(name string) (float64, bool) {
		f, ok := families[name]
		if !ok || len(f.GetMetric()) == 0 {
			return 0, false
		}
		var total float64
		var found bool
		for _, m := range f.GetMetric() {
			switch f.GetType() {
			case dto.MetricType_GAUGE:
				total += m.GetGauge().GetValue()
				found = true
			case dto.MetricType_COUNTER:
				total += m.GetCounter().GetValue()
				found = true
			case dto.MetricType_UNTYPED:
				total += m.GetUntyped().GetValue()
				found = true
			}
		}
		return total, found
	}

	// Helper: build a tag slice for overview metrics.
	overviewTags := func() []tags.Tag {
		return []tags.Tag{{Key: "metric_type", Value: "overview"}}
	}

	// Helper: build per-cluster tag slices.
	clusterTags := func(clusterName string) []tags.Tag {
		return []tags.Tag{
			{Key: "metric_type", Value: "cluster"},
			{Key: "cluster", Value: clusterName},
		}
	}

	// Helper: add a scalar datapoint when the metric is present.
	addScalar := func(pts []data_store.DataPoint, metricName string, famName string, t []tags.Tag) []data_store.DataPoint {
		if v, ok := scalar(famName); ok {
			pts = append(pts, data_store.DataPoint{
				Name: metricName, Value: float64(v), Timestamp: ts, Tags: t,
			})
		}
		return pts
	}

	addSum := func(pts []data_store.DataPoint, metricName string, famName string, t []tags.Tag) []data_store.DataPoint {
		if v, ok := sumAll(famName); ok {
			pts = append(pts, data_store.DataPoint{
				Name: metricName, Value: float64(v), Timestamp: ts, Tags: t,
			})
		}
		return pts
	}

	points := []data_store.DataPoint{
		{Name: "senhub.envoy.up", Value: 1, Timestamp: ts, Tags: overviewTags()},
	}

	// Server-level metrics.
	points = addScalar(points, "envoy.server.uptime", "envoy_server_uptime", overviewTags())
	points = addScalar(points, "envoy.server.memory.allocated", "envoy_server_memory_allocated", overviewTags())
	points = addScalar(points, "envoy.server.memory.heap_size", "envoy_server_memory_heap_size", overviewTags())

	// Listener downstream connections — sum across all listener instances.
	points = addSum(points, "envoy.listener.downstream.connections.total", "envoy_listener_downstream_cx_total", overviewTags())
	points = addSum(points, "envoy.listener.downstream.connections.active", "envoy_listener_downstream_cx_active", overviewTags())

	// HTTP downstream requests — sum across all HTTP connections.
	points = addSum(points, "envoy.http.downstream.requests.total", "envoy_http_downstream_rq_total", overviewTags())

	// Per-cluster upstream metrics: one datapoint per envoy_cluster_name label value.
	for _, famName := range []string{
		"envoy_cluster_upstream_cx_total",
		"envoy_cluster_upstream_rq_total",
		"envoy_cluster_upstream_rq_time_sum",
	} {
		fam, ok := families[famName]
		if !ok {
			continue
		}
		for _, m := range fam.GetMetric() {
			clusterName := labelValue(m, "envoy_cluster_name")
			if clusterName == "" {
				continue
			}
			var val float64
			switch fam.GetType() {
			case dto.MetricType_GAUGE:
				val = m.GetGauge().GetValue()
			case dto.MetricType_COUNTER:
				val = m.GetCounter().GetValue()
			case dto.MetricType_UNTYPED:
				val = m.GetUntyped().GetValue()
			default:
				continue
			}

			var metricName string
			switch famName {
			case "envoy_cluster_upstream_cx_total":
				metricName = "envoy.cluster.upstream.connections.total"
			case "envoy_cluster_upstream_rq_total":
				metricName = "envoy.cluster.upstream.requests.total"
			case "envoy_cluster_upstream_rq_time_sum":
				metricName = "envoy.cluster.upstream.requests.time"
			}

			points = append(points, data_store.DataPoint{
				Name:      metricName,
				Value:     float64(val),
				Timestamp: ts,
				Tags:      clusterTags(clusterName),
			})
		}
	}

	return points
}

// labelValue returns the value of the given label from a Prometheus metric,
// or an empty string when the label is absent.
func labelValue(m *dto.Metric, name string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}
