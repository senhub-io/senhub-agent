// Package couchdb implements the FREE-tier CouchDB monitoring probe.
// It queries the CouchDB /_node/_local/_stats endpoint using BasicAuth
// and emits counters and gauges aligned with OTel semantic conventions.
package couchdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable type identifier — must match couchdb.yaml.
const ProbeType = "couchdb"

const (
	defaultEndpoint = "http://localhost:5984"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second
)

// couchdbConfig holds the validated probe configuration.
type couchdbConfig struct {
	Endpoint     string
	Username     string
	Password     string
	Timeout      time.Duration
	Interval     time.Duration
	InstanceName string // optional stable id override (db.instance.id)
}

// CouchDBProbe polls a CouchDB node stats endpoint.
type CouchDBProbe struct {
	*types.BaseProbe
	cfg          couchdbConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySrc    *couchdbEntitySource
	unregister   func()
}

// statsResponse maps the relevant fields of GET /_node/_local/_stats.
// CouchDB returns deeply-nested JSON: {"httpd":{"requests":{"value":123,...},...},...}.
type statsResponse struct {
	HTTPD struct {
		Requests struct {
			Value float64 `json:"value"`
		} `json:"requests"`
	} `json:"httpd"`

	HTTPDRequestMethods struct {
		GET struct {
			Value float64 `json:"value"`
		} `json:"GET"`
		POST struct {
			Value float64 `json:"value"`
		} `json:"POST"`
		PUT struct {
			Value float64 `json:"value"`
		} `json:"PUT"`
		DELETE struct {
			Value float64 `json:"value"`
		} `json:"DELETE"`
	} `json:"httpd_request_methods"`

	HTTPDStatusCodes struct {
		S200 struct {
			Value float64 `json:"value"`
		} `json:"200"`
		S201 struct {
			Value float64 `json:"value"`
		} `json:"201"`
		S400 struct {
			Value float64 `json:"value"`
		} `json:"400"`
		S401 struct {
			Value float64 `json:"value"`
		} `json:"401"`
		S404 struct {
			Value float64 `json:"value"`
		} `json:"404"`
		S500 struct {
			Value float64 `json:"value"`
		} `json:"500"`
	} `json:"httpd_status_codes"`

	OpenDatabases struct {
		Value float64 `json:"value"`
	} `json:"open_databases"`

	OpenOSFiles struct {
		Value float64 `json:"value"`
	} `json:"open_os_files"`

	DatabaseReads struct {
		Value float64 `json:"value"`
	} `json:"database_reads"`

	DatabaseWrites struct {
		Value float64 `json:"value"`
	} `json:"database_writes"`

	IOInput struct {
		Value float64 `json:"value"`
	} `json:"io_input"`

	IOOutput struct {
		Value float64 `json:"value"`
	} `json:"io_output"`
}

// rootResponse maps the fields returned by GET / on a CouchDB node.
// The uuid field is a permanent, server-assigned identifier that never
// changes across restarts — used as the stable db.instance.id.
type rootResponse struct {
	UUID    string `json:"uuid"`
	Version string `json:"version"`
}

