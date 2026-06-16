// Package promscrape implements the free prometheus_scrape probe: the
// agent scrapes Prometheus /metrics endpoints and ingests the samples
// through the existing pipeline, the same edge-collector posture as
// otlp_receiver but for the pull side (#304). Many appliances and
// exporters only expose Prometheus text format; without this the
// universal-collection story has a hole that Telegraf and the OTel
// Collector both cover.
//
// Mapping: typed pass-through. Every sample keeps its scraped name and
// labels; the otel_type tag carries counter/gauge semantics to the
// mapper (same mechanism as snmp_poll dynamic OIDs, #207). Histogram
// and summary families are dropped and counted, mirroring
// otlp_receiver's scalar-only contract.
//
// Shares the bounded multi-target fan-out chassis with the active
// checks: a failing target is a measurement (up=0), never a collection
// error.
package promscrape

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "prometheus_scrape"

const (
	defaultTimeout     = 10 * time.Second
	defaultInterval    = 60 * time.Second
	maxParallelTargets = 8
	// maxBodyBytes caps a scrape response; a runaway exporter must not
	// OOM the agent. 32 MiB holds ~100k series of text exposition.
	maxBodyBytes = 32 << 20
)

// sample is one scraped series flattened from a metric family.
type sample struct {
	name     string
	labels   map[string]string
	value    float64
	otelType string // "counter" or "gauge"
}

// scrapeResult is the per-target outcome the datapoint builder consumes.
type scrapeResult struct {
	target   string
	samples  []sample
	dropped  int // series in unsupported families (histogram/summary)
	duration time.Duration
	err      error
}

type scrapeFunc func(target string) scrapeResult

type PromScrapeProbe struct {
	*types.BaseProbe
	config       scrapeConfig
	moduleLogger *logger.ModuleLogger
	scrape       scrapeFunc
	client       *http.Client
	metricRe     *regexp.Regexp
}

type scrapeConfig struct {
	Targets            []string
	Interval           time.Duration
	Timeout            time.Duration
	InsecureSkipVerify bool
	BearerToken        string
	MetricMatch        string
}

// NewPromScrapeProbe constructs the probe. Config errors surface here.
func NewPromScrapeProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.prometheus_scrape")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	var metricRe *regexp.Regexp
	if cfg.MetricMatch != "" {
		metricRe, err = regexp.Compile(cfg.MetricMatch)
		if err != nil {
			return nil, fmt.Errorf("prometheus_scrape metric_match is not a valid regexp: %w", err)
		}
	}

	probe := &PromScrapeProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		metricRe:     metricRe,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 - operator opt-in for self-signed exporters
				},
			},
		},
	}
	probe.SetProbeType(ProbeType)
	probe.scrape = probe.scrapeOnce
	return probe, nil
}

func parseConfig(config map[string]interface{}) (scrapeConfig, error) {
	cfg := scrapeConfig{
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	raw, ok := config["targets"]
	if !ok {
		return cfg, fmt.Errorf("prometheus_scrape requires a targets list")
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok || s == "" {
				return cfg, fmt.Errorf("prometheus_scrape targets must be non-empty URL strings (got %T)", item)
			}
			cfg.Targets = append(cfg.Targets, s)
		}
	case []string:
		cfg.Targets = v
	default:
		return cfg, fmt.Errorf("prometheus_scrape targets must be a list (got %T)", raw)
	}
	if len(cfg.Targets) == 0 {
		return cfg, fmt.Errorf("prometheus_scrape requires at least one target")
	}

	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["insecure_skip_verify"].(bool); ok {
		cfg.InsecureSkipVerify = v
	}
	if v, ok := config["bearer_token"].(string); ok {
		cfg.BearerToken = v
	}
	if v, ok := config["metric_match"].(string); ok {
		cfg.MetricMatch = v
	}
	return cfg, nil
}

func (p *PromScrapeProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *PromScrapeProbe) ShouldStart() bool          { return true }
func (p *PromScrapeProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *PromScrapeProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("targets", p.config.Targets).
		Msg("Starting prometheus_scrape probe")
	for _, t := range p.config.Targets {
		if looksLikeOwnEndpoint(t) {
			p.moduleLogger.Warn().
				Str("target", t).
				Msg("prometheus_scrape target looks like the agent's own Prometheus endpoint; scraping it re-ingests already-namespaced series and is almost never intended")
		}
	}
	return nil
}

// looksLikeOwnEndpoint flags a target whose path matches the agent's own
// Prometheus exposition routes (/api/<key>/prometheus/metrics or the bare
// /metrics alias). A heuristic warning only — third-party exporters also use
// /metrics, so this never refuses the scrape; it just surfaces the footgun.
func looksLikeOwnEndpoint(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	return strings.HasSuffix(path, "/prometheus/metrics") || path == "/metrics"
}

