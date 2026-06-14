// Package pulsar implements the free pulsar probe: broker-level health
// and throughput metrics from Apache Pulsar's Admin REST API and its
// Prometheus /metrics endpoint. The probe covers the key operational
// signals — availability, messaging rates, storage, and consumer backlog
// — that an operator needs to know the broker is healthy and not falling
// behind.
//
// Data sources:
//
//   - GET <endpoint>/admin/v2/brokers/ready — availability check.
//     A 200 response means the broker is ready.
//
//   - GET <endpoint>/metrics — Prometheus text exposition.
//     The probe extracts a fixed set of well-known Pulsar broker metrics
//     from the exposition rather than ingesting all series, keeping the
//     cardinality predictable.
//
// OTel-first: every internal metric name follows OTel conventions.
// pulsar.* names mirror the Pulsar naming; senhub.pulsar.up is the
// custom availability gauge. PRTG channels are derived from the YAML
// transformer, not hardcoded here.
package pulsar

import (
	"bufio"
	"context"
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

// ProbeType is the stable technical identifier used in YAML config,
// transformer file names, and license JWT claims.
const ProbeType = "pulsar"

const (
	defaultEndpoint = "http://localhost:8080"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
	// maxBodyBytes caps the /metrics response; a runaway exporter must
	// not OOM the agent. 8 MiB is ample for broker-level aggregates.
	maxBodyBytes = 8 << 20
)

// pulsarMetricNames is the set of Prometheus metric names the probe
// extracts from the /metrics exposition. Only broker-level aggregates
// are listed; per-topic time series are intentionally excluded to keep
// cardinality flat and predictable.
var pulsarMetricNames = map[string]string{
	"pulsar_topics_count":       "pulsar.topics.count",
	"pulsar_producers_count":    "pulsar.producers.count",
	"pulsar_consumers_count":    "pulsar.consumers.count",
	"pulsar_rate_in":            "pulsar.rate.messages.in",
	"pulsar_rate_out":           "pulsar.rate.messages.out",
	"pulsar_throughput_in":      "pulsar.throughput.in",
	"pulsar_throughput_out":     "pulsar.throughput.out",
	"pulsar_storage_size":       "pulsar.storage.size",
	"pulsar_msg_backlog":        "pulsar.message.backlog",
	"pulsar_storage_read_rate":  "pulsar.storage.read.rate",
	"pulsar_storage_write_rate": "pulsar.storage.write.rate",
}

type probeConfig struct {
	Endpoint string
	Timeout  time.Duration
	Interval time.Duration
}

// PulsarProbe collects broker metrics from Apache Pulsar.
type PulsarProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *pulsarEntitySource
	unregister   func()
}

// NewPulsarProbe constructs the probe. Config errors surface here.
func NewPulsarProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.pulsar")

	cfg := probeConfig{
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

	probe := &PulsarProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		entitySrc: newPulsarEntitySource(cfg.Endpoint),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *PulsarProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *PulsarProbe) ShouldStart() bool          { return true }
