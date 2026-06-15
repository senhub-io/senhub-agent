// Package postgresql implements the FREE-tier postgresql probe.
// It monitors a PostgreSQL server via database/sql (pgx v5 stdlib adapter)
// and emits OTel-first metrics aligned with the otelcol-contrib
// postgresqlreceiver (https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/postgresqlreceiver).
//
// Metric names follow the otelcol-contrib canon where available;
// extensions live under senhub.db.postgresql.* (engine-specific) or
// senhub.db.* (cross-engine).  See docs/developer-guide/otel/senhub-semantic-conventions.md §4.13.
package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ProbeType is the stable technical identifier that matches the YAML
// transformer file name (postgresql.yaml) and the license catalogue.
const ProbeType = "postgresql"

// defaultInterval is used when the operator omits interval.
const defaultInterval = 60 * time.Second

// pgProbe is the runtime state of one postgresql probe instance.
type pgProbe struct {
	*types.BaseProbe

	cfg          config
	db           *sql.DB
	moduleLogger *logger.ModuleLogger
	entitySrc    *pgEntitySource

	unregisterEntity func()
}

// NewPostgreSQLProbe is the ProbeConstructor registered in init().
// It validates configuration but does NOT open a database connection —
// that is deferred to OnStart so the agent can load-validate config
// without network access.
func NewPostgreSQLProbe(params map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.postgresql")

	cfg, err := parseConfig(params)
	if err != nil {
		return nil, err
	}

	p := &pgProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
	}
	p.SetProbeType(ProbeType)
	p.entitySrc = newPgEntitySource(cfg, moduleLogger)
	return p, nil
}

func (p *pgProbe) ShouldStart() bool          { return true }
func (p *pgProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens the connection pool but does NOT verify reachability:
// sql.Open is lazy (no network I/O) and only fails on a malformed DSN, so
// a target that is down at start does not abort the probe. The first real
// connection is made in Collect, which emits senhub.db.up=0 while the
// server is unreachable and reconnects automatically once it returns —
// the pool re-dials on demand. This mirrors the oracle/mssql probes (#485).
func (p *pgProbe) OnStart(_ chan struct{}) error {
	dsn := p.buildDSN()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("postgresql: open: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	p.db = db
	p.unregisterEntity = entity.RegisterSource(p.entitySrc)

	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Msg("postgresql probe started")
	return nil
}

// OnShutdown closes the connection cleanly.
func (p *pgProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntity != nil {
		p.unregisterEntity()
	}
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("postgresql: close connection")
		}
		p.db = nil
	}
	return nil
}

// Collect runs one monitoring cycle. senhub.db.up is always emitted (0
// when the connection is broken); other families are emitted on a
// best-effort basis (partial failure keeps the up gauge and skips the
// rest, logged at Warn).
func (p *pgProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	instance := p.cfg.Host + ":" + strconv.Itoa(p.cfg.Port)

	baseTags := []tags.Tag{
		{Key: "metric_type", Value: string(dbcommon.MetricTypeOverview)},
		{Key: "instance", Value: instance},
		{Key: "db.system.name", Value: "postgresql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}

	// Ping first — the connection is established here (not in OnStart) so a
	// down-at-start target is a recoverable outage, not a fatal probe
	// failure; emit up=0 and return while unreachable, the pool re-dials on
	// demand once the server returns (#485).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if p.db == nil {
		p.moduleLogger.Warn().Str("instance", instance).Msg("postgresql: no connection pool")
		up := p.dp("senhub.db.up", 0, now, baseTags)
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{up}, p.GetName()), nil
	}
	if err := p.db.PingContext(ctx); err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", instance).Msg("postgresql: ping failed")
		up := p.dp("senhub.db.up", 0, now, baseTags)
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{up}, p.GetName()), nil
	}

	var points []data_store.DataPoint
	points = append(points, p.dp("senhub.db.up", 1, now, baseTags))

	// ── Overview ────────────────────────────────────────────────────────────
	p.collectOverview(ctx, now, baseTags, instance, &points)

	// ── Connections / Backends ───────────────────────────────────────────────
	p.collectBackends(ctx, now, instance, &points)

	// ── Throughput (commits / rollbacks) ────────────────────────────────────
	p.collectThroughput(ctx, now, instance, &points)

	// ── Storage (db_size, table count) ──────────────────────────────────────
	p.collectStorage(ctx, now, instance, &points)

	// ── Locks ───────────────────────────────────────────────────────────────
	p.collectLocks(ctx, now, instance, &points)

	// ── WAL / Archiver ──────────────────────────────────────────────────────
	p.collectWAL(ctx, now, instance, &points)

	// ── Replication ─────────────────────────────────────────────────────────
	p.collectReplication(ctx, now, instance, &points)

	// ── Entity update ───────────────────────────────────────────────────────
	// Pin the stable tech id on the first successful collect.  instance_name
	// takes precedence (already pinned in newPgEntitySource); if not set, try
	// the cluster-scoped system_identifier from pg_control_system(). The entity
	// is not emitted until the id is pinned, so we never re-key Toise.
	if p.entitySrc.pinnedID() == "" {
		sysID := p.fetchSystemIdentifier(ctx)
		p.entitySrc.pinTechID(sysID)
	}
	// pass "" so update() uses the already-pinned instanceID; a non-empty
	// fallback is only passed when we deliberately fall back to host:port.
	p.entitySrc.update("")

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// fetchSystemIdentifier queries the PostgreSQL cluster-scoped system identifier
// (a 64-bit integer stable across restarts, replicas, and promotions) from
// pg_control_system(). Returns the decimal string representation, or "" when
// the query fails (older PG versions, restricted permissions).
func (p *pgProbe) fetchSystemIdentifier(ctx context.Context) string {
	var sysID string
	err := p.db.QueryRowContext(ctx,
		`SELECT system_identifier::text FROM pg_control_system()`).Scan(&sysID)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Msg("postgresql: pg_control_system unavailable; db.instance.id pending")
		return ""
	}
	return sysID
}

