// Package clickhouse implements the free clickhouse probe: monitors a
// ClickHouse server through its HTTP interface (port 8123) by reading
// system.metrics, system.events and system.asynchronous_metrics.
//
// The Prometheus endpoint is not used: it is off by default and needs a
// <prometheus> block and a port of its own, whereas the HTTP interface
// answers on every install.
//
// The probe maps a fixed set of these to OTel-canonical names and always
// emits senhub.clickhouse.up so the pipeline has a health signal even when
// the server is unreachable.
package clickhouse

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in the YAML transformer
// and licence catalogue.
const ProbeType = "clickhouse"

const (
	defaultEndpoint = "http://localhost:8123"
	defaultUsername = "default"
	defaultDatabase = "system"
	defaultTimeout  = 10 * time.Second
	defaultInterval = 60 * time.Second

	maxBodyBytes = 1 << 20
)

// ClickHouse system tables the probe reads.
const (
	tableMetrics      = "metrics"
	tableEvents       = "events"
	tableAsyncMetrics = "asynchronous_metrics"
)

// metricMapping links ClickHouse system-table rows to the OTel-canonical
// internal name the probe emits. Several names are summed into one value.
type metricMapping struct {
	table        string
	names        []string
	internalName string
}

// knownMetrics is the curated set the probe collects. system.events only
// lists events that have happened at least once, so an event absent from
// the answer is reported as 0 rather than left out.
var knownMetrics = []metricMapping{
	{table: tableMetrics, names: []string{"Query"}, internalName: "clickhouse.queries.active"},
	{table: tableMetrics, names: []string{"TCPConnection", "HTTPConnection", "MySQLConnection", "PostgreSQLConnection"}, internalName: "clickhouse.connections"},
	{table: tableMetrics, names: []string{"MemoryTracking"}, internalName: "clickhouse.memory.used"},
	{table: tableMetrics, names: []string{"PartsActive"}, internalName: "clickhouse.parts.active"},
	{table: tableMetrics, names: []string{"Merge"}, internalName: "clickhouse.merges.active"},
	{table: tableAsyncMetrics, names: []string{"Uptime"}, internalName: "clickhouse.uptime"},
	{table: tableEvents, names: []string{"Query"}, internalName: "clickhouse.queries.total"},
	{table: tableEvents, names: []string{"SelectQuery"}, internalName: "clickhouse.queries.select"},
	{table: tableEvents, names: []string{"InsertQuery"}, internalName: "clickhouse.queries.insert"},
	{table: tableEvents, names: []string{"InsertedRows"}, internalName: "clickhouse.inserted.rows"},
	{table: tableEvents, names: []string{"InsertedBytes"}, internalName: "clickhouse.inserted.data"},
	{table: tableEvents, names: []string{"ReadCompressedBytes"}, internalName: "clickhouse.read.data"},
	{table: tableEvents, names: []string{"MergeTreeDataWriterCompressedBytes"}, internalName: "clickhouse.written.data"},
}

// systemQuery reads every row knownMetrics needs in one round trip.
var systemQuery = buildSystemQuery()

func buildSystemQuery() string {
	byTable := map[string][]string{}
	for _, m := range knownMetrics {
		for _, n := range m.names {
			byTable[m.table] = append(byTable[m.table], "'"+n+"'")
		}
	}
	column := map[string]string{tableMetrics: "metric", tableEvents: "event", tableAsyncMetrics: "metric"}
	var parts []string
	for _, t := range []string{tableMetrics, tableEvents, tableAsyncMetrics} {
		parts = append(parts, fmt.Sprintf("SELECT '%s', %s, toFloat64(value) FROM system.%s WHERE %s IN (%s)",
			t, column[t], t, column[t], strings.Join(byTable[t], ",")))
	}
	return strings.Join(parts, " UNION ALL ") + " FORMAT TabSeparated"
}

type probeConfig struct {
	Endpoint     string
	Username     string
	Password     string
	Database     string
	InstanceName string
	Timeout      time.Duration
	Interval     time.Duration
}

// ClickHouseProbe monitors a single ClickHouse server through its HTTP interface.
type ClickHouseProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client

	// entitySrc feeds the Toise topology inventory (db.clickhouse entity).
	entitySrc *clickhouseEntitySource
}

