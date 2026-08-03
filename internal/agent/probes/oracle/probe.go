// Package oracle implements the free-tier oracle probe: Oracle Database
// monitoring through go-ora (pure Go, no CGO / OCI client required). The
// metric set targets parity with the community oracledb_exporter — up,
// sessions, SGA/PGA, buffer cache hit ratio, tablespace usage, wait
// classes and enqueue deadlocks — read from the v$ dynamic performance
// views and dba_tablespace_usage_metrics.
package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	go_ora "github.com/sijms/go-ora/v2"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier — part of license JWT
// claims, the transformer file name and DiscriminantTagsRegistry keys.
const ProbeType = "oracle"

const (
	metricTypeOverview    = "overview"
	metricTypeConnections = "connections"
	metricTypeIO          = "io"
	metricTypeCache       = "cache"
	metricTypeMemory      = "memory"
	metricTypeStorage     = "storage"
	metricTypeWait        = "wait"
	metricTypeLocks       = "locks"

	queryTimeout = 15 * time.Second
)

type oracleProbe struct {
	*types.BaseProbe
	cfg          config
	instance     string
	moduleLogger *logger.ModuleLogger

	db           *sql.DB
	entitySource *oracleEntitySource
}

// NewOracleProbe builds an oracle probe from its raw params block.
// Config errors surface here; the connection itself is opened in OnStart.
func NewOracleProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.oracle")
	instance := cfg.instance()

	probe := &oracleProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		moduleLogger: moduleLogger,
		entitySource: newEntitySource(instance, cfg.Host),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySource)
	return probe, nil
}

func (p *oracleProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *oracleProbe) ShouldStart() bool          { return true }
func (p *oracleProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens the database/sql handle (go-ora is registered under the
// "oracle" driver name). sql.Open does not dial — the first ping happens
// in Collect, so a database that is down at agent start does not block
// the probe; it reports up=0 instead.
func (p *oracleProbe) OnStart(_ chan struct{}) error {
	dsn := go_ora.BuildUrl(p.cfg.Host, p.cfg.Port, p.cfg.ServiceName, p.cfg.Username, p.cfg.Password, nil)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("opening oracle connection to %s: %w", p.instance, err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	p.db = db

	p.moduleLogger.Info().Str("instance", p.instance).Msg("Oracle probe started")
	return nil
}

// OnShutdown closes the pool.
func (p *oracleProbe) OnShutdown(_ context.Context) error {
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("error closing oracle connection")
		}
		p.db = nil
	}
	return nil
}

// Collect runs one cycle. A connection or query failure is a measurement
// (senhub.db.up=0), never a collection error — the outage stays
// observable, mirroring the always-emit-up contract of the DB probes.
func (p *oracleProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	var points []data_store.DataPoint

	up := float64(1)
	if p.db == nil || p.db.PingContext(ctx) != nil {
		up = 0
	}
	points = append(points, p.point("senhub.db.up", up, now, metricTypeOverview, nil))

	if up == 1 {
		p.entitySource.setVersion(p.queryVersion(ctx))
		points = append(points, p.collectSessions(ctx, now)...)
		points = append(points, p.collectSysstat(ctx, now)...)
		points = append(points, p.collectMemory(ctx, now)...)
		points = append(points, p.collectTablespaces(ctx, now)...)
		points = append(points, p.collectWaitClasses(ctx, now)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// queryVersion reads the Oracle release from v$instance ("19.0.0.0.0"),
// best-effort: "" when the query fails (restricted view) so the entity simply
// omits db.system.version rather than carrying a wrong value.
func (p *oracleProbe) queryVersion(ctx context.Context) string {
	var v string
	if err := p.db.QueryRowContext(ctx, "SELECT version FROM v$instance").Scan(&v); err != nil {
		return ""
	}
	return v
}

// collectSessions reads v$session (per-status counts) and v$resource_limit
// (session limit). active/inactive ride the same metric, discriminated by
// the `status` tag (multi_instance).
func (p *oracleProbe) collectSessions(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	rows, err := p.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM v$session GROUP BY status")
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$session failed")
	} else {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count float64
			if err := rows.Scan(&status, &count); err != nil {
				p.moduleLogger.Warn().Err(err).Msg("scan v$session row failed")
				continue
			}
			points = append(points, p.point("oracle.sessions.count", float64(count), now, metricTypeConnections,
				map[string]string{"status": normalizeStatus(status)}))
		}
	}

	var limit sql.NullFloat64
	row := p.db.QueryRowContext(ctx,
		"SELECT limit_value FROM v$resource_limit WHERE resource_name = 'sessions'")
	if err := row.Scan(&limit); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$resource_limit (sessions) failed")
	} else if limit.Valid {
		points = append(points, p.point("oracle.sessions.limit", float64(limit.Float64), now, metricTypeConnections, nil))
	}

	return points
}

// collectSysstat reads physical reads/writes and the buffer cache hit
// ratio from v$sysstat. The hit ratio is derived from the consistent
// gets / db block gets / physical reads counters (oracledb_exporter
// parity).
func (p *oracleProbe) collectSysstat(ctx context.Context, now time.Time) []data_store.DataPoint {
	stats, err := p.sysstat(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$sysstat failed")
		return nil
	}

	var points []data_store.DataPoint
	physReads := stats["physical reads"]
	points = append(points,
		p.point("oracle.physical.reads", float64(physReads), now, metricTypeIO, nil),
		p.point("oracle.physical.writes", float64(stats["physical writes"]), now, metricTypeIO, nil),
	)

	// Buffer cache hit ratio = 1 - physical reads / (consistent gets +
	// db block gets), emitted as a percentage. Bounded to [0,100];
	// undefined (no logical reads) is reported as 100 (a freshly started
	// instance has nothing to miss).
	logical := stats["consistent gets"] + stats["db block gets"]
	hit := float64(1)
	if logical > 0 {
		hit = 1 - physReads/logical
		if hit < 0 {
			hit = 0
		}
		if hit > 1 {
			hit = 1
		}
	}
	points = append(points, p.point("oracle.buffer.cache.hit_ratio", hit*100, now, metricTypeCache, nil))

	deadlocks, ok := stats["enqueue deadlocks"]
	if ok {
		points = append(points, p.point("oracle.enqueue_deadlocks", float64(deadlocks), now, metricTypeLocks, nil))
	}

	return points
}

func (p *oracleProbe) sysstat(ctx context.Context) (map[string]float64, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT name, value FROM v$sysstat")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]float64, 16)
	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}

// collectMemory reads total SGA (v$sgastat aggregate) and total PGA
// (v$pgastat "total PGA allocated"), both in bytes.
func (p *oracleProbe) collectMemory(ctx context.Context, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	var sga sql.NullFloat64
	if err := p.db.QueryRowContext(ctx, "SELECT SUM(bytes) FROM v$sgastat").Scan(&sga); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$sgastat failed")
	} else if sga.Valid {
		points = append(points, p.point("oracle.sga.total", float64(sga.Float64), now, metricTypeMemory, nil))
	}

	var pga sql.NullFloat64
	if err := p.db.QueryRowContext(ctx,
		"SELECT value FROM v$pgastat WHERE name = 'total PGA allocated'").Scan(&pga); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$pgastat failed")
	} else if pga.Valid {
		points = append(points, p.point("oracle.pga.total", float64(pga.Float64), now, metricTypeMemory, nil))
	}

	return points
}