func (p *PromScrapeProbe) OnShutdown(ctx context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect scrapes every target (bounded parallelism). A failing target
// is a measurement (up=0), never a collection error.
func (p *PromScrapeProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	results := make([]scrapeResult, len(p.config.Targets))

	sem := make(chan struct{}, maxParallelTargets)
	var wg sync.WaitGroup
	for i, target := range p.config.Targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.scrape(target)
		}(i, target)
	}
	wg.Wait()

	var points []data_store.DataPoint
	for _, res := range results {
		points = append(points, p.buildDatapoints(res, now)...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// buildDatapoints converts one scrape outcome into datapoints: the
// scraped series as typed pass-throughs plus the probe's self-metrics.
func (p *PromScrapeProbe) buildDatapoints(res scrapeResult, ts time.Time) []data_store.DataPoint {
	selfTags := []tags.Tag{
		{Key: "target", Value: res.target},
		{Key: "metric_type", Value: "scrape"},
	}

	up := float64(1)
	if res.err != nil {
		up = 0
		p.moduleLogger.Warn().
			Err(res.err).
			Str("target", res.target).
			Msg("prometheus_scrape target failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.promscrape.up", Value: up, Timestamp: ts, Tags: selfTags},
	}
	if res.err == nil {
		points = append(points,
			data_store.DataPoint{Name: "senhub.promscrape.scrape.duration", Value: res.duration.Seconds() * 1000, Timestamp: ts, Tags: selfTags},
			data_store.DataPoint{Name: "senhub.promscrape.samples", Value: float64(len(res.samples)), Timestamp: ts, Tags: selfTags},
			data_store.DataPoint{Name: "senhub.promscrape.dropped", Value: float64(res.dropped), Timestamp: ts, Tags: selfTags},
		)
	}

	for _, s := range res.samples {
		t := make([]tags.Tag, 0, len(s.labels)+3)
		t = append(t,
			tags.Tag{Key: "target", Value: res.target},
			tags.Tag{Key: "metric_type", Value: "scrape"},
			tags.Tag{Key: "otel_type", Value: s.otelType},
		)
		for k, v := range s.labels {
			if v == "" {
				continue
			}
			t = append(t, tags.Tag{Key: k, Value: v})
		}
		points = append(points, data_store.DataPoint{
			Name:      s.name,
			Value:     s.value,
			Timestamp: ts,
			Tags:      t,
		})
	}
	return points
}

// scrapeOnce is the production scrapeFunc: one HTTP GET + text parse.
func (p *PromScrapeProbe) scrapeOnce(target string) scrapeResult {
	res := scrapeResult{target: target}

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		res.err = fmt.Errorf("building scrape request for %s: %w", target, err)
		return res
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	if p.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.BearerToken)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		res.err = fmt.Errorf("scraping %s: %w", target, err)
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.err = fmt.Errorf("scraping %s: unexpected status %d", target, resp.StatusCode)
		return res
	}

	// The zero-value TextParser carries an unset validation scheme and
	// panics on the first name; the constructor is mandatory. UTF-8 is
	// the prometheus/common >= 0.60 default.
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(io.LimitReader(resp.Body, maxBodyBytes))
	res.duration = time.Since(start)
	if err != nil {
		res.err = fmt.Errorf("parsing exposition from %s: %w", target, err)
		return res
	}

	for name, family := range families {
		if p.metricRe != nil && !p.metricRe.MatchString(name) {
			continue
		}
		switch family.GetType() {
		case dto.MetricType_COUNTER:
			for _, m := range family.GetMetric() {
				res.samples = append(res.samples, sample{
					name: name, labels: labelMap(m), value: m.GetCounter().GetValue(), otelType: "counter",
				})
			}
		case dto.MetricType_GAUGE:
			for _, m := range family.GetMetric() {
				res.samples = append(res.samples, sample{
					name: name, labels: labelMap(m), value: m.GetGauge().GetValue(), otelType: "gauge",
				})
			}
		case dto.MetricType_UNTYPED:
			for _, m := range family.GetMetric() {
				res.samples = append(res.samples, sample{
					name: name, labels: labelMap(m), value: m.GetUntyped().GetValue(), otelType: "gauge",
				})
			}
		default:
			// Histogram and summary series are dropped and counted,
			// mirroring otlp_receiver's scalar-only contract.
			res.dropped += len(family.GetMetric())
		}
	}
	return res
}

func labelMap(m *dto.Metric) map[string]string {
	if len(m.GetLabel()) == 0 {
		return nil
	}
	labels := make(map[string]string, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		labels[l.GetName()] = l.GetValue()
	}
	return labels
}
