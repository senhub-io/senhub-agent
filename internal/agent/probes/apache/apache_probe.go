// Package apache implements the free apache probe: collects metrics from
// the Apache HTTP Server mod_status endpoint (?auto format). No external
// dependencies — pure stdlib HTTP client with optional BasicAuth.
//
// Metric names follow OTel semantic conventions where defined:
// apache.uptime, apache.current_connections, apache.workers,
// apache.requests, apache.traffic align with the otelcol-contrib
// Apache receiver. senhub.apache.up is a senhub extension.
package apache

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier.
const ProbeType = "apache"

const (
	defaultEndpoint = "http://localhost/server-status?auto"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
)

type apacheProbe struct {
	*types.BaseProbe
	cfg          apacheConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *apacheEntitySource
	unregister   func()
}

type apacheConfig struct {
	Endpoint string
	Username string
	Password string
	Interval time.Duration
	Timeout  time.Duration
}

// NewApacheProbe constructs the probe. Config errors surface here.
func NewApacheProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.apache")

	cfg := apacheConfig{
		Endpoint: defaultEndpoint,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}

	addr, port := hostPortFromEndpoint(cfg.Endpoint)

	probe := &apacheProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
		entitySrc: newApacheEntitySource(addr, port),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

// hostPortFromEndpoint parses a URL and returns the host and port as separate
// values. The port defaults to 80 for http and 443 for https when absent.
func hostPortFromEndpoint(endpoint string) (string, int) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "localhost", 80
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		// No explicit port in the URL.
		host = u.Host
		switch u.Scheme {
		case "https":
			return host, 443
		default:
			return host, 80
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 80
	}
	return host, port
}

func (p *apacheProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *apacheProbe) ShouldStart() bool          { return true }
func (p *apacheProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *apacheProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting apache probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *apacheProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches mod_status and emits metrics. A fetch failure produces
// senhub.apache.up=0 but is never a collection-level error — the probe
// continues scheduling on the next interval.
func (p *apacheProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points := p.collect(now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// modStatusFields holds the parsed fields from mod_status ?auto output.
type modStatusFields struct {
	uptime      *int64
	busyWorkers *int64
	idleWorkers *int64
	connsTotal  *int64
	totalAccess *int64
	totalKBytes *int64
}

func (p *apacheProbe) collect(now time.Time) []data_store.DataPoint {
	commonTags := []tags.Tag{
		{Key: "metric_type", Value: "status"},
	}

	fields, err := p.fetchStatus()
	if err != nil {
		p.moduleLogger.Warn().
			Err(err).
			Str("endpoint", p.cfg.Endpoint).
			Msg("apache mod_status fetch failed")
		p.entitySrc.setReachable(false, "")
		return []data_store.DataPoint{
			{Name: "senhub.apache.up", Value: 0, Timestamp: now, Tags: commonTags},
		}
	}

	p.entitySrc.setReachable(true, "")

	points := []data_store.DataPoint{
		{Name: "senhub.apache.up", Value: 1, Timestamp: now, Tags: commonTags},
	}

	if fields.uptime != nil {
		points = append(points, data_store.DataPoint{
			Name:      "apache.uptime",
			Value:     float32(*fields.uptime),
			Timestamp: now,
			Tags:      withType(commonTags, "status"),
		})
	}

	if fields.connsTotal != nil {
		points = append(points, data_store.DataPoint{
			Name:      "apache.current_connections",
			Value:     float32(*fields.connsTotal),
			Timestamp: now,
			Tags:      withType(commonTags, "connections"),
		})
	}

	if fields.busyWorkers != nil {
		workerTags := append(tagsClone(withType(commonTags, "workers")), tags.Tag{Key: "state", Value: "busy"})
		points = append(points, data_store.DataPoint{
			Name:      "apache.workers",
			Value:     float32(*fields.busyWorkers),
			Timestamp: now,
			Tags:      workerTags,
		})
	}

	if fields.idleWorkers != nil {
		workerTags := append(tagsClone(withType(commonTags, "workers")), tags.Tag{Key: "state", Value: "idle"})
		points = append(points, data_store.DataPoint{
			Name:      "apache.workers",
			Value:     float32(*fields.idleWorkers),
			Timestamp: now,
			Tags:      workerTags,
		})
	}

	if fields.totalAccess != nil {
		points = append(points, data_store.DataPoint{
			Name:      "apache.requests",
			Value:     float32(*fields.totalAccess),
			Timestamp: now,
			Tags:      withType(commonTags, "throughput"),
		})
	}

	if fields.totalKBytes != nil {
		points = append(points, data_store.DataPoint{
			Name:      "apache.traffic",
			Value:     float32(*fields.totalKBytes * 1024),
			Timestamp: now,
			Tags:      withType(commonTags, "throughput"),
		})
	}

	return points
}

// fetchStatus performs the HTTP GET and parses the mod_status ?auto text.
func (p *apacheProbe) fetchStatus() (*modStatusFields, error) {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", p.cfg.Endpoint, err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", p.cfg.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned HTTP %d", p.cfg.Endpoint, resp.StatusCode)
	}

	return parseModStatus(resp)
}

// parseModStatus reads the mod_status ?auto text body and extracts metric fields.
// Each line has the form "Key: Value". Unknown keys are silently skipped.
func parseModStatus(resp *http.Response) (*modStatusFields, error) {
	var fields modStatusFields
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		key, valStr, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		valStr = strings.TrimSpace(valStr)
		switch key {
		case "Uptime":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.uptime = &v
			}
		case "BusyWorkers":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.busyWorkers = &v
			}
		case "IdleWorkers":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.idleWorkers = &v
			}
		case "ConnsTotal":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.connsTotal = &v
			}
		case "Total Accesses":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.totalAccess = &v
			}
		case "Total kBytes":
			if v, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				fields.totalKBytes = &v
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading mod_status body: %w", err)
	}
	return &fields, nil
}

// withType returns a copy of baseTags with metric_type overwritten to t.
// Used to change the category tag per metric family without mutating
// the shared base slice.
func withType(baseTags []tags.Tag, t string) []tags.Tag {
	out := make([]tags.Tag, 0, len(baseTags))
	for _, tag := range baseTags {
		if tag.Key == "metric_type" {
			out = append(out, tags.Tag{Key: "metric_type", Value: t})
		} else {
			out = append(out, tag)
		}
	}
	return out
}

// tagsClone returns a shallow copy of the tag slice so appending to it
// does not mutate the original.
func tagsClone(src []tags.Tag) []tags.Tag {
	out := make([]tags.Tag, len(src))
	copy(out, src)
	return out
}
