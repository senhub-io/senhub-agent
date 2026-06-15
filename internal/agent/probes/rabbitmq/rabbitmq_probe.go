// Package rabbitmq implements the free rabbitmq probe: node-level totals,
// per-node resource metrics, and per-queue depth via the RabbitMQ HTTP
// Management API (GET /api/overview, /api/nodes, /api/queues).
//
// Authentication: HTTP Basic (default guest/guest). No external dependencies;
// stdlib net/http + encoding/json only.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "rabbitmq"

// rabbitProbe collects metrics from the RabbitMQ HTTP Management API.
type rabbitProbe struct {
	*types.BaseProbe
	cfg          rabbitConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *rabbitmqEntitySource
	unregister   func()
}

type rabbitConfig struct {
	Endpoint     string
	Username     string
	Password     string
	InstanceName string
	Interval     time.Duration
	Timeout      time.Duration
}

const (
	defaultEndpoint = "http://localhost:15672"
	defaultUsername = "guest"
	defaultPassword = "guest"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

// NewRabbitMQProbe constructs the probe. Config errors surface here.
func NewRabbitMQProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.rabbitmq")

	cfg := rabbitConfig{
		Endpoint: defaultEndpoint,
		Username: defaultUsername,
		Password: defaultPassword,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok && v != "" {
		cfg.Password = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	probe := &rabbitProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       &http.Client{Timeout: cfg.Timeout},
	}
	probe.SetProbeType(ProbeType)

	probe.entitySrc = newRabbitmqEntitySource(
		cfg.InstanceName,
		endpointHost(cfg.Endpoint),
		endpointPort(cfg.Endpoint),
		defaultHostIDFn,
	)

	return probe, nil
}

// defaultHostIDFn is the production host-id provider for the fallback id.
func defaultHostIDFn() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

// endpointHost extracts the hostname from the management API endpoint URL.
// Falls back to the raw endpoint string when parsing fails.
func endpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return endpoint
	}
	return u.Hostname()
}

// endpointPort extracts the port from the management API endpoint URL as an
// int64. When the URL carries no explicit port the scheme default is used
// (15672 for http, 15671 for https). Unknown schemes return 0.
func endpointPort(endpoint string) int64 {
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0
	}
	if p := u.Port(); p != "" {
		v, _ := strconv.ParseInt(p, 10, 64)
		return v
	}
	switch u.Scheme {
	case "https":
		return 15671
	default:
		return 15672
	}
}

