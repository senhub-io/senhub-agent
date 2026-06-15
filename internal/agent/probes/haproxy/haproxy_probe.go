// Package haproxy implements the free haproxy probe: polls the HAProxy
// stats CSV endpoint over HTTP and emits session, throughput and error
// metrics per frontend / backend / server component.
//
// Protocol: HTTP GET to the stats;csv endpoint (default port 8080).
// BasicAuth is supported. CSV format: first line is a comment-header
// starting with "#". Values are parsed by column index (stable across
// HAProxy versions).
//
// Metric names follow the otelcol-contrib haproxy receiver where
// available.
package haproxy

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in registry, JWT
// claims and transformer YAML file names.
const ProbeType = "haproxy"

// CSV column indices (0-indexed, HAProxy stats CSV v3+).
const (
	colPxname  = 0  // proxy name (frontend/backend name)
	colSvname  = 1  // server name: "FRONTEND", "BACKEND", or server hostname
	colScur    = 4  // current sessions
	colStot    = 8  // total sessions (counter)
	colBin     = 9  // bytes in (counter)
	colBout    = 10 // bytes out (counter)
	colEreq    = 12 // request errors (frontends only)
	colEcon    = 13 // connection errors
	colEresp   = 14 // response errors
	colReqRate = 46 // request rate (requests/s) — may be absent in older builds
	colType    = 47 // row type: 0=frontend, 1=backend, 2=server, 3=listener
	minColumns = 48 // minimum column count for full parsing
)

// HAProxy row types from the type column (index 47).
const (
	typeFrontend = 0
	typeBackend  = 1
	typeServer   = 2
	typeListener = 3
)

type haproxyProbe struct {
	*types.BaseProbe
	cfg          haproxyConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *haproxyEntitySource
	unregister   func()
}

type haproxyConfig struct {
	Endpoint     string
	Username     string
	Password     string
	InstanceName string
	Interval     time.Duration
	Timeout      time.Duration
}

const (
	defaultEndpoint = "http://localhost:8080/stats;csv"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second
)

// NewHAProxyProbe constructs the probe. All config fields are optional
// and fall back to safe defaults.
func NewHAProxyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.haproxy")

	cfg := haproxyConfig{
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
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	// Resolve the stable host id once at construction. On error we pass ""
	// and the entity source falls back to the bare "haproxy" last-resort id.
	hostID := ""
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	probe := &haproxyProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
	probe.SetProbeType(ProbeType)

	addr, port := endpointHostPort(cfg.Endpoint)
	probe.entitySrc = newHAProxyEntitySource(addr, port, cfg.InstanceName, hostID)

	return probe, nil
}

func (p *haproxyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *haproxyProbe) ShouldStart() bool          { return true }
func (p *haproxyProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *haproxyProbe) OnStart(_ chan struct{}) error {
	p.unregister = entity.RegisterSource(p.entitySrc)
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting haproxy probe")
	return nil
}

