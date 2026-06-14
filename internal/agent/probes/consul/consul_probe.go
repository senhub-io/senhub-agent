// Package consul implements the free consul probe: monitoring of a
// Consul agent via its HTTP API (v1/agent/metrics, v1/agent/self,
// v1/health/state/*) — service discovery health, raft performance,
// RPC/DNS counters, cluster member count, leader status.
//
// Metric parsing follows two paths:
//   - Prometheus text exposition from /v1/agent/metrics?format=prometheus
//     (Consul 1.1+) for counters and gauges.
//   - JSON from /v1/health/state/* for per-state check counts and from
//     /v1/agent/self for node metadata and leader status.
//
// A failing agent is always represented as senhub.consul.up=0 and
// Collect never returns an error — a probe that cannot reach Consul
// is itself a measurement.
package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// licence catalogues.
const ProbeType = "consul"

const (
	defaultEndpoint = "http://localhost:8500"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 30 * time.Second

	// maxBodyBytes caps API response bodies so a misbehaving Consul
	// node cannot OOM the agent.
	maxBodyBytes = 4 << 20 // 4 MiB
)

// healthStates are the three Consul check states.
var healthStates = []string{"critical", "warning", "passing"}

// consulConfig holds validated configuration for a consul probe instance.
type consulConfig struct {
	Endpoint string
	Token    string
	Timeout  time.Duration
	Interval time.Duration
}

// agentSelf is the subset of /v1/agent/self we consume.
type agentSelf struct {
	Config struct {
		Version string `json:"Version"`
	} `json:"Config"`
	Stats struct {
		Consul struct {
			Leader string `json:"leader"`
		} `json:"consul"`
	} `json:"Stats"`
}

// ConsulProbe collects metrics from a single Consul agent HTTP API.
type ConsulProbe struct {
	*types.BaseProbe
	cfg          consulConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	// entity rail (Toise topology)
	entitySrc  *consulEntitySource
	unregister func()
}

// NewConsulProbe is the constructor registered in the probe catalogue.
// It returns an error only for invalid configuration; connection
// failures are handled in Collect.
func NewConsulProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.consul")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &ConsulProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
	p.SetProbeType(ProbeType)

	// Entity rail: extract host and port from the endpoint URL.
	addr, port := endpointHostPort(cfg.Endpoint)
	p.entitySrc = newConsulEntitySource(addr, port)

	return p, nil
}

// endpointHostPort splits an HTTP endpoint URL into host and port strings.
// If no port is present in the URL, defaults to "8500" (Consul default).
func endpointHostPort(endpoint string) (host, port string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint, "8500"
	}
	h := u.Hostname()
	p := u.Port()
	if h == "" {
		h = "localhost"
	}
	if p == "" {
		p = "8500"
	}
	return h, p
}

func parseConfig(config map[string]interface{}) (consulConfig, error) {
	cfg := consulConfig{
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
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	return cfg, nil
}

func (p *ConsulProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ConsulProbe) ShouldStart() bool          { return true }
func (p *ConsulProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *ConsulProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting consul probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *ConsulProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches all Consul API endpoints and assembles datapoints.
// It always emits senhub.consul.up and returns nil — a connection
// failure is a measurement, not a collection error.
func (p *ConsulProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "metric_type", Value: "overview"},
	}

	// Always emit the up metric.
	up := float32(1)

	var points []data_store.DataPoint
	var version string

	// 1. Prometheus metrics from /v1/agent/metrics?format=prometheus
	promPoints, err := p.collectPrometheusMetrics(now, baseTags)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).
			Msg("consul: failed to fetch prometheus metrics")
		up = 0
	} else {
		points = append(points, promPoints...)
	}

	// 2. Leader status and version from /v1/agent/self
	selfPoints, ver, err := p.collectAgentSelf(now, baseTags)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).
			Msg("consul: failed to fetch agent/self")
		up = 0
	} else {
		version = ver
		points = append(points, selfPoints...)
	}

	// 3. Health check counts per state; also collect healthy service names for
	// the entity topology snapshot.
	healthPoints, services, err := p.collectHealthChecks(now)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).
			Msg("consul: failed to fetch health states")
		up = 0
	} else {
		points = append(points, healthPoints...)
	}

	// Update the entity rail.
	if up == 1 {
		p.entitySrc.setReachable(true, version)
		p.entitySrc.updateSnapshot(services)
	} else {
		p.entitySrc.setReachable(false, "")
	}

	// Prepend the up metric so it is always first.
	all := make([]data_store.DataPoint, 0, 1+len(points))
	all = append(all, data_store.DataPoint{
		Name:      "senhub.consul.up",
		Value:     up,
		Timestamp: now,
		Tags:      baseTags,
	})
	all = append(all, points...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(all, p.GetName()), nil
}

// get issues an authenticated GET request to the given URL and returns
// the response body, capped at maxBodyBytes. The caller must close the
// body via the returned ReadCloser — here we return raw bytes directly
// to simplify callers.
func (p *ConsulProbe) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	if p.cfg.Token != "" {
		req.Header.Set("X-Consul-Token", p.cfg.Token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}
	return body, nil
}