// NewCouchDBProbe constructs the probe from the YAML params block.
func NewCouchDBProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.couchdb")

	cfg := couchdbConfig{
		Endpoint: defaultEndpoint,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
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
	if v, ok := config["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	p := &CouchDBProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
	p.SetProbeType(ProbeType)

	p.entitySrc = newCouchDBEntitySource(cfg.Endpoint, cfg.InstanceName)

	p.SetEntitySource(p.entitySrc)
	return p, nil
}

func (p *CouchDBProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *CouchDBProbe) ShouldStart() bool          { return true }
func (p *CouchDBProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *CouchDBProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("Starting CouchDB probe")
	p.unregister = entity.RegisterSource(p.entitySrc)
	return nil
}

func (p *CouchDBProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect queries /_node/_local/_stats (and GET / for the server UUID on
// first successful contact) and emits metrics.
// If the endpoint is unreachable, senhub.couchdb.up=0 is emitted and
// nil is returned — a failing remote is a measurement, not a collection error.
func (p *CouchDBProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "metric_type", Value: "overview"},
	}

	stats, err := p.fetchStats()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).Msg("CouchDB unreachable")
		p.entitySrc.setReachable(false)
		points := []data_store.DataPoint{
			{Name: "senhub.couchdb.up", Value: 0, Timestamp: now, Tags: baseTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	// Fetch server UUID + version from GET / and pin the stable id on the
	// first successful contact. pinServerUUID is a no-op once pinned.
	if root, err := p.fetchRoot(); err == nil {
		p.entitySrc.pinServerUUID(root.UUID)
		p.entitySrc.updateVersion(root.Version)
	}

	p.entitySrc.setReachable(true)
	points := p.buildDatapoints(stats, now)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchStats performs the authenticated GET to /_node/_local/_stats.
func (p *CouchDBProbe) fetchStats() (*statsResponse, error) {
	url := p.cfg.Endpoint + "/_node/_local/_stats"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body from %s: %w", url, err)
	}

	var stats statsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("parsing stats from %s: %w", url, err)
	}
	return &stats, nil
}

// fetchRoot performs GET / to retrieve the server UUID and version.
// The UUID is a permanent per-node identifier assigned at first startup and
// never changes across restarts — the canonical stable db.instance.id source.
func (p *CouchDBProbe) fetchRoot() (*rootResponse, error) {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint+"/", nil)
	if err != nil {
		return nil, fmt.Errorf("building root request for %s: %w", p.cfg.Endpoint, err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.Password)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s/: %w", p.cfg.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s/: unexpected status %d", p.cfg.Endpoint, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading root response from %s: %w", p.cfg.Endpoint, err)
	}

	var root rootResponse
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parsing root response from %s: %w", p.cfg.Endpoint, err)
	}
	return &root, nil
}

// buildDatapoints converts the parsed stats into OTel-first datapoints.
func (p *CouchDBProbe) buildDatapoints(s *statsResponse, ts time.Time) []data_store.DataPoint {
	overviewTags := []tags.Tag{{Key: "metric_type", Value: "overview"}}
	httpdTags := []tags.Tag{{Key: "metric_type", Value: "httpd"}}
	ioTags := []tags.Tag{{Key: "metric_type", Value: "io"}}

	points := []data_store.DataPoint{
		// Availability
		{Name: "senhub.couchdb.up", Value: 1, Timestamp: ts, Tags: overviewTags},

		// Total HTTP requests
		{Name: "couchdb.httpd.requests", Value: float64(s.HTTPD.Requests.Value), Timestamp: ts, Tags: httpdTags},

		// HTTP methods — collapsed via "method" tag
		{Name: "couchdb.httpd.method.requests", Value: float64(s.HTTPDRequestMethods.GET.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "method", Value: "GET"})},
		{Name: "couchdb.httpd.method.requests", Value: float64(s.HTTPDRequestMethods.POST.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "method", Value: "POST"})},
		{Name: "couchdb.httpd.method.requests", Value: float64(s.HTTPDRequestMethods.PUT.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "method", Value: "PUT"})},
		{Name: "couchdb.httpd.method.requests", Value: float64(s.HTTPDRequestMethods.DELETE.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "method", Value: "DELETE"})},

		// HTTP status codes — collapsed via "status" tag
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S200.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "200"})},
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S201.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "201"})},
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S400.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "400"})},
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S401.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "401"})},
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S404.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "404"})},
		{Name: "couchdb.httpd.status.responses", Value: float64(s.HTTPDStatusCodes.S500.Value), Timestamp: ts,
			Tags: append(append([]tags.Tag{}, httpdTags...), tags.Tag{Key: "status", Value: "500"})},

		// Database-level gauges and counters
		{Name: "couchdb.open.databases", Value: float64(s.OpenDatabases.Value), Timestamp: ts, Tags: overviewTags},
		{Name: "couchdb.open.files", Value: float64(s.OpenOSFiles.Value), Timestamp: ts, Tags: overviewTags},
		{Name: "couchdb.database.reads", Value: float64(s.DatabaseReads.Value), Timestamp: ts, Tags: ioTags},
		{Name: "couchdb.database.writes", Value: float64(s.DatabaseWrites.Value), Timestamp: ts, Tags: ioTags},

		// I/O bytes
		{Name: "couchdb.io.bytes.read", Value: float64(s.IOInput.Value), Timestamp: ts, Tags: ioTags},
		{Name: "couchdb.io.bytes.written", Value: float64(s.IOOutput.Value), Timestamp: ts, Tags: ioTags},
	}
	return points
}
