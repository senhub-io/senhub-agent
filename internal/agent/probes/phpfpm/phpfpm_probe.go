// Package phpfpm implements the free phpfpm probe: PHP-FPM pool
// monitoring via the status page JSON endpoint
// (pm.status_path in php-fpm config, served by nginx/Apache).
//
// Each probe instance targets one pool (single pm.status_path endpoint).
// For multi-pool installations, configure multiple probe instances — one
// per pool — and rely on the "pool" tag to distinguish them.
package phpfpm

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

// ProbeType is the stable technical identifier used in YAML config,
// transformer definitions, and license claims.
const ProbeType = "phpfpm"

// fpmStatus mirrors the JSON body returned by ?json on the PHP-FPM
// status endpoint.
type fpmStatus struct {
	Pool              string `json:"pool"`
	StartSince        int64  `json:"start since"`
	AcceptedConn      int64  `json:"accepted conn"`
	ListenQueue       int64  `json:"listen queue"`
	MaxListenQueue    int64  `json:"max listen queue"`
	IdleProcesses     int64  `json:"idle processes"`
	ActiveProcesses   int64  `json:"active processes"`
	TotalProcesses    int64  `json:"total processes"`
	MaxChildrenReached int64 `json:"max children reached"`
	SlowRequests      int64  `json:"slow requests"`
}

type phpFPMProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
}

type probeConfig struct {
	Endpoint string
	Interval time.Duration
	Timeout  time.Duration
}

const (
	defaultEndpoint = "http://localhost/fpm-status"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

// NewPHPFPMProbe constructs the probe. Config errors surface here.
func NewPHPFPMProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.phpfpm")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &phpFPMProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
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
	return cfg, nil
}

func (p *phpFPMProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *phpFPMProbe) ShouldStart() bool          { return true }
func (p *phpFPMProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *phpFPMProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting phpfpm probe")
	return nil
}

func (p *phpFPMProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches the PHP-FPM status page and builds datapoints.
// The "up" gauge is always emitted even when the endpoint is unreachable.
func (p *phpFPMProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points, _ := p.collect(now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *phpFPMProbe) collect(now time.Time) ([]data_store.DataPoint, error) {
	status, err := p.fetchStatus()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).Msg("phpfpm status fetch failed")
		upTags := []tags.Tag{{Key: "metric_type", Value: "availability"}}
		return []data_store.DataPoint{
			{Name: "senhub.phpfpm.up", Value: 0, Timestamp: now, Tags: upTags},
		}, err
	}

	poolTag := status.Pool
	if poolTag == "" {
		poolTag = "unknown"
	}

	baseTags := []tags.Tag{
		{Key: "pool", Value: poolTag},
		{Key: "metric_type", Value: "availability"},
	}
	processTags := []tags.Tag{
		{Key: "pool", Value: poolTag},
		{Key: "metric_type", Value: "processes"},
	}
	connTags := []tags.Tag{
		{Key: "pool", Value: poolTag},
		{Key: "metric_type", Value: "connections"},
	}

	points := []data_store.DataPoint{
		{Name: "senhub.phpfpm.up", Value: 1, Timestamp: now, Tags: baseTags},
		{Name: "phpfpm.uptime", Value: float32(status.StartSince), Timestamp: now, Tags: baseTags},
		{Name: "phpfpm.accepted_connections", Value: float32(status.AcceptedConn), Timestamp: now, Tags: connTags},
		{Name: "phpfpm.slow_requests", Value: float32(status.SlowRequests), Timestamp: now, Tags: connTags},
		{Name: "phpfpm.listen_queue.current", Value: float32(status.ListenQueue), Timestamp: now, Tags: connTags},
		{Name: "phpfpm.listen_queue.max", Value: float32(status.MaxListenQueue), Timestamp: now, Tags: connTags},
		{Name: "phpfpm.processes.active", Value: float32(status.ActiveProcesses), Timestamp: now, Tags: processTags},
		{Name: "phpfpm.processes.idle", Value: float32(status.IdleProcesses), Timestamp: now, Tags: processTags},
		{Name: "phpfpm.processes.total", Value: float32(status.TotalProcesses), Timestamp: now, Tags: processTags},
		{Name: "phpfpm.max_children_reached", Value: float32(status.MaxChildrenReached), Timestamp: now, Tags: connTags},
	}
	return points, nil
}

// fetchStatus performs the HTTP GET to the status endpoint and
// unmarshals the JSON response.
func (p *phpFPMProbe) fetchStatus() (*fpmStatus, error) {
	url := p.cfg.Endpoint
	// Append ?json unless the caller already included it.
	if !strings.Contains(url, "json") {
		if strings.Contains(url, "?") {
			url += "&json"
		} else {
			url += "?json"
		}
	}

	resp, err := p.client.Get(url) // #nosec G107 - URL is operator-configured
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	var status fpmStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("parsing JSON from %s: %w", url, err)
	}
	return &status, nil
}
