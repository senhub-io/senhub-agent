// Package nats implements the free nats probe: NATS Server monitoring via the
// HTTP management API (/varz, /routez, /jsz). No external dependencies — stdlib
// only (net/http + encoding/json).
//
// Monitored endpoints:
//   - /varz  — connections, subscriptions, messages and bytes in/out,
//     slow consumers
//   - /routez — cluster route count
//   - /jsz   — JetStream streams, consumers, messages and bytes (when enabled)
//
// A NATS server exposing its management port (default 8222) with no auth is
// the common dev/small-cluster setup; the probe follows that convention.
// Enterprise auth is out of scope for this FREE-tier probe.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable canonical identifier used in YAML configs and
// the license catalogue. It MUST NOT change after release.
const ProbeType = "nats"

const (
	defaultEndpoint = "http://localhost:8222"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

// probeConfig holds validated configuration.
type probeConfig struct {
	Endpoint     string
	Interval     time.Duration
	InstanceName string // optional operator-supplied override for service.instance.id
}

// varzResponse is a partial mapping of the /varz JSON object. Only the fields
// this probe exposes are decoded; the rest are ignored.
type varzResponse struct {
	ServerName       string `json:"server_name"`
	ServerID         string `json:"server_id"`
	Connections      int64  `json:"connections"`
	TotalConnections int64  `json:"total_connections"`
	Subscriptions    int64  `json:"subscriptions"`
	InMsgs           int64  `json:"in_msgs"`
	OutMsgs          int64  `json:"out_msgs"`
	InBytes          int64  `json:"in_bytes"`
	OutBytes         int64  `json:"out_bytes"`
	SlowConsumers    int64  `json:"slow_consumers"`
}

// routezResponse is a partial mapping of /routez.
type routezResponse struct {
	NumRoutes int `json:"num_routes"`
}

// jszResponse is a partial mapping of /jsz.
type jszResponse struct {
	Streams   int   `json:"streams"`
	Consumers int   `json:"consumers"`
	Messages  int64 `json:"messages"`
	Bytes     int64 `json:"bytes"`
}

// NATSProbe collects metrics from a NATS Server management API.
type NATSProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *natsEntitySource

	// fetch is the HTTP GET function, replaceable in tests.
	fetch func(path string) ([]byte, int, error)

	// unregisterEntity detaches the entity source on shutdown.
	unregisterEntity func()
}

// NewNATSProbe builds a nats probe from its raw params block.
func NewNATSProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.nats")

	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: defaultTimeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	p := &NATSProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       client,
		entitySrc:    newNATSEntitySource(cfg.Endpoint, cfg.InstanceName),
	}
	p.SetProbeType(ProbeType)
	p.fetch = p.httpGet
	p.SetEntitySource(p.entitySrc)
	return p, nil
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint: defaultEndpoint,
		Interval: defaultInterval,
	}

	if v, ok := raw["endpoint"].(string); ok && v != "" {
		// Validate the URL is parseable.
		if _, err := url.Parse(v); err != nil {
			return cfg, fmt.Errorf("nats: invalid endpoint %q: %w", v, err)
		}
		cfg.Endpoint = v
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	return cfg, nil
}

func (p *NATSProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *NATSProbe) ShouldStart() bool          { return true }
func (p *NATSProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *NATSProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntity = entity.RegisterSource(p.entitySrc)
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("NATS probe started")
	return nil
}