func (p *rabbitProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *rabbitProbe) ShouldStart() bool          { return true }
func (p *rabbitProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *rabbitProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting rabbitmq probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *rabbitProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches /api/overview, /api/nodes, and /api/queues.
// On any HTTP or parse error the probe emits up=0 and returns nil
// (a reachability failure is a measurement, not a collection error).
//
// Entity identity: on first successful contact the broker's Erlang node name
// (from /api/overview "node" field) is pinned as the service.instance.id.
// Before the id is pinned the entity rail returns ok=false (no entity emitted).
func (p *rabbitProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	var points []data_store.DataPoint

	overviewPoints, nodeName, reachable := p.collectOverview(now)
	points = append(points, overviewPoints...)

	if reachable {
		// Pin the tech id on first successful collect.
		p.entitySrc.tryPinTechID(nodeName)
		p.entitySrc.setReachable(true, "")
		points = append(points, p.collectNodes(now)...)
		points = append(points, p.collectQueues(now)...)
	} else {
		p.entitySrc.setReachable(false, "")
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// ── Overview ────────────────────────────────────────────────────────────────

// overviewResponse is a partial mapping of GET /api/overview.
type overviewResponse struct {
	// Node is the Erlang node name of the RabbitMQ broker (e.g. "rabbit@myhost").
	// It is the stable, persistent identity source for this probe (D1 option A).
	Node         string `json:"node"`
	MessageStats struct {
		Publish    *int64 `json:"publish"`
		DeliverGet *int64 `json:"deliver_get"`
		Ack        *int64 `json:"ack"`
	} `json:"message_stats"`
	QueueTotals struct {
		MessagesUnacknowledged *int64 `json:"messages_unacknowledged"`
		MessagesReady          *int64 `json:"messages_ready"`
	} `json:"queue_totals"`
	ObjectTotals struct {
		Consumers   *int64 `json:"consumers"`
		Queues      *int64 `json:"queues"`
		Connections *int64 `json:"connections"`
		Channels    *int64 `json:"channels"`
	} `json:"object_totals"`
}

// collectOverview fetches /api/overview and returns the datapoints, the
// broker node name (for entity identity), and whether the broker was reachable.
func (p *rabbitProbe) collectOverview(now time.Time) ([]data_store.DataPoint, string, bool) {
	baseTags := []tags.Tag{
		{Key: "metric_type", Value: "overview"},
	}

	var overview overviewResponse
	err := p.fetchJSON("/api/overview", &overview)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("rabbitmq overview fetch failed")
		return []data_store.DataPoint{
			{Name: "senhub.rabbitmq.up", Value: 0, Timestamp: now, Tags: baseTags},
		}, "", false
	}

	pts := []data_store.DataPoint{
		{Name: "senhub.rabbitmq.up", Value: 1, Timestamp: now, Tags: baseTags},
	}

	if v := overview.MessageStats.Publish; v != nil {
		pts = append(pts, dp("rabbitmq.messages.published", float64(*v), now, baseTags))
	}
	if v := overview.MessageStats.DeliverGet; v != nil {
		pts = append(pts, dp("rabbitmq.messages.delivered", float64(*v), now, baseTags))
	}
	if v := overview.MessageStats.Ack; v != nil {
		pts = append(pts, dp("rabbitmq.messages.acknowledged", float64(*v), now, baseTags))
	}
	if v := overview.QueueTotals.MessagesUnacknowledged; v != nil {
		pts = append(pts, dp("rabbitmq.messages.unacknowledged", float64(*v), now, baseTags))
	}
	if v := overview.QueueTotals.MessagesReady; v != nil {
		pts = append(pts, dp("rabbitmq.messages.ready", float64(*v), now, baseTags))
	}
	if v := overview.ObjectTotals.Consumers; v != nil {
		pts = append(pts, dp("rabbitmq.consumers.total", float64(*v), now, baseTags))
	}
	if v := overview.ObjectTotals.Queues; v != nil {
		pts = append(pts, dp("rabbitmq.queues.total", float64(*v), now, baseTags))
	}
	if v := overview.ObjectTotals.Connections; v != nil {
		pts = append(pts, dp("rabbitmq.connections.total", float64(*v), now, baseTags))
	}
	if v := overview.ObjectTotals.Channels; v != nil {
		pts = append(pts, dp("rabbitmq.channels.total", float64(*v), now, baseTags))
	}

	return pts, overview.Node, true
}

// ── Nodes ───────────────────────────────────────────────────────────────────

// nodeResponse is a partial mapping of one entry from GET /api/nodes.
type nodeResponse struct {
	Name        string `json:"name"`
	MemUsed     *int64 `json:"mem_used"`
	DiskFree    *int64 `json:"disk_free"`
	FdUsed      *int64 `json:"fd_used"`
	SocketsUsed *int64 `json:"sockets_used"`
	Running     bool   `json:"running"`
	Uptime      *int64 `json:"uptime"`
}

func (p *rabbitProbe) collectNodes(now time.Time) []data_store.DataPoint {
	var nodes []nodeResponse
	if err := p.fetchJSON("/api/nodes", &nodes); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("rabbitmq nodes fetch failed")
		return nil
	}

	var pts []data_store.DataPoint
	for _, node := range nodes {
		nodeTags := []tags.Tag{
			{Key: "node", Value: node.Name},
			{Key: "metric_type", Value: "node"},
		}

		if v := node.MemUsed; v != nil {
			pts = append(pts, dp("rabbitmq.node.memory.used", float64(*v), now, nodeTags))
		}
		if v := node.DiskFree; v != nil {
			pts = append(pts, dp("rabbitmq.node.disk.free", float64(*v), now, nodeTags))
		}
		if v := node.FdUsed; v != nil {
			pts = append(pts, dp("rabbitmq.node.fd.used", float64(*v), now, nodeTags))
		}
		if v := node.SocketsUsed; v != nil {
			pts = append(pts, dp("rabbitmq.node.sockets.used", float64(*v), now, nodeTags))
		}

		running := float64(0)
		if node.Running {
			running = 1
		}
		pts = append(pts, dp("rabbitmq.node.running", running, now, nodeTags))

		if v := node.Uptime; v != nil {
			pts = append(pts, dp("rabbitmq.node.uptime", float64(*v), now, nodeTags))
		}
	}
	return pts
}

// ── Queues ──────────────────────────────────────────────────────────────────

// queueResponse is a partial mapping of one entry from GET /api/queues.
type queueResponse struct {
	Name                   string `json:"name"`
	Vhost                  string `json:"vhost"`
	MessagesReady          *int64 `json:"messages_ready"`
	MessagesUnacknowledged *int64 `json:"messages_unacknowledged"`
	Consumers              *int64 `json:"consumers"`
}

func (p *rabbitProbe) collectQueues(now time.Time) []data_store.DataPoint {
	var queues []queueResponse
	if err := p.fetchJSON("/api/queues", &queues); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("rabbitmq queues fetch failed")
		return nil
	}

	var pts []data_store.DataPoint
	for _, q := range queues {
		qTags := []tags.Tag{
			{Key: "vhost", Value: q.Vhost},
			{Key: "queue", Value: q.Name},
			{Key: "metric_type", Value: "queue"},
		}

		if v := q.MessagesReady; v != nil {
			pts = append(pts, dp("rabbitmq.queue.messages.ready", float64(*v), now, qTags))
		}
		if v := q.MessagesUnacknowledged; v != nil {
			pts = append(pts, dp("rabbitmq.queue.messages.unacknowledged", float64(*v), now, qTags))
		}
		if v := q.Consumers; v != nil {
			pts = append(pts, dp("rabbitmq.queue.consumers", float64(*v), now, qTags))
		}
	}
	return pts
}

// ── helpers ─────────────────────────────────────────────────────────────────

// dp builds a DataPoint from a name, value, timestamp, and tag slice.
// It copies the tags so callers can reuse their slice safely.
func dp(name string, value float64, ts time.Time, baseTags []tags.Tag) data_store.DataPoint {
	t := make([]tags.Tag, len(baseTags))
	copy(t, baseTags)
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: t}
}

// fetchJSON performs a BasicAuth GET to path (relative to the configured
// endpoint) and JSON-decodes the response body into dst.
func (p *rabbitProbe) fetchJSON(path string, dst interface{}) error {
	url := p.cfg.Endpoint + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}
	req.SetBasicAuth(p.cfg.Username, p.cfg.Password)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", url, err)
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding JSON from %s: %w", url, err)
	}
	return nil
}
