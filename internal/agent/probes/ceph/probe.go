// Package ceph implements the FREE-tier ceph probe: Ceph cluster health,
// OSD counts, monitor quorum and per-pool stats via the Ceph REST API v1.
//
// Authentication: POST /api/auth → bearer token (session-scoped).
// Every subsequent request carries Accept: application/vnd.ceph.api.v1.0+json
// and Authorization: Bearer <token>.
//
// Stdlib only — no external dependency.
package ceph

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable type name used in YAML config and the registry.
const ProbeType = "ceph"

const (
	defaultInterval = 60 * time.Second
	defaultTimeout  = 15 * time.Second
	cephAccept      = "application/vnd.ceph.api.v1.0+json"
	cephContentType = "application/json"
)

// healthStatus maps the Ceph health status string to a numeric value.
// 0 = HEALTH_ERR, 1 = HEALTH_WARN, 2 = HEALTH_OK.
var healthStatus = map[string]float32{
	"HEALTH_ERR":  0,
	"HEALTH_WARN": 1,
	"HEALTH_OK":   2,
}

type probeConfig struct {
	Endpoint     string
	Username     string
	Password     string
	VerifyTLS    bool
	Interval     time.Duration
	InstanceName string // optional operator-supplied stable id override
}

// CephProbe monitors a Ceph cluster through the REST management API.
type CephProbe struct {
	*types.BaseProbe
	cfg                 probeConfig
	moduleLogger        *logger.ModuleLogger
	client              *http.Client
	token               string
	entitySrc           *cephEntitySource
	unregisterEntitySrc func()
}

// NewCephProbe constructs the probe from the raw params block.
func NewCephProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.ceph")

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !cfg.VerifyTLS, // #nosec G402 - operator opt-in for self-signed labs
		},
	}
	client := &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}

	p := &CephProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client:       client,
	}
	p.entitySrc = newCephEntitySource(
		cfg.InstanceName,
		cfg.Endpoint,
		defaultFetchFsid(p),
		defaultGetHostID(),
	)
	p.SetProbeType(ProbeType)
	return p, nil
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint:  "https://localhost:8443",
		VerifyTLS: true,
		Interval:  defaultInterval,
	}

	if v, ok := raw["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = strings.TrimRight(v, "/")
	}
	if v, ok := raw["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := raw["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := raw["verify_tls"].(bool); ok {
		cfg.VerifyTLS = v
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	if cfg.Username == "" {
		return cfg, fmt.Errorf("ceph: username is required")
	}
	if cfg.Password == "" {
		return cfg, fmt.Errorf("ceph: password is required")
	}
	return cfg, nil
}

func (p *CephProbe) ShouldStart() bool          { return true }
func (p *CephProbe) GetInterval() time.Duration { return p.cfg.Interval }
func (p *CephProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *CephProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySrc = entity.RegisterSource(p.entitySrc)
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Msg("ceph probe started")
	return nil
}

func (p *CephProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySrc != nil {
		p.unregisterEntitySrc()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect authenticates once per cycle, then fetches health/OSD/monitor/pool
// data. A connection or auth failure is not a collection error: the probe
// emits senhub.ceph.up=0 so the outage is observable.
func (p *CephProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	instance := p.cfg.Endpoint

	baseTags := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "overview"},
	}

	// Authenticate — get a fresh token each cycle to avoid expiry edge cases.
	token, err := p.authenticate()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", instance).Msg("ceph auth failed")
		up := data_store.DataPoint{
			Name:      "senhub.ceph.up",
			Value:     0,
			Timestamp: now,
			Tags:      baseTags,
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{up}, p.GetName()), nil
	}
	p.token = token

	// Pin the entity id on the first successful collect (requires a live token
	// so that GET /api/cluster can resolve the cluster fsid). Subsequent calls
	// are no-ops once the id is pinned.
	p.entitySrc.pinID()

	var points []data_store.DataPoint
	points = append(points, data_store.DataPoint{
		Name:      "senhub.ceph.up",
		Value:     1,
		Timestamp: now,
		Tags:      baseTags,
	})

	// Health
	if hp, err := p.collectHealth(now, instance); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("ceph health fetch failed")
	} else {
		points = append(points, hp...)
	}

	// OSDs
	if op, err := p.collectOSDs(now, instance); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("ceph osd fetch failed")
	} else {
		points = append(points, op...)
	}

	// Monitors
	if mp, err := p.collectMonitors(now, instance); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("ceph monitor fetch failed")
	} else {
		points = append(points, mp...)
	}

	// Pools (multi-instance)
	if pp, err := p.collectPools(now, instance); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("ceph pool fetch failed")
	} else {
		points = append(points, pp...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// authenticate posts credentials to /api/auth and returns the bearer token.
func (p *CephProbe) authenticate() (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username": p.cfg.Username,
		"password": p.cfg.Password,
	})

	req, err := http.NewRequest(http.MethodPost, p.cfg.Endpoint+"/api/auth", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building auth request: %w", err)
	}
	req.Header.Set("Content-Type", cephContentType)
	req.Header.Set("Accept", cephAccept)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding auth response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("auth response contains no token")
	}
	return result.Token, nil
}