// ── helpers ────────────────────────────────────────────────────────────────

func (p *pgProbe) dp(name string, value float64, ts time.Time, baseTags []tags.Tag) data_store.DataPoint {
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: baseTags}
}

func (p *pgProbe) dpWithTags(name string, value float64, ts time.Time, base []tags.Tag, extra ...tags.Tag) data_store.DataPoint {
	t := make([]tags.Tag, len(base)+len(extra))
	copy(t, base)
	copy(t[len(base):], extra)
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: t}
}

// tagsFor copies baseTags and overrides metric_type.
func (p *pgProbe) tagsFor(mt dbcommon.MetricType, instance string) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: string(mt)},
		{Key: "instance", Value: instance},
		{Key: "db.system.name", Value: "postgresql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}
}

// ── collectOverview ────────────────────────────────────────────────────────

func (p *pgProbe) collectOverview(ctx context.Context, now time.Time, baseTags []tags.Tag, instance string, points *[]data_store.DataPoint) {
	ot := p.tagsFor(dbcommon.MetricTypeOverview, instance)

	// Uptime: now() - pg_postmaster_start_time()
	var uptimeSec float64
	if err := p.db.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))`,
	).Scan(&uptimeSec); err == nil {
		*points = append(*points, p.dp("senhub.db.postgresql.uptime", float64(uptimeSec), now, ot))
	} else {
		p.moduleLogger.Warn().Err(err).Str("query", "uptime").Msg("postgresql: query failed")
	}

	// Version string
	var version string
	if err := p.db.QueryRowContext(ctx, `SELECT version()`).Scan(&version); err == nil {
		versionTags := make([]tags.Tag, len(ot)+1)
		copy(versionTags, ot)
		versionTags[len(ot)] = tags.Tag{Key: "version", Value: version}
		*points = append(*points, p.dp("senhub.db.version.info", 1, now, versionTags))
		// Detect managed environment from version string
		env := dbcommon.DetectEnvironment(version)
		envTags := make([]tags.Tag, len(baseTags)+1)
		copy(envTags, baseTags)
		envTags[len(baseTags)] = tags.Tag{Key: "environment", Value: string(env)}
		p.entitySrc.setEnvironment(env)
	} else {
		p.moduleLogger.Warn().Err(err).Str("query", "version").Msg("postgresql: query failed")
	}
}

// ── collectBackends ────────────────────────────────────────────────────────

func (p *pgProbe) collectBackends(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	connTags := p.tagsFor(dbcommon.MetricTypeConnections, instance)

	// Backends per state from pg_stat_activity
	rows, err := p.db.QueryContext(ctx, `
		SELECT state, COUNT(*) FROM pg_stat_activity
		WHERE backend_type = 'client backend'
		GROUP BY state`)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("query", "backends").Msg("postgresql: query failed")
		return
	}
	defer rows.Close()

	counts := map[string]float64{}
	for rows.Next() {
		var state sql.NullString
		var cnt float64
		if err := rows.Scan(&state, &cnt); err == nil {
			key := "unknown"
			if state.Valid && state.String != "" {
				key = state.String
			}
			counts[key] += cnt
		}
	}
	_ = rows.Close()

	for _, state := range []string{"active", "idle", "idle in transaction"} {
		val := counts[state]
		tagKey := state
		// Normalise "idle in transaction" → "idle_in_transaction" for tag value.
		if state == "idle in transaction" {
			tagKey = "idle_in_transaction"
		}
		t := append(append([]tags.Tag{}, connTags...), tags.Tag{Key: "state", Value: tagKey})
		*points = append(*points, p.dp("postgresql.backends", val, now, t))
	}

	// max_connections
	var maxConn float64
	if err := p.db.QueryRowContext(ctx, `SHOW max_connections`).Scan(&maxConn); err == nil {
		*points = append(*points, p.dp("postgresql.connection.max", maxConn, now, connTags))

		// Connection utilization ratio
		total := counts["active"] + counts["idle"] + counts["idle in transaction"]
		if maxConn > 0 {
			*points = append(*points, p.dp("senhub.db.connection.utilization", total/maxConn, now, connTags))
		}
	}
}

// ── collectThroughput ──────────────────────────────────────────────────────

func (p *pgProbe) collectThroughput(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	tpt := p.tagsFor(dbcommon.MetricTypeThroughput, instance)

	var commits, rollbacks, blksHit, blksRead float64
	err := p.db.QueryRowContext(ctx, `
		SELECT
			SUM(xact_commit),
			SUM(xact_rollback),
			SUM(blks_hit),
			SUM(blks_read)
		FROM pg_stat_database`).Scan(&commits, &rollbacks, &blksHit, &blksRead)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("query", "throughput").Msg("postgresql: query failed")
		return
	}

	*points = append(*points,
		p.dp("postgresql.commits", float64(commits), now, tpt),
		p.dp("postgresql.rollbacks", float64(rollbacks), now, tpt),
	)

	// Buffer hit ratio (cache family)
	cacheTags := p.tagsFor(dbcommon.MetricTypeCache, instance)
	total := blksHit + blksRead
	if total > 0 {
		*points = append(*points, p.dp("senhub.db.postgresql.buffer.hit_ratio", float64(blksHit/total), now, cacheTags))
	}

	// Deadlocks (locks family)
	locksTags := p.tagsFor(dbcommon.MetricTypeLocks, instance)
	var deadlocks float64
	if err2 := p.db.QueryRowContext(ctx, `SELECT SUM(deadlocks) FROM pg_stat_database`).Scan(&deadlocks); err2 == nil {
		*points = append(*points, p.dp("postgresql.deadlocks", float64(deadlocks), now, locksTags))
	}
}

// ── collectStorage ─────────────────────────────────────────────────────────

func (p *pgProbe) collectStorage(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	stTags := p.tagsFor(dbcommon.MetricTypeStorage, instance)

	// Total size of all non-template databases
	var dbSize float64
	if err := p.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(pg_database_size(datname)), 0)
		FROM pg_database
		WHERE NOT datistemplate`).Scan(&dbSize); err == nil {
		*points = append(*points, p.dp("postgresql.db_size", float64(dbSize), now, stTags))
	} else {
		p.moduleLogger.Warn().Err(err).Str("query", "db_size").Msg("postgresql: query failed")
	}

	// Table count (user tables in pg_class)
	var tableCount float64
	if err := p.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_class WHERE relkind = 'r'`).Scan(&tableCount); err == nil {
		*points = append(*points, p.dp("postgresql.table.count", float64(tableCount), now, stTags))
	}
}

// ── collectLocks ───────────────────────────────────────────────────────────

func (p *pgProbe) collectLocks(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	locksTags := p.tagsFor(dbcommon.MetricTypeLocks, instance)

	// Locks waiting (not granted)
	var waiting float64
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_locks WHERE NOT granted`).Scan(&waiting); err == nil {
		*points = append(*points, p.dp("senhub.db.postgresql.lock.waiting", float64(waiting), now, locksTags))
	}

	// Oldest active transaction age
	var longestXact sql.NullFloat64
	if err := p.db.QueryRowContext(ctx, `
		SELECT MAX(EXTRACT(EPOCH FROM (now() - xact_start)))
		FROM pg_stat_activity
		WHERE state IN ('active', 'idle in transaction')
		  AND xact_start IS NOT NULL`).Scan(&longestXact); err == nil && longestXact.Valid {
		*points = append(*points, p.dp("senhub.db.postgresql.long_running_xact", float64(longestXact.Float64), now, locksTags))
	}
}

