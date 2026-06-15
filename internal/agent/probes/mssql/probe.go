// Package mssql implements the free mssql probe: SQL Server health and
// throughput over database/sql. It mirrors the otelcol-contrib
// sqlserverreceiver metric set so dashboards built for that receiver are a
// drop-in fit, and feeds the entity rail with the monitored instance as a
// "db" entity (Toise discovery).
//
// Sources: sys.dm_os_performance_counters (the bulk of the perf metrics),
// sys.databases (per-database status) and sys.dm_io_virtual_file_stats
// (per-database read/write I/O). No agent install on the SQL host is needed —
// a single read-only login with VIEW SERVER STATE is enough.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier — part of licence JWT claims,
// the transformer file name (mssql.yaml) and customer configs. Never rename.
const ProbeType = "mssql"

const (
	defaultPort     = 1433
	defaultInterval = 60 * time.Second
	// queryTimeout bounds a single collect cycle's SQL work so a wedged
	// server never stalls the scheduler.
	queryTimeout = 10 * time.Second
)

// metric_type families — drive Sensor Builder chips and dashboard grouping.
const (
	metricTypeOverview    = "overview"
	metricTypeThroughput  = "throughput"
	metricTypeCache       = "cache"
	metricTypeConnections = "connections"
	metricTypeLocks       = "locks"
	metricTypeIO          = "io"
	metricTypeStorage     = "storage"
)

type config struct {
	Host            string
	Port            int
	Username        string
	Password        string
	Encrypt         string
	TrustServerCert bool
	Interval        time.Duration
}

// MSSQLProbe polls one SQL Server instance per cycle. A connection or query
// failure is a measurement (senhub.db.up=0), never a collection error.
type MSSQLProbe struct {
	*types.BaseProbe
	cfg          config
	instance     string
	dsn          string
	moduleLogger *logger.ModuleLogger
	entitySource *mssqlEntitySource

	db *sql.DB

	unregisterEntitySource func()
}

// NewMSSQLProbe builds an mssql probe from its raw params block. Config errors
// surface here; the SQL connection is opened lazily in OnStart.
func NewMSSQLProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.mssql")

	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	instance := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	probe := &MSSQLProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		dsn:          buildDSN(cfg),
		moduleLogger: moduleLogger,
		entitySource: newEntitySource(cfg.Host, cfg.Port),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(raw map[string]interface{}) (config, error) {
	cfg := config{
		Port: defaultPort,
		// Encrypt defaults to "true": TLS on the wire is the safe default,
		// and an operator must opt out explicitly for a legacy server that
		// cannot negotiate it. go-mssqldb accepts true/false/disable/strict.
		Encrypt:         "true",
		TrustServerCert: false,
		Interval:        defaultInterval,
	}

	host, _ := raw["host"].(string)
	if host == "" {
		return cfg, fmt.Errorf("mssql requires a non-empty host")
	}
	cfg.Host = host

	if v, ok := raw["port"].(int); ok && v > 0 {
		cfg.Port = v
	}
	if v, ok := raw["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := raw["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := raw["encrypt"].(string); ok && v != "" {
		switch v {
		case "true", "false", "disable", "strict":
			cfg.Encrypt = v
		default:
			return cfg, fmt.Errorf("mssql encrypt must be one of true/false/disable/strict, got %q", v)
		}
	}
	if v, ok := raw["trust_server_cert"].(bool); ok {
		cfg.TrustServerCert = v
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	return cfg, nil
}

// buildDSN assembles the sqlserver:// URL the go-mssqldb driver expects.
// The URL builder percent-escapes every component, so a ';' or '&' in a
// username or password can no longer smuggle extra DSN parameters (the
// ';'-joined form did — a password of "x;encrypt=disable" would have
// silently downgraded the connection).
func buildDSN(cfg config) string {
	q := url.Values{}
	q.Set("encrypt", cfg.Encrypt)
	q.Set("TrustServerCertificate", strconv.FormatBool(cfg.TrustServerCert))

	u := url.URL{
		Scheme:   "sqlserver",
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		RawQuery: q.Encode(),
	}
	if cfg.Username != "" {
		if cfg.Password != "" {
			u.User = url.UserPassword(cfg.Username, cfg.Password)
		} else {
			u.User = url.User(cfg.Username)
		}
	}
	return u.String()
}

func (p *MSSQLProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *MSSQLProbe) ShouldStart() bool          { return true }
func (p *MSSQLProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens the connection pool and registers the entity source. A failed
// open here marks the probe unhealthy; a failed ping is tolerated (the server
// may be briefly down) — Collect reports it as senhub.db.up=0.
func (p *MSSQLProbe) OnStart(_ chan struct{}) error {
	db, err := sql.Open("sqlserver", p.dsn)
	if err != nil {
		return fmt.Errorf("opening mssql connection to %s: %w", p.instance, err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	p.db = db

	p.unregisterEntitySource = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Msg("mssql probe started")
	return nil
}

// Collect runs one poll cycle. It always emits senhub.db.up; a connection or
// query failure yields up=0 and no further metrics rather than an error, so the
// outage is observable on every sink (always-emit-up contract of the DB probes).
func (p *MSSQLProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var points []data_store.DataPoint
	up := float32(1)

	if p.db == nil {
		up = 0
	} else if err := p.db.PingContext(ctx); err != nil {
		up = 0
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("mssql ping failed")
	}

	points = append(points, data_store.DataPoint{
		Name: "senhub.db.up", Value: up, Timestamp: now, Tags: p.baseTags(metricTypeOverview),
	})

	if up == 1 {
		points = append(points, p.collectPerformanceCounters(ctx, now)...)
		points = append(points, p.collectDatabaseIO(ctx, now)...)
		points = append(points, p.collectDatabaseStatus(ctx, now)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// OnShutdown closes the pool and unregisters the entity source so a stopped or
// reloaded probe stops heartbeating the cached entity (audit D4).
func (p *MSSQLProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("error closing mssql connection")
		}
	}
	return nil
}

// baseTags carries the OTel-canonical resource attributes plus the standard
// per-probe tags on every datapoint, so they flow to OTLP / Prom via
// IncludeProbeTags (see .claude/rules/probes.md commonTags).
func (p *MSSQLProbe) baseTags(metricType string) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: metricType},
		{Key: "instance", Value: p.instance},
		{Key: "db.system.name", Value: "mssql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}
}

// taggedWith clones baseTags and appends discriminating key/value pairs —
// used for multi_instance metrics (per-database, per-direction).
func (p *MSSQLProbe) taggedWith(metricType string, extra ...tags.Tag) []tags.Tag {
	base := p.baseTags(metricType)
	return append(base, extra...)
}