// collectTablespaces reads used/total bytes per tablespace from
// dba_tablespace_usage_metrics joined with dba_tablespaces. used and
// total ride per-tablespace metrics, discriminated by `tablespace`
// (multi_instance).
func (p *oracleProbe) collectTablespaces(ctx context.Context, now time.Time) []data_store.DataPoint {
	const q = `SELECT m.tablespace_name, m.used_space * t.block_size, m.tablespace_size * t.block_size
		FROM dba_tablespace_usage_metrics m
		JOIN dba_tablespaces t ON t.tablespace_name = m.tablespace_name`

	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query dba_tablespace_usage_metrics failed")
		return nil
	}
	defer rows.Close()

	var points []data_store.DataPoint
	for rows.Next() {
		var name string
		var used, total float64
		if err := rows.Scan(&name, &used, &total); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("scan tablespace row failed")
			continue
		}
		ts := map[string]string{"tablespace": name}
		points = append(points,
			p.point("oracle.tablespace.used", float64(used), now, metricTypeStorage, ts),
			p.point("oracle.tablespace.total", float64(total), now, metricTypeStorage, ts),
		)
	}
	return points
}

// collectWaitClasses reads cumulative time waited per wait class from
// v$system_wait_class, discriminated by `wait_class` (multi_instance).
func (p *oracleProbe) collectWaitClasses(ctx context.Context, now time.Time) []data_store.DataPoint {
	rows, err := p.db.QueryContext(ctx,
		"SELECT wait_class, time_waited FROM v$system_wait_class")
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("query v$system_wait_class failed")
		return nil
	}
	defer rows.Close()

	var points []data_store.DataPoint
	for rows.Next() {
		var class string
		var timeWaited float64
		if err := rows.Scan(&class, &timeWaited); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("scan wait class row failed")
			continue
		}
		points = append(points, p.point("oracle.wait_class.total", float64(timeWaited), now, metricTypeWait,
			map[string]string{"wait_class": class}))
	}
	return points
}

// point builds a datapoint with the standard DB tag set plus optional
// per-instance discriminator tags. server.address / server.port /
// db.system.name flow to OTLP / Prom via IncludeProbeTags.
func (p *oracleProbe) point(name string, value float64, ts time.Time, metricType string, extra map[string]string) data_store.DataPoint {
	dpTags := []tags.Tag{
		{Key: "metric_type", Value: metricType},
		{Key: "instance", Value: p.instance},
		{Key: "db.system.name", Value: "oracle"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}
	for k, v := range extra {
		dpTags = append(dpTags, tags.Tag{Key: k, Value: v})
	}
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: dpTags}
}

// normalizeStatus collapses v$session.status to the active/inactive
// dimension the metric advertises; KILLED / SNIPED / CACHED stay as-is
// (lowercased) so an operator still sees them.
func normalizeStatus(status string) string {
	switch status {
	case "ACTIVE":
		return "active"
	case "INACTIVE":
		return "inactive"
	default:
		return toLower(status)
	}
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
