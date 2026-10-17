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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/subcollect"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
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
	failures     subcollect.Tracker
}

type haproxyConfig struct {
	Endpoint     string
	Username     string
	Password     string
	InstanceName string
	Interval     time.Duration
	Timeout      time.Duration
}

// httpStatusError is returned by fetchCSV for non-200 responses so Collect
// can classify the HTTP status with dbcommon.ClassifyHTTPStatus.
type httpStatusError struct {
	code int
	msg  string
}

func (e *httpStatusError) Error() string { return e.msg }

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
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
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

	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func (p *haproxyProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *haproxyProbe) ShouldStart() bool          { return true }
func (p *haproxyProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *haproxyProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting haproxy probe")
	return nil
}

func (p *haproxyProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect fetches the stats CSV and emits one set of datapoints per
// frontend and backend row. Server rows are included for bytes/errors
// but session counts are frontend/backend only.
// senhub.haproxy.up is always emitted — even on error.
func (p *haproxyProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	statusTags := p.baseStatusTags()
	upTags := cloneTags(statusTags)
	checkTags := cloneTags(statusTags)

	up := float64(1)

	rows, err := p.fetchCSV()
	if err != nil {
		p.failures.Failed(p.moduleLogger, subcollect.Warn, "stats", err, "haproxy stats fetch failed")
		p.entitySrc.setReachable(false)
		up = 0

		checkResult := float64(dbcommon.ClassifyError(err))
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) {
			checkResult = float64(dbcommon.ClassifyHTTPStatus(statusErr.code))
		}

		points := []data_store.DataPoint{
			{Name: "senhub.haproxy.up", Value: up, Timestamp: now, Tags: upTags},
			{Name: dbcommon.CheckResultMetric, Value: checkResult, Timestamp: now, Tags: checkTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	p.entitySrc.setReachable(true)
	p.failures.Succeeded(p.moduleLogger, "stats", "haproxy stats fetch recovered")
	points := []data_store.DataPoint{
		{Name: "senhub.haproxy.up", Value: up, Timestamp: now, Tags: upTags},
		{Name: dbcommon.CheckResultMetric, Value: float64(dbcommon.CheckOK), Timestamp: now, Tags: checkTags},
	}
	points = append(points, p.buildDatapoints(rows, now)...)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// baseStatusTags returns the probe-level status tags: metric_type status and
// the service.instance identity key carried by every status datapoint.
func (p *haproxyProbe) baseStatusTags() []tags.Tag {
	t := []tags.Tag{{Key: "metric_type", Value: "status"}}
	return append(t, p.identityTags()...)
}

// identityTags returns the service.instance.id identity tag for the target.
func (p *haproxyProbe) identityTags() []tags.Tag {
	if p.entitySrc == nil {
		return nil
	}
	if id := p.entitySrc.InstanceID(); id != "" {
		return []tags.Tag{{Key: "service.instance.id", Value: id}}
	}
	return nil
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
		return nil, &httpStatusError{
			code: resp.StatusCode,
			msg:  fmt.Sprintf("haproxy stats endpoint returned HTTP %d", resp.StatusCode),
		}
	}

	// The HTML stats page answers on the same URL without ";csv", and read
	// as CSV it fails on its first quote with an error that names nothing
	// the operator can act on.
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil, fmt.Errorf("the endpoint returned the HTML stats page; point it at the CSV form, %s;csv", strings.TrimSuffix(p.cfg.Endpoint, "/"))
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
		baseTags = append(baseTags, p.identityTags()...)

		// Sessions — frontends and backends only.
		if rowType == typeFrontend || rowType == typeBackend {
			sessionTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "sessions"})
			points = appendCell(points, "haproxy.sessions.count", row[colScur], ts, sessionTags)
			points = appendCell(points, "haproxy.sessions.total", row[colStot], ts, sessionTags)
		}

		// Throughput — all row types.
		throughputTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "throughput"})
		points = appendCell(points, "haproxy.bytes.input", row[colBin], ts, throughputTags)
		points = appendCell(points, "haproxy.bytes.output", row[colBout], ts, throughputTags)

		// Errors. econ and eresp only have a value on backends and servers;
		// HAProxy leaves them empty on a frontend, which is not a zero.
		errorTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "errors"})
		points = appendCell(points, "haproxy.connections.errors", row[colEcon], ts, errorTags)
		points = appendCell(points, "haproxy.responses.errors", row[colEresp], ts, errorTags)

		// Request errors — frontends only.
		if rowType == typeFrontend {
			points = appendCell(points, "haproxy.requests.errors", row[colEreq], ts, errorTags)
		}

		// Request rate — frontends only; column may be absent in older HAProxy.
		if rowType == typeFrontend && len(row) > colReqRate {
			requestTags := append(cloneTags(baseTags), tags.Tag{Key: "metric_type", Value: "requests"})
			points = appendCell(points, "haproxy.requests.rate", row[colReqRate], ts, requestTags)
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

// appendCell adds a point for a CSV cell. The stats documentation lists, per
// column, the row types that have a value; for the others the cell is empty.
// An empty or non-numeric cell therefore means "not applicable" and yields no
// point, rather than a 0 that reads as a measurement.
func appendCell(points []data_store.DataPoint, name, cell string, ts time.Time, t []tags.Tag) []data_store.DataPoint {
	v, err := strconv.ParseFloat(strings.TrimSpace(cell), 64)
	if err != nil {
		return points
	}
	return append(points, data_store.DataPoint{Name: name, Value: v, Timestamp: ts, Tags: t})
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
