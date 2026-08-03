// Package nginx implements the free nginx probe: Nginx stub_status page
// scraping for connection and request metrics via HTTP GET. Enables
// monitoring of the Nginx active connections, request throughput, and
// connection state breakdown (reading/writing/waiting).
//
// Requires the ngx_http_stub_status_module to be enabled in nginx.conf:
//
//	location /nginx_status {
//	    stub_status;
//	    allow 127.0.0.1;
//	    deny all;
//	}
package nginx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "nginx"

// NginxProbe polls the nginx stub_status page.
type NginxProbe struct {
	*types.BaseProbe
	cfg          nginxConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *nginxEntitySource
}

type nginxConfig struct {
	Endpoint     string
	Interval     time.Duration
	Timeout      time.Duration
	InstanceName string
}

const (
	defaultEndpoint = "http://localhost/nginx_status"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

// NewNginxProbe constructs the probe. Config errors surface here.
func NewNginxProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.nginx")

	cfg := nginxConfig{
		Endpoint: defaultEndpoint,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
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

	var hostID string
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	probe := &NginxProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
		entitySrc: newNginxEntitySource(cfg.Endpoint, cfg.InstanceName, hostID),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *NginxProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *NginxProbe) ShouldStart() bool          { return true }
func (p *NginxProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *NginxProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting nginx probe")
	return nil
}

func (p *NginxProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches the stub_status page and emits the Nginx metrics.
// A non-reachable endpoint is a measurement (up=0), never a
// collection-level error.
func (p *NginxProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	commonTags := []tags.Tag{
		{Key: "metric_type", Value: "connections"},
	}
	upTags := []tags.Tag{
		{Key: "metric_type", Value: "availability"},
	}

	body, err := p.fetchStatus()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).Msg("nginx stub_status unreachable")
		p.entitySrc.setReachable(false)
		points := []data_store.DataPoint{
			{Name: "senhub.nginx.up", Value: 0, Timestamp: now, Tags: upTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	stats, err := parseStubStatus(body)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("nginx stub_status parse error")
		p.entitySrc.setReachable(false)
		points := []data_store.DataPoint{
			{Name: "senhub.nginx.up", Value: 0, Timestamp: now, Tags: upTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	p.entitySrc.setReachable(true)

	points := []data_store.DataPoint{
		{Name: "senhub.nginx.up", Value: 1, Timestamp: now, Tags: upTags},
		{Name: "nginx.connections.current", Value: float64(stats.activeConnections), Timestamp: now, Tags: commonTags},
		{Name: "nginx.connections.accepted", Value: float64(stats.accepted), Timestamp: now, Tags: commonTags},
		{Name: "nginx.connections.handled", Value: float64(stats.handled), Timestamp: now, Tags: commonTags},
		{Name: "nginx.requests", Value: float64(stats.requests), Timestamp: now, Tags: []tags.Tag{{Key: "metric_type", Value: "throughput"}}},
		{Name: "nginx.connections.reading", Value: float64(stats.reading), Timestamp: now, Tags: commonTags},
		{Name: "nginx.connections.writing", Value: float64(stats.writing), Timestamp: now, Tags: commonTags},
		{Name: "nginx.connections.waiting", Value: float64(stats.waiting), Timestamp: now, Tags: commonTags},
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// nginxVersionFromServerHeader extracts the version from an nginx Server header
// ("nginx/1.27.0" → "1.27.0"). Returns "" when the header is absent or carries
// no version (server_tokens off → bare "nginx").
func nginxVersionFromServerHeader(h string) string {
	const prefix = "nginx/"
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

func (p *NginxProbe) fetchStatus() (string, error) {
	resp, err := p.client.Get(p.cfg.Endpoint) // #nosec G107 - endpoint is operator-configured
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", p.cfg.Endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: unexpected status %d", p.cfg.Endpoint, resp.StatusCode)
	}
	// service.version rides the entity from the Server header ("nginx/1.27.0").
	p.entitySrc.setVersion(nginxVersionFromServerHeader(resp.Header.Get("Server")))
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("reading body from %s: %w", p.cfg.Endpoint, err)
	}
	return string(body), nil
}

// stubStatusData holds the parsed values from the nginx stub_status page.
type stubStatusData struct {
	activeConnections int64
	accepted          int64
	handled           int64
	requests          int64
	reading           int64
	writing           int64
	waiting           int64
}

// parseStubStatus parses the nginx stub_status plain-text format:
//
//	Active connections: 291
//	server accepts handled requests
//	 16630948 16630948 31070465
//	Reading: 6 Writing: 179 Waiting: 106
func parseStubStatus(body string) (stubStatusData, error) {
	var s stubStatusData
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 4 {
		return s, fmt.Errorf("stub_status: expected at least 4 lines, got %d", len(lines))
	}

	// Line 1: "Active connections: N"
	fields := strings.Fields(lines[0])
	if len(fields) < 3 || fields[0] != "Active" {
		return s, fmt.Errorf("stub_status: unexpected line 1: %q", lines[0])
	}
	var err error
	s.activeConnections, err = parseInt64(fields[2])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing active connections: %w", err)
	}

	// Line 3: "N N N" — accepts handled requests
	// (Line 2 is the "server accepts handled requests" header.)
	countFields := strings.Fields(lines[2])
	if len(countFields) < 3 {
		return s, fmt.Errorf("stub_status: unexpected line 3: %q", lines[2])
	}
	s.accepted, err = parseInt64(countFields[0])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing accepted: %w", err)
	}
	s.handled, err = parseInt64(countFields[1])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing handled: %w", err)
	}
	s.requests, err = parseInt64(countFields[2])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing requests: %w", err)
	}

	// Line 4: "Reading: N Writing: N Waiting: N"
	rwFields := strings.Fields(lines[3])
	if len(rwFields) < 6 {
		return s, fmt.Errorf("stub_status: unexpected line 4: %q", lines[3])
	}
	s.reading, err = parseInt64(rwFields[1])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing reading: %w", err)
	}
	s.writing, err = parseInt64(rwFields[3])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing writing: %w", err)
	}
	s.waiting, err = parseInt64(rwFields[5])
	if err != nil {
		return s, fmt.Errorf("stub_status: parsing waiting: %w", err)
	}

	return s, nil
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