// NewClickHouseProbe constructs the probe. Config errors surface here.
func NewClickHouseProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.clickhouse")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &ClickHouseProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
	probe.SetProbeType(ProbeType)
	probe.entitySrc = newClickhouseEntitySource(cfg.InstanceName, cfg.Endpoint)
	probe.SetEntitySource(probe.entitySrc)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Endpoint: defaultEndpoint,
		Username: defaultUsername,
		Database: defaultDatabase,
		Timeout:  defaultTimeout,
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := config["username"].(string); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := config["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := config["database"].(string); ok && v != "" {
		// Every query the probe issues names the system database, so this
		// changes nothing. It is still read so an existing configuration
		// does not start reporting an unknown key.
		cfg.Database = v
	}
	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	return cfg, nil
}

func (p *ClickHouseProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ClickHouseProbe) ShouldStart() bool          { return true }
func (p *ClickHouseProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *ClickHouseProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.config.Endpoint).
		Str("username", p.config.Username).
		Msg("Starting clickhouse probe")
	return nil
}

func (p *ClickHouseProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect reads the system tables and maps the known ClickHouse rows to
// OTel-canonical names. A failing read is recorded as up=0; Collect
// always returns nil so the framework does not mark the probe unhealthy.
//
// On the first successful scrape, Collect also fetches the server UUID via
// SELECT serverUUID() and pins the db entity id ("clickhouse:<uuid>"). Once
// pinned the id is immutable. When the server does not expose a UUID (pre-
// 21.x or the query fails) the entity source falls back to host:port on the
// next Observe call, which is the documented db degraded fallback.
func (p *ClickHouseProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "instance", Value: p.config.Endpoint},
		{Key: "metric_type", Value: "overview"},
	}

	up := float64(1)
	rows, err := p.fetchSystemRows()
	if err != nil {
		up = 0
		p.entitySrc.setReachable(false, "")
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.config.Endpoint).Msg("clickhouse collection failed")
	} else {
		// Try to pin the instance id on the first successful collect.
		// isPinned() is a no-op check when instance_name was already set.
		if !p.entitySrc.isPinned() {
			uuid, uuidErr := p.fetchServerUUID()
			if uuidErr != nil {
				p.moduleLogger.Debug().Err(uuidErr).Msg("clickhouse serverUUID unavailable; falling back to host:port")
			}
			// pinTechID accepts an empty uuid and falls back to host:port — the
			// documented db degraded fallback when no stable tech id is available.
			p.entitySrc.pinTechID(uuid)
		}
		p.entitySrc.setReachable(true, "")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.clickhouse.up", Value: up, Timestamp: now, Tags: baseTags},
	}

	if err == nil {
		for _, m := range knownMetrics {
			var value float64
			found := false
			for _, n := range m.names {
				if v, ok := rows[m.table+"."+n]; ok {
					value += v
					found = true
				}
			}
			if !found && m.table != tableEvents {
				continue
			}
			points = append(points, data_store.DataPoint{
				Name:      m.internalName,
				Value:     value,
				Timestamp: now,
				Tags:      baseTags,
			})
		}
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchSystemRows runs systemQuery and returns the values keyed by
// "<table>.<name>".
func (p *ClickHouseProbe) fetchSystemRows() (map[string]float64, error) {
	body, err := p.query(systemQuery)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]float64)
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		v, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		rows[fields[0]+"."+fields[1]] = v
	}
	return rows, nil
}

// query POSTs one SQL statement to the HTTP interface and returns the
// answer. ClickHouse puts the reason for a refusal in the body, so it is
// carried into the error.
func (p *ClickHouseProbe) query(sql string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, p.config.Endpoint+"/", strings.NewReader(sql))
	if err != nil {
		return "", fmt.Errorf("building query request: %w", err)
	}
	if p.config.Username != "" || p.config.Password != "" {
		req.SetBasicAuth(p.config.Username, p.config.Password)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", p.config.Endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading answer from %s: %w", p.config.Endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _, _ := strings.Cut(strings.TrimSpace(string(raw)), "\n")
		return "", fmt.Errorf("POST %s: HTTP %d: %s", p.config.Endpoint, resp.StatusCode, msg)
	}
	return string(raw), nil
}

// fetchServerUUID queries SELECT serverUUID() through the ClickHouse HTTP
// interface and returns the raw UUID string (e.g.
// "a1b2c3d4-e5f6-7890-abcd-ef1234567890"). Returns "" + non-nil error when
// the server is unreachable or does not support serverUUID() (pre-21.x).
// The caller pins the result via entitySrc.pinTechID.
func (p *ClickHouseProbe) fetchServerUUID() (string, error) {
	body, err := p.query("SELECT serverUUID()")
	if err != nil {
		return "", fmt.Errorf("serverUUID: %w", err)
	}
	uuid := strings.TrimSpace(body)
	if uuid == "" {
		return "", fmt.Errorf("serverUUID returned empty response")
	}
	return uuid, nil
}