// apiGet performs an authenticated GET request and returns the body bytes.
func (p *CephProbe) apiGet(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Accept", cephAccept)
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// --- health ---

type healthFullResponse struct {
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
	ClientPerf struct {
		ReadBytesSec  float64 `json:"read_bytes_sec"`
		WriteBytesSec float64 `json:"write_bytes_sec"`
	} `json:"client_perf"`
	Df struct {
		Stats struct {
			TotalBytes     float64 `json:"total_bytes"`
			TotalUsedBytes float64 `json:"total_used_bytes"`
		} `json:"stats"`
	} `json:"df"`
}

func (p *CephProbe) collectHealth(now time.Time, instance string) ([]data_store.DataPoint, error) {
	raw, err := p.apiGet("/api/health/full")
	if err != nil {
		return nil, err
	}

	var h healthFullResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("decoding health: %w", err)
	}

	statusVal, ok := healthStatus[h.Health.Status]
	if !ok {
		statusVal = 0
	}

	base := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "overview"},
	}

	return []data_store.DataPoint{
		{Name: "ceph.health.status", Value: statusVal, Timestamp: now, Tags: base},
		{Name: "ceph.cluster.capacity", Value: float32(h.Df.Stats.TotalBytes), Timestamp: now, Tags: base},
		{Name: "ceph.cluster.used", Value: float32(h.Df.Stats.TotalUsedBytes), Timestamp: now, Tags: base},
	}, nil
}

// --- OSDs ---

type osdDumpResponse struct {
	OSDs []struct {
		Up int `json:"up"`
		In int `json:"in"`
	} `json:"osds"`
}

func (p *CephProbe) collectOSDs(now time.Time, instance string) ([]data_store.DataPoint, error) {
	raw, err := p.apiGet("/api/osd")
	if err != nil {
		return nil, err
	}

	// The /api/osd endpoint returns an array of OSD descriptors.
	var osds []struct {
		OSDInfo struct {
			Up int `json:"up"`
			In int `json:"in"`
		} `json:"osd_info"`
	}
	if err := json.Unmarshal(raw, &osds); err != nil {
		return nil, fmt.Errorf("decoding osd list: %w", err)
	}

	var total, inCount, upCount int
	for _, o := range osds {
		total++
		if o.OSDInfo.Up == 1 {
			upCount++
		}
		if o.OSDInfo.In == 1 {
			inCount++
		}
	}

	base := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "osd"},
	}
	return []data_store.DataPoint{
		{Name: "ceph.osd.total", Value: float32(total), Timestamp: now, Tags: base},
		{Name: "ceph.osd.in", Value: float32(inCount), Timestamp: now, Tags: base},
		{Name: "ceph.osd.up", Value: float32(upCount), Timestamp: now, Tags: base},
	}, nil
}

// --- monitors ---

type monitorResponse struct {
	InQuorum []struct{} `json:"in_quorum"`
	Mons     []struct{} `json:"mons"`
}

func (p *CephProbe) collectMonitors(now time.Time, instance string) ([]data_store.DataPoint, error) {
	raw, err := p.apiGet("/api/monitor")
	if err != nil {
		return nil, err
	}

	var mon monitorResponse
	if err := json.Unmarshal(raw, &mon); err != nil {
		return nil, fmt.Errorf("decoding monitor: %w", err)
	}

	base := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "monitor"},
	}
	return []data_store.DataPoint{
		{Name: "ceph.monitor.count", Value: float32(len(mon.Mons)), Timestamp: now, Tags: base},
		{Name: "ceph.monitor.quorum_count", Value: float32(len(mon.InQuorum)), Timestamp: now, Tags: base},
	}, nil
}

// --- pools ---

type poolEntry struct {
	PoolName string `json:"pool_name"`
	Stats    struct {
		Objects struct {
			Latest float64 `json:"latest"`
		} `json:"objects"`
		Used struct {
			Latest float64 `json:"latest"`
		} `json:"stored"`
		ReadOps struct {
			Latest float64 `json:"latest"`
		} `json:"rd_ops"`
		WriteOps struct {
			Latest float64 `json:"latest"`
		} `json:"wr_ops"`
	} `json:"stats"`
}

func (p *CephProbe) collectPools(now time.Time, instance string) ([]data_store.DataPoint, error) {
	raw, err := p.apiGet("/api/pool")
	if err != nil {
		return nil, err
	}

	var pools []poolEntry
	if err := json.Unmarshal(raw, &pools); err != nil {
		return nil, fmt.Errorf("decoding pool list: %w", err)
	}

	var points []data_store.DataPoint
	for _, pool := range pools {
		base := []tags.Tag{
			{Key: "instance", Value: instance},
			{Key: "pool", Value: pool.PoolName},
			{Key: "metric_type", Value: "pool"},
		}
		points = append(points,
			data_store.DataPoint{Name: "ceph.pool.objects", Value: float32(pool.Stats.Objects.Latest), Timestamp: now, Tags: base},
			data_store.DataPoint{Name: "ceph.pool.used", Value: float32(pool.Stats.Used.Latest), Timestamp: now, Tags: base},
			data_store.DataPoint{Name: "ceph.pool.rd_ops", Value: float32(pool.Stats.ReadOps.Latest), Timestamp: now, Tags: base},
			data_store.DataPoint{Name: "ceph.pool.wr_ops", Value: float32(pool.Stats.WriteOps.Latest), Timestamp: now, Tags: base},
		)
	}
	return points, nil
}