// ── collectWAL ─────────────────────────────────────────────────────────────

func (p *pgProbe) collectWAL(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	archTags := p.tagsFor(dbcommon.MetricTypeArchiver, instance)

	// Archiver stats (only meaningful when archive_mode is on)
	var archiveMode string
	_ = p.db.QueryRowContext(ctx, `SHOW archive_mode`).Scan(&archiveMode)

	var failedCount float64
	var lastArchivedAge sql.NullFloat64

	err := p.db.QueryRowContext(ctx, `
		SELECT
			failed_count,
			CASE
				WHEN last_archived_time IS NOT NULL
					THEN EXTRACT(EPOCH FROM (now() - last_archived_time))
				ELSE NULL
			END
		FROM pg_stat_archiver`).Scan(&failedCount, &lastArchivedAge)
	if err == nil {
		*points = append(*points, p.dp("senhub.db.postgresql.archiver.failed", float64(failedCount), now, archTags))
		if archiveMode != "off" && archiveMode != "" && lastArchivedAge.Valid {
			*points = append(*points, p.dp("senhub.db.postgresql.archiver.last_archived.age", float64(lastArchivedAge.Float64), now, archTags))
		}
	}
}

// ── collectReplication ─────────────────────────────────────────────────────

func (p *pgProbe) collectReplication(ctx context.Context, now time.Time, instance string, points *[]data_store.DataPoint) {
	replTags := p.tagsFor(dbcommon.MetricTypeReplication, instance)

	// Detect role: pg_is_in_recovery() → replica; connected replicas → primary
	var isReplica bool
	_ = p.db.QueryRowContext(ctx, `SELECT pg_is_in_recovery()`).Scan(&isReplica)

	var role dbcommon.Role
	var replHealth float64 = 1

	if isReplica {
		role = dbcommon.RoleReplica

		// Replica WAL lag (replay)
		var replayLag sql.NullFloat64
		err := p.db.QueryRowContext(ctx, `
			SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))`).Scan(&replayLag)
		if err == nil && replayLag.Valid {
			lagTags := append(append([]tags.Tag{}, replTags...), tags.Tag{Key: "operation", Value: "replay"})
			*points = append(*points, p.dp("postgresql.wal.lag", float64(replayLag.Float64), now, lagTags))
			if replayLag.Float64 > 300 { // >5 min lag → degraded
				replHealth = 0
			}
		}

		// WAL receiver status
		var walRecvStatus sql.NullString
		_ = p.db.QueryRowContext(ctx, `SELECT status FROM pg_stat_wal_receiver LIMIT 1`).Scan(&walRecvStatus)
		ioRunning := float64(0)
		if walRecvStatus.Valid && walRecvStatus.String == "streaming" {
			ioRunning = 1
		} else {
			replHealth = 0
		}
		*points = append(*points, p.dp("senhub.db.postgresql.replica.io.running", ioRunning, now, replTags))

	} else {
		// Count connected replicas
		var replicaCount float64
		if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_replication`).Scan(&replicaCount); err == nil {
			if replicaCount > 0 {
				role = dbcommon.RolePrimary
			}
			*points = append(*points, p.dp("senhub.db.replication.replicas.connected", float64(replicaCount), now, replTags))
		}
	}

	// Role metric (numeric + tag for PRTG lookup)
	roleTags := append(append([]tags.Tag{}, replTags...), tags.Tag{Key: "role", Value: role.String()})
	*points = append(*points,
		p.dp("senhub.db.replication.role", role.RoleValue(), now, roleTags),
		p.dp("senhub.db.replication.health", replHealth, now, roleTags),
	)

	p.entitySrc.setRole(role)
}

// ── buildDSN ───────────────────────────────────────────────────────────────

// buildDSN builds a postgres:// URL DSN. url.URL percent-escapes every
// component, so a space or '=' in the password can no longer smuggle a
// second key into the space-separated keyword/value form the pgx parser
// reads left-to-right — there, a password of "x host=evil" would have
// redirected the connection (last host= wins) with the real credentials.
func (p *pgProbe) buildDSN() string {
	db := "postgres"
	if len(p.cfg.Databases) > 0 {
		db = p.cfg.Databases[0]
	}
	q := url.Values{}
	q.Set("sslmode", p.tlsMode())

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.cfg.Username, p.cfg.Password),
		Host:     net.JoinHostPort(p.cfg.Host, strconv.Itoa(p.cfg.Port)),
		Path:     "/" + db,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// tlsMode maps the operator TLS config to a libpq sslmode. With no explicit
// config we use "prefer": TLS is negotiated opportunistically when the
// server supports it and falls back to plaintext otherwise — strictly safer
// than "disable" without breaking the many self-hosted servers that have no
// certificate. An operator who sets tls gets verification ("verify-full"),
// or "require" when they opt into skipping verification.
func (p *pgProbe) tlsMode() string {
	if p.cfg.TLSConfig == nil {
		return "prefer"
	}
	if p.cfg.TLSConfig.InsecureSkipVerify {
		return "require"
	}
	return "verify-full"
}

// pgTLSConfig holds operator TLS overrides; nil means "no TLS".
type pgTLSConfig struct {
	InsecureSkipVerify bool
	CACert             string
}