func (p *PulsarProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *PulsarProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.config.Endpoint).
		Msg("Starting pulsar probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *PulsarProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect checks broker readiness, then scrapes the /metrics endpoint
// for the tracked subset of broker-level metrics. A failing broker is
// a measurement (senhub.pulsar.up = 0), never a collection error —
// the pipeline always receives a datapoint set.
func (p *PulsarProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "endpoint", Value: p.config.Endpoint},
		{Key: "metric_type", Value: "broker"},
	}

	up := p.checkReady()
	p.entitySrc.setReachable(up)
	upVal := float32(0)
	if up {
		upVal = 1
	}

	points := []data_store.DataPoint{
		{Name: "senhub.pulsar.up", Value: upVal, Timestamp: now, Tags: baseTags},
	}

	if up {
		scraped, err := p.scrapeMetrics(now, baseTags)
		if err != nil {
			p.moduleLogger.Warn().
				Err(err).
				Str("endpoint", p.config.Endpoint).
				Msg("pulsar /metrics scrape failed")
		} else {
			points = append(points, scraped...)
		}
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// checkReady performs GET <endpoint>/admin/v2/brokers/ready and returns
// true when the broker responds with HTTP 200.
func (p *PulsarProbe) checkReady() bool {
	url := p.config.Endpoint + "/admin/v2/brokers/ready"
	resp, err := p.client.Get(url) // #nosec G107 - endpoint is operator-configured
	if err != nil {
		p.moduleLogger.Warn().
			Err(err).
			Str("url", url).
			Msg("pulsar readiness check failed")
		return false
	}
	defer resp.Body.Close()
	// Drain body so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK
}

// scrapeMetrics fetches GET <endpoint>/metrics (Prometheus text) and
// extracts the broker-level series listed in pulsarMetricNames.
// Labels on the scraped series become tags on the datapoint; this lets
// PRTG/Prom/OTLP consumers filter by namespace or topic when Pulsar
// annotates its broker-level aggregates.
func (p *PulsarProbe) scrapeMetrics(ts time.Time, baseTags []tags.Tag) ([]data_store.DataPoint, error) {
	url := p.config.Endpoint + "/metrics"
	resp, err := p.client.Get(url) // #nosec G107 - endpoint is operator-configured
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %d", url, resp.StatusCode)
	}

	return parsePrometheusText(io.LimitReader(resp.Body, maxBodyBytes), ts, baseTags)
}

// parsePrometheusText reads a Prometheus text exposition line-by-line
// and returns datapoints for the metric names in pulsarMetricNames.
// It handles the simple "name{labels} value" form that Pulsar uses for
// broker-level aggregates; histograms and summaries are ignored.
//
// Using a hand-rolled line scanner instead of the prometheus/common
// parser avoids adding a new dependency for what is essentially a
// fixed-set extraction of flat gauges. The format is stable (Pulsar
// 2.x+) and the probe only needs the numeric VALUE; it does not need
// type/help metadata.
func parsePrometheusText(r io.Reader, ts time.Time, baseTags []tags.Tag) ([]data_store.DataPoint, error) {
	var points []data_store.DataPoint
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Prometheus text line: name[{labels}] value [timestamp]
		// Split on the first space or '{' to extract the metric name.
		var name, labelStr, valueStr string

		if brace := strings.IndexByte(line, '{'); brace >= 0 {
			// Labels present: name{...} value
			name = strings.TrimSpace(line[:brace])
			rest := line[brace+1:]
			close := strings.IndexByte(rest, '}')
			if close < 0 {
				continue
			}
			labelStr = rest[:close]
			valueStr = strings.TrimSpace(rest[close+1:])
		} else {
			// No labels: "name value [timestamp]"
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			name = fields[0]
			valueStr = fields[1]
		}

		otelName, tracked := pulsarMetricNames[name]
		if !tracked {
			continue
		}

		// value field may be followed by a timestamp; take only the first token.
		if sp := strings.IndexByte(valueStr, ' '); sp >= 0 {
			valueStr = valueStr[:sp]
		}
		val, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}

		dpTags := make([]tags.Tag, len(baseTags))
		copy(dpTags, baseTags)
		dpTags = appendLabelTags(dpTags, labelStr)

		points = append(points, data_store.DataPoint{
			Name:      otelName,
			Value:     float32(val),
			Timestamp: ts,
			Tags:      dpTags,
		})
	}

	if err := scanner.Err(); err != nil {
		return points, fmt.Errorf("reading metrics exposition: %w", err)
	}
	return points, nil
}

// appendLabelTags parses a Prometheus label set string of the form
// key="value",key2="value2" and appends each pair as a tag.
func appendLabelTags(t []tags.Tag, labelStr string) []tags.Tag {
	if labelStr == "" {
		return t
	}
	for _, pair := range strings.Split(labelStr, ",") {
		pair = strings.TrimSpace(pair)
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if k == "" || v == "" {
			continue
		}
		t = append(t, tags.Tag{Key: k, Value: v})
	}
	return t
}