// collectPrometheusMetrics parses /v1/agent/metrics?format=prometheus and
// extracts the specific Consul metrics we expose.
func (p *ConsulProbe) collectPrometheusMetrics(now time.Time, base []tags.Tag) ([]data_store.DataPoint, error) {
	url := p.cfg.Endpoint + "/v1/agent/metrics?format=prometheus"
	body, err := p.get(url)
	if err != nil {
		return nil, err
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(string(body)))
	if err != nil {
		// Prometheus parser returns partial results + error on some edge
		// cases; proceed with what we got.
		p.moduleLogger.Warn().Err(err).Msg("consul: partial prometheus parse")
	}

	var points []data_store.DataPoint

	// consul_catalog_registered_services → consul.catalog.services
	if fam, ok := families["consul_catalog_registered_services"]; ok {
		for _, m := range fam.GetMetric() {
			points = append(points, data_store.DataPoint{
				Name:      "consul.catalog.services",
				Value:     float32(m.GetGauge().GetValue()),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	// consul_serf_lan_members → consul.serf.members
	if fam, ok := families["consul_serf_lan_members"]; ok {
		for _, m := range fam.GetMetric() {
			points = append(points, data_store.DataPoint{
				Name:      "consul.serf.members",
				Value:     float32(m.GetGauge().GetValue()),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	// consul_raft_commitTime_sum / consul_raft_commitTime_count → consul.raft.commit.time (mean, ms)
	sumFam, hasSum := families["consul_raft_commitTime_sum"]
	cntFam, hasCnt := families["consul_raft_commitTime_count"]
	if hasSum && hasCnt && len(sumFam.GetMetric()) > 0 && len(cntFam.GetMetric()) > 0 {
		sum := sumFam.GetMetric()[0].GetGauge().GetValue()
		cnt := cntFam.GetMetric()[0].GetGauge().GetValue()
		if cnt > 0 {
			points = append(points, data_store.DataPoint{
				Name:      "consul.raft.commit.time",
				Value:     float32(sum / cnt),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	// consul_rpc_request → consul.rpc.requests (counter pass-through)
	if fam, ok := families["consul_rpc_request"]; ok {
		for _, m := range fam.GetMetric() {
			points = append(points, data_store.DataPoint{
				Name:      "consul.rpc.requests",
				Value:     float32(m.GetCounter().GetValue()),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	// consul_dns_domain_query_count → consul.dns.queries (counter)
	if fam, ok := families["consul_dns_domain_query_count"]; ok {
		for _, m := range fam.GetMetric() {
			points = append(points, data_store.DataPoint{
				Name:      "consul.dns.queries",
				Value:     float32(m.GetCounter().GetValue()),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	return points, nil
}

// collectAgentSelf parses /v1/agent/self and emits consul.leader.
// It also returns the Consul version string for the entity snapshot.
func (p *ConsulProbe) collectAgentSelf(now time.Time, base []tags.Tag) ([]data_store.DataPoint, string, error) {
	u := p.cfg.Endpoint + "/v1/agent/self"
	body, err := p.get(u)
	if err != nil {
		return nil, "", err
	}

	var self agentSelf
	if err := json.Unmarshal(body, &self); err != nil {
		return nil, "", fmt.Errorf("parsing /v1/agent/self: %w", err)
	}

	leader := float32(0)
	if self.Stats.Consul.Leader == "true" {
		leader = 1
	}

	return []data_store.DataPoint{{
		Name:      "consul.leader",
		Value:     leader,
		Timestamp: now,
		Tags:      base,
	}}, self.Config.Version, nil
}

// healthCheck is the minimal shape of a Consul health-check JSON object.
// Only ServiceName is needed for the entity topology snapshot.
type healthCheck struct {
	ServiceName string `json:"ServiceName"`
}

// collectHealthChecks calls /v1/health/state/{state} for each of
// critical, warning, passing and emits consul.health.checks with a
// state tag. It also returns the deduplicated list of passing service names
// for the entity topology snapshot.
func (p *ConsulProbe) collectHealthChecks(now time.Time) ([]data_store.DataPoint, []string, error) {
	var points []data_store.DataPoint
	seen := map[string]bool{}
	var services []string

	for _, state := range healthStates {
		u := p.cfg.Endpoint + "/v1/health/state/" + state
		body, err := p.get(u)
		if err != nil {
			return nil, nil, fmt.Errorf("health state %s: %w", state, err)
		}

		var checks []healthCheck
		if err := json.Unmarshal(body, &checks); err != nil {
			return nil, nil, fmt.Errorf("parsing health state %s: %w", state, err)
		}

		points = append(points, data_store.DataPoint{
			Name:  "consul.health.checks",
			Value: float32(len(checks)),
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "health"},
				{Key: "state", Value: state},
			},
		})

		// Collect passing service names for topology; skip the node-level
		// check (empty ServiceName means it is a node check, not a service).
		if state == "passing" {
			for _, c := range checks {
				if c.ServiceName != "" && !seen[c.ServiceName] {
					seen[c.ServiceName] = true
					services = append(services, c.ServiceName)
				}
			}
		}
	}

	return points, services, nil
}