func (p *haproxyProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches the stats CSV and emits one set of datapoints per
// frontend and backend row. Server rows are included for bytes/errors
// but session counts are frontend/backend only.
// senhub.haproxy.up is always emitted — even on error.
func (p *haproxyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	upTags := []tags.Tag{{Key: "metric_type", Value: "status"}}
	up := float32(1)

	rows, err := p.fetchCSV()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("haproxy stats fetch failed")
		p.entitySrc.setReachable(false)
		up = 0
		points := []data_store.DataPoint{
			{Name: "senhub.haproxy.up", Value: up, Timestamp: now, Tags: upTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	p.entitySrc.setReachable(true)
	points := []data_store.DataPoint{
		{Name: "senhub.haproxy.up", Value: up, Timestamp: now, Tags: upTags},
	}
	points = append(points, p.buildDatapoints(rows, now)...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchCSV issues the HTTP GET, optionally with BasicAuth, and returns
// the parsed rows (excluding the header comment line).
func (p *haproxyProbe) fetchCSV() ([][]string, error) {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", p.cfg.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("haproxy stats endpoint returned HTTP %d", resp.StatusCode)
	}

	return parseCSV(resp.Body)
}

// parseCSV reads the HAProxy stats CSV from an io.Reader. The first
// line is a comment-header starting with "#" and is skipped via
// csv.Reader.Comment.
func parseCSV(r io.Reader) ([][]string, error) {
	reader := csv.NewReader(r)
	reader.Comment = '#'
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // variable columns across HAProxy versions

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing CSV: %w", err)
	}
	return records, nil
}

// buildDatapoints converts the CSV rows into metric datapoints.
func (p *haproxyProbe) buildDatapoints(rows [][]string, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	for _, row := range rows {
		if len(row) < minColumns {
			continue
		}

		rowType := int(parseInt(row[colType]))
		// Listener rows duplicate frontend counters — skip them.
		if rowType == typeListener {
			continue
		}

		pxname := strings.TrimSpace(row[colPxname])
		component := componentName(rowType)

		baseTags := []tags.Tag{
			{Key: "proxy", Value: pxname},
			{Key: "component", Value: component},
		}

		// Sessions — frontends and backends only.
		if rowType == typeFrontend || rowType == typeBackend {
			sessionTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "sessions"})
			points = append(points,
				data_store.DataPoint{
					Name:      "haproxy.sessions.count",
					Value:     float32(parseInt(row[colScur])),
					Timestamp: ts,
					Tags:      sessionTags,
				},
				data_store.DataPoint{
					Name:      "haproxy.sessions.total",
					Value:     float32(parseInt(row[colStot])),
					Timestamp: ts,
					Tags:      sessionTags,
				},
			)
		}

		// Throughput — all row types.
		throughputTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "throughput"})
		points = append(points,
			data_store.DataPoint{
				Name:      "haproxy.bytes.input",
				Value:     float32(parseInt(row[colBin])),
				Timestamp: ts,
				Tags:      throughputTags,
			},
			data_store.DataPoint{
				Name:      "haproxy.bytes.output",
				Value:     float32(parseInt(row[colBout])),
				Timestamp: ts,
				Tags:      throughputTags,
			},
		)

		// Errors — all row types.
		errorTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "errors"})
		points = append(points,
			data_store.DataPoint{
				Name:      "haproxy.connections.errors",
				Value:     float32(parseInt(row[colEcon])),
				Timestamp: ts,
				Tags:      errorTags,
			},
			data_store.DataPoint{
				Name:      "haproxy.responses.errors",
				Value:     float32(parseInt(row[colEresp])),
				Timestamp: ts,
				Tags:      errorTags,
			},
		)

		// Request errors — frontends only.
		if rowType == typeFrontend {
			points = append(points,
				data_store.DataPoint{
					Name:      "haproxy.requests.errors",
					Value:     float32(parseInt(row[colEreq])),
					Timestamp: ts,
					Tags:      errorTags,
				},
			)
		}

		// Request rate — frontends only; column may be absent in older HAProxy.
		if rowType == typeFrontend && len(row) > colReqRate {
			requestTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "requests"})
			points = append(points,
				data_store.DataPoint{
					Name:      "haproxy.requests.rate",
					Value:     float32(parseFloat(row[colReqRate])),
					Timestamp: ts,
					Tags:      requestTags,
				},
			)
		}
	}

	return points
}

// componentName maps the HAProxy row type integer to the component tag.
func componentName(rowType int) string {
	switch rowType {
	case typeFrontend:
		return "frontend"
	case typeBackend:
		return "backend"
	case typeServer:
		return "server"
	default:
		return "other"
	}
}

// cloneTags returns a copy of the slice so that append on different
// paths does not share the underlying array.
func cloneTags(src []tags.Tag) []tags.Tag {
	dst := make([]tags.Tag, len(src))
	copy(dst, src)
	return dst
}

// parseInt parses a CSV cell as int64, returning 0 on empty/invalid.
func parseInt(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// parseFloat parses a CSV cell as float64, returning 0 on empty/invalid.
func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// endpointHostPort extracts the hostname and port from the stats endpoint URL.
// Falls back to "localhost" / 8080 on any parse error, matching the probe default.
func endpointHostPort(endpoint string) (host string, port int) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return "localhost", 8080
	}
	host = u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		switch u.Scheme {
		case "https":
			return host, 443
		default:
			return host, 80
		}
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return host, 8080
	}
	return host, p
}