func (p *NATSProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntity != nil {
		p.unregisterEntity()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches /varz, /routez, and /jsz from the NATS management API and
// builds the metric set. A failure on /varz (primary endpoint) marks the
// server as down; /routez and /jsz failures are tolerated (partial data).
func (p *NATSProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	baseTags := []tags.Tag{
		{Key: "instance", Value: p.cfg.Endpoint},
		{Key: "metric_type", Value: "overview"},
	}

	var points []data_store.DataPoint

	// --- /varz (required) --------------------------------------------------
	varzBody, status, varzErr := p.fetch("/varz")
	up := float64(0)
	if varzErr == nil && status == http.StatusOK {
		up = 1
	}
	points = append(points, data_store.DataPoint{
		Name: "senhub.nats.up", Value: up, Timestamp: now, Tags: baseTags,
	})

	if varzErr != nil {
		p.moduleLogger.Warn().Err(varzErr).
			Str("endpoint", p.cfg.Endpoint).
			Msg("NATS /varz request failed; emitting up=0")
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	var varz varzResponse
	if err := json.Unmarshal(varzBody, &varz); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("NATS /varz JSON decode failed")
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	connTags := tagsWithType(baseTags, "connections")
	points = append(points,
		data_store.DataPoint{Name: "nats.connections.count", Value: float64(varz.Connections), Timestamp: now, Tags: connTags},
		data_store.DataPoint{Name: "nats.connections.total", Value: float64(varz.TotalConnections), Timestamp: now, Tags: connTags},
	)

	subTags := tagsWithType(baseTags, "subscriptions")
	points = append(points,
		data_store.DataPoint{Name: "nats.subscriptions.count", Value: float64(varz.Subscriptions), Timestamp: now, Tags: subTags},
	)

	msgTags := tagsWithType(baseTags, "throughput")
	points = append(points,
		data_store.DataPoint{Name: "nats.messages.in", Value: float64(varz.InMsgs), Timestamp: now, Tags: msgTags},
		data_store.DataPoint{Name: "nats.messages.out", Value: float64(varz.OutMsgs), Timestamp: now, Tags: msgTags},
		data_store.DataPoint{Name: "nats.bytes.in", Value: float64(varz.InBytes), Timestamp: now, Tags: msgTags},
		data_store.DataPoint{Name: "nats.bytes.out", Value: float64(varz.OutBytes), Timestamp: now, Tags: msgTags},
	)

	slowTags := tagsWithType(baseTags, "errors")
	points = append(points,
		data_store.DataPoint{Name: "nats.slow_consumers", Value: float64(varz.SlowConsumers), Timestamp: now, Tags: slowTags},
	)

	// Pin the entity identity on the first successful /varz (nop if already pinned).
	p.entitySrc.pinFromVarz(varz.ServerName, varz.ServerID)

	// --- /routez (optional) ------------------------------------------------
	routezBody, routeStatus, routeErr := p.fetch("/routez")
	if routeErr == nil && routeStatus == http.StatusOK {
		var routez routezResponse
		if err := json.Unmarshal(routezBody, &routez); err == nil {
			routeTags := tagsWithType(baseTags, "cluster")
			points = append(points,
				data_store.DataPoint{Name: "nats.routes.count", Value: float64(routez.NumRoutes), Timestamp: now, Tags: routeTags},
			)
		} else {
			p.moduleLogger.Debug().Err(err).Msg("NATS /routez JSON decode failed; skipping routes metric")
		}
	} else if routeErr != nil {
		p.moduleLogger.Debug().Err(routeErr).Msg("NATS /routez not available; skipping routes metric")
	}

	// --- /jsz (optional — only present when JetStream is enabled) ----------
	jszBody, jszStatus, jszErr := p.fetch("/jsz")
	if jszErr == nil && jszStatus == http.StatusOK {
		var jsz jszResponse
		if err := json.Unmarshal(jszBody, &jsz); err == nil {
			jsTags := tagsWithType(baseTags, "jetstream")
			points = append(points,
				data_store.DataPoint{Name: "nats.jetstream.streams", Value: float64(jsz.Streams), Timestamp: now, Tags: jsTags},
				data_store.DataPoint{Name: "nats.jetstream.consumers", Value: float64(jsz.Consumers), Timestamp: now, Tags: jsTags},
				data_store.DataPoint{Name: "nats.jetstream.messages", Value: float64(jsz.Messages), Timestamp: now, Tags: jsTags},
				data_store.DataPoint{Name: "nats.jetstream.bytes", Value: float64(jsz.Bytes), Timestamp: now, Tags: jsTags},
			)
		} else {
			p.moduleLogger.Debug().Err(err).Msg("NATS /jsz JSON decode failed; skipping JetStream metrics")
		}
	} else if jszErr != nil {
		p.moduleLogger.Debug().Err(jszErr).Msg("NATS /jsz not available (JetStream likely disabled); skipping JetStream metrics")
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// httpGet performs a GET request to endpoint+path and returns the body, HTTP
// status code, and any transport-level error.
func (p *NATSProbe) httpGet(path string) ([]byte, int, error) {
	resp, err := p.client.Get(p.cfg.Endpoint + path) // #nosec G107 — operator-supplied management URL
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	return body, resp.StatusCode, nil
}

// tagsWithType returns a copy of base with metric_type overwritten to t.
func tagsWithType(base []tags.Tag, t string) []tags.Tag {
	out := make([]tags.Tag, len(base))
	copy(out, base)
	for i := range out {
		if out[i].Key == "metric_type" {
			out[i].Value = t
			return out
		}
	}
	return append(out, tags.Tag{Key: "metric_type", Value: t})
}
