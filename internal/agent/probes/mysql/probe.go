// Package mysql implements the free mysql probe: MySQL / MariaDB server
// monitoring via database/sql + the go-sql-driver/mysql driver.
//
// Metric coverage mirrors the otelcol-contrib mysqlreceiver where names
// match (mysql.uptime, mysql.threads, mysql.commands, mysql.query.count,
// mysql.replica.time_behind_source, mysql.buffer_pool.data_pages,
// mysql.connection.errors) and is extended under senhub.db.* / senhub.db.mysql.*
// for what contrib lacks (idle connections, utilisation ratio, deadlocks,
// IO bytes, size, table count, tmp-table spill ratio, etc.).
//
// SHOW GLOBAL STATUS drives the bulk of the collection; SHOW REPLICA STATUS
// gates the replication family. Information-schema queries back the storage
// family (total size, table count, per-database/per-table sizes when opt-in).
//
// Configuration:
//
//	type: mysql
//	name: my-mysql
//	params:
//	  host:     127.0.0.1
//	  port:     3306
//	  username: senhub_monitor
//	  password: ${env:MYSQL_MONITOR_PASSWORD}
//	  tls:      false
//	  interval: 60
//	  per_database: false   # opt-in size per database
//	  per_table:    false   # opt-in size per table (requires per_database)
//	  top_n_tables: 20      # max tables per database when per_table=true
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"

	gomysql "github.com/go-sql-driver/mysql"
)

// ProbeType is the stable type name used in probe YAML (type: mysql).
const ProbeType = "mysql"

const (
	defaultPort     = 3306
	defaultInterval = 60 * time.Second
	defaultTimeout  = 10 * time.Second

	// serverUUIDQuery fetches the MySQL server's persistent unique id.
	// @@server_uuid is set once at server initialisation and survives
	// restarts, making it a stable identity source for the db entity.
	serverUUIDQuery = "SELECT @@server_uuid"

	// statusQuery pulls every variable from GLOBAL STATUS in one round-trip.
	statusQuery = "SHOW GLOBAL STATUS"

	// variablesQuery reads server variables needed (max_connections).
	variablesQuery = "SHOW GLOBAL VARIABLES WHERE Variable_name IN " +
		"('max_connections', 'version', 'version_comment')"

	// replicaStatusQuery checks replica / slave status. We try the modern
	// form first (SHOW REPLICA STATUS) and fall back to SHOW SLAVE STATUS
	// for MySQL < 8.0.22 / MariaDB.
	replicaStatusQuery    = "SHOW REPLICA STATUS"
	replicaStatusFallback = "SHOW SLAVE STATUS"
	replicaCountQuery     = "SHOW REPLICAS"
	replicaCountFallback  = "SHOW SLAVE HOSTS"
	totalSizeQuery        = "SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.TABLES WHERE table_schema NOT IN ('information_schema','performance_schema','mysql','sys')"
	tableCountQuery       = "SELECT COUNT(*) FROM information_schema.TABLES WHERE table_schema NOT IN ('information_schema','performance_schema','mysql','sys')"
	perDatabaseSizeQuery  = "SELECT table_schema, COALESCE(SUM(data_length+index_length),0) FROM information_schema.TABLES WHERE table_schema NOT IN ('information_schema','performance_schema','mysql','sys') GROUP BY table_schema"
	perTableSizeQuery     = "SELECT table_schema, table_name, COALESCE(data_length+index_length,0) FROM information_schema.TABLES WHERE table_schema NOT IN ('information_schema','performance_schema','mysql','sys')"
)

// config holds the parsed probe configuration.
type config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	TLS         bool
	Interval    time.Duration
	PerDatabase bool
	PerTable    bool
	TopNTables  int
	// InstanceName is an optional operator-supplied stable identifier for the
	// db entity (db.instance.id). When set it takes precedence over the
	// MySQL-reported @@server_uuid so that operators can assign a meaningful,
	// human-readable name to the node in Toise. Leave empty to use the
	// tech-reported stable id (MySQL persists @@server_uuid across restarts).
	InstanceName string
}

// mysqlProbe implements types.Probe for MySQL / MariaDB monitoring.
type mysqlProbe struct {
	*types.BaseProbe
	cfg          config
	db           *sql.DB
	moduleLogger *logger.ModuleLogger
	entitySrc    *mysqlEntitySource
	unregister   func()
	// uuidFetched guards the one-time @@server_uuid fetch; once pinned
	// the entity source holds the id and this flag prevents re-querying.
	uuidFetched bool
}

// NewMysqlProbe constructs the probe. Configuration errors surface here.
func NewMysqlProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.mysql")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &mysqlProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
	}
	p.SetProbeType(ProbeType)
	p.entitySrc = newMysqlEntitySource(cfg, moduleLogger)
	p.SetEntitySource(p.entitySrc)
	return p, nil
}

func parseConfig(raw map[string]interface{}) (config, error) {
	cfg := config{
		Port:       defaultPort,
		Interval:   defaultInterval,
		TopNTables: 20,
	}

	if v, ok := raw["host"].(string); ok && v != "" {
		cfg.Host = v
	} else {
		cfg.Host = "127.0.0.1"
	}
	if v, ok := raw["port"].(int); ok && v > 0 {
		cfg.Port = v
	}
	if v, ok := raw["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := raw["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := raw["tls"].(bool); ok {
		cfg.TLS = v
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["per_database"].(bool); ok {
		cfg.PerDatabase = v
	}
	if v, ok := raw["per_table"].(bool); ok {
		cfg.PerTable = v
	}
	if v, ok := raw["top_n_tables"].(int); ok && v > 0 {
		cfg.TopNTables = v
	}
	if v, ok := raw["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	return cfg, nil
}

func (p *mysqlProbe) ShouldStart() bool          { return true }
func (p *mysqlProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens the connection pool but does NOT verify reachability:
// sql.Open is lazy (no network I/O) and only fails on a malformed DSN, so
// a target that is down at start does not abort the probe. The first real
// connection is made in Collect, which emits senhub.db.up=0 while the
// server is unreachable and reconnects automatically once it returns —
// the pool re-dials on demand. This mirrors the oracle/mssql probes (#485).
func (p *mysqlProbe) OnStart(_ chan struct{}) error {
	dsn, err := p.buildDSN()
	if err != nil {
		return fmt.Errorf("mysql probe %s: building DSN: %w", p.GetName(), err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql probe %s: opening connection: %w", p.GetName(), err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	p.db = db

	p.unregister = entity.RegisterSource(p.entitySrc)
	p.moduleLogger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Msg("mysql probe started")
	return nil
}

// OnShutdown closes the database connection.
func (p *mysqlProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// Collect gathers one cycle of metrics. The probe always emits
// senhub.db.up so that monitoring systems see a 0 when the engine is
// unreachable, rather than a missing series.
func (p *mysqlProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	instance := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	commonTags := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "db.system.name", Value: "mysql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}

	// Always emit up=0 first; overwritten to 1 if queries succeed.
	up := float64(0)
	var points []data_store.DataPoint

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Ping the server. The connection is established here (not in OnStart)
	// so a down-at-start target is a recoverable outage, not a fatal probe
	// failure; the pool reconnects on demand once the server returns (#485).
	if p.db == nil {
		p.moduleLogger.Warn().Str("instance", instance).Msg("mysql: no connection pool")
		points = append(points, p.dp("senhub.db.up", 0, now, string(dbcommon.MetricTypeOverview), commonTags))
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	if err := p.db.PingContext(ctx); err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", instance).Msg("mysql ping failed")
		points = append(points, p.dp("senhub.db.up", 0, now, string(dbcommon.MetricTypeOverview), commonTags))
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}
	up = 1

	// Lazy one-time fetch of the server UUID for the db entity identity.
	// Skipped when the operator has set instance_name (already pinned) or
	// when the uuid has been fetched before. On failure, pinServerUUID("")
	// falls through to the host:port degraded fallback.
	if !p.uuidFetched && !p.entitySrc.isIDPinned() {
		uuid := p.queryServerUUID(ctx)
		p.entitySrc.pinServerUUID(uuid)
		p.uuidFetched = true
	}

	// SHOW GLOBAL STATUS
	status, err := p.collectStatus(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", instance).Msg("mysql SHOW GLOBAL STATUS failed")
		points = append(points, p.dp("senhub.db.up", up, now, string(dbcommon.MetricTypeOverview), commonTags))
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	// SHOW GLOBAL VARIABLES
	vars, err := p.collectVariables(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", instance).Msg("mysql SHOW GLOBAL VARIABLES failed")
	}

	// Merge tags that were filled during the call.
	versionComment := vars["version_comment"]
	if versionComment == "" {
		versionComment = vars["version"]
	}
	env := dbcommon.DetectEnvironment(versionComment)
	versionVal := vars["version"]
	p.entitySrc.setVersion(versionVal)
	p.entitySrc.setEnvironment(string(env))

	allCommonTags := append([]tags.Tag{}, commonTags...)
	allCommonTags = append(allCommonTags,
		tags.Tag{Key: "environment", Value: string(env)},
	)

	points = append(points,
		p.dp("senhub.db.up", float64(up), now, string(dbcommon.MetricTypeOverview), allCommonTags),
	)
	if versionVal != "" {
		versionTags := append([]tags.Tag{}, allCommonTags...)
		versionTags = append(versionTags, tags.Tag{Key: "version", Value: versionVal})
		points = append(points, p.dp("senhub.db.version.info", 1, now, string(dbcommon.MetricTypeOverview), versionTags))
	}

	// Uptime
	points = append(points, p.dp("mysql.uptime", asFloat(status["Uptime"]), now, string(dbcommon.MetricTypeOverview), allCommonTags))

	// ─── Connections ─────────────────────────────────────────────────────────
	connected := asFloat(status["Threads_connected"])
	running := asFloat(status["Threads_running"])
	maxConns := asFloat(vars["max_connections"])

	connTags := append([]tags.Tag{{Key: "kind", Value: "connected"}}, allCommonTags...)
	runTags := append([]tags.Tag{{Key: "kind", Value: "running"}}, allCommonTags...)
	points = append(points,
		p.dp("mysql.threads.connected", connected, now, string(dbcommon.MetricTypeConnections), connTags),
		p.dp("mysql.threads.running", running, now, string(dbcommon.MetricTypeConnections), runTags),
	)
	idle := connected - running
	if idle < 0 {
		idle = 0
	}
	points = append(points,
		p.dp("senhub.db.connection.idle", idle, now, string(dbcommon.MetricTypeConnections), allCommonTags),
	)
	if maxConns > 0 {
		points = append(points,
			p.dp("senhub.db.mysql.connection.max", maxConns, now, string(dbcommon.MetricTypeConnections), allCommonTags),
			p.dp("senhub.db.connection.utilization", connected/maxConns*100, now, string(dbcommon.MetricTypeConnections), allCommonTags),
		)
	}

	// Connection errors
	for _, errKey := range []struct{ statusKey, attr string }{
		{"Aborted_clients", "aborted_clients"},
		{"Aborted_connects", "aborted_connects"},
		{"Connection_errors_max_connections", "max_connections"},
	} {
		errTags := append([]tags.Tag{{Key: "error", Value: errKey.attr}}, allCommonTags...)
		points = append(points, p.dp("mysql.connection.errors."+errKey.attr, asFloat(status[errKey.statusKey]), now, string(dbcommon.MetricTypeConnections), errTags))
	}

	// ─── Throughput ──────────────────────────────────────────────────────────
	points = append(points,
		p.dp("mysql.query.count", asFloat(status["Questions"]), now, string(dbcommon.MetricTypeThroughput), allCommonTags),
		p.dp("mysql.query.slow.count", asFloat(status["Slow_queries"]), now, string(dbcommon.MetricTypeThroughput), allCommonTags),
	)

	// Commands (multi-instance by command tag).
	cmdMap := map[string]string{
		"select":  "Com_select",
		"insert":  "Com_insert",
		"update":  "Com_update",
		"delete":  "Com_delete",
		"replace": "Com_replace",
	}
	for verb, key := range cmdMap {
		cmdTags := append([]tags.Tag{{Key: "command", Value: verb}}, allCommonTags...)
		points = append(points, p.dp("mysql.commands", asFloat(status[key]), now, string(dbcommon.MetricTypeThroughput), cmdTags))
	}

	// Transactions (Com_commit, Com_rollback).
	committedTags := append([]tags.Tag{{Key: "state", Value: "committed"}}, allCommonTags...)
	rolledBackTags := append([]tags.Tag{{Key: "state", Value: "rolled_back"}}, allCommonTags...)
	points = append(points,
		p.dp("senhub.db.mysql.transaction.count.committed", asFloat(status["Com_commit"]), now, string(dbcommon.MetricTypeThroughput), committedTags),
		p.dp("senhub.db.mysql.transaction.count.rolled_back", asFloat(status["Com_rollback"]), now, string(dbcommon.MetricTypeThroughput), rolledBackTags),
	)

	// Tmp tables.
	tmpTotal := asFloat(status["Created_tmp_tables"])
	tmpDisk := asFloat(status["Created_tmp_disk_tables"])
	if tmpTotal > 0 {
		points = append(points, p.dp("senhub.db.mysql.tmp_tables.disk_ratio", tmpDisk/tmpTotal*100, now, string(dbcommon.MetricTypeThroughput), allCommonTags))
	}

	// ─── Cache (InnoDB buffer pool) ───────────────────────────────────────────
	poolReads := asFloat(status["Innodb_buffer_pool_reads"])
	poolReadReqs := asFloat(status["Innodb_buffer_pool_read_requests"])
	if poolReadReqs > 0 {
		hitRatio := (float64(1) - poolReads/poolReadReqs) * 100
		points = append(points, p.dp("senhub.db.mysql.buffer_pool.hit_ratio", hitRatio, now, string(dbcommon.MetricTypeCache), allCommonTags))
	}
	poolPagesTotal := asFloat(status["Innodb_buffer_pool_pages_total"])
	poolPagesData := asFloat(status["Innodb_buffer_pool_pages_data"])
	if poolPagesTotal > 0 {
		points = append(points, p.dp("senhub.db.mysql.buffer_pool.utilization", poolPagesData/poolPagesTotal*100, now, string(dbcommon.MetricTypeCache), allCommonTags))
	}
	dirtyTags := append([]tags.Tag{{Key: "status", Value: "dirty"}}, allCommonTags...)
	points = append(points,
		p.dp("mysql.buffer_pool.data_pages.dirty", asFloat(status["Innodb_buffer_pool_pages_dirty"]), now, string(dbcommon.MetricTypeCache), dirtyTags),
	)

	// ─── Locks ────────────────────────────────────────────────────────────────
	points = append(points,
		p.dp("senhub.db.mysql.lock.deadlocks", asFloat(status["Innodb_deadlocks"]), now, string(dbcommon.MetricTypeLocks), allCommonTags),
		p.dp("senhub.db.mysql.lock.waiting", asFloat(status["Innodb_row_lock_current_waits"]), now, string(dbcommon.MetricTypeLocks), allCommonTags),
		p.dp("senhub.db.mysql.row_lock.time.avg", asFloat(status["Innodb_row_lock_time_avg"]), now, string(dbcommon.MetricTypeLocks), allCommonTags),
	)

	// ─── IO ───────────────────────────────────────────────────────────────────
	ioReadTags := append([]tags.Tag{{Key: "io.direction", Value: "read"}}, allCommonTags...)
	ioWriteTags := append([]tags.Tag{{Key: "io.direction", Value: "write"}}, allCommonTags...)
	points = append(points,
		p.dp("senhub.db.mysql.io.read", asFloat(status["Innodb_data_read"]), now, string(dbcommon.MetricTypeIO), ioReadTags),
		p.dp("senhub.db.mysql.io.write", asFloat(status["Innodb_data_written"]), now, string(dbcommon.MetricTypeIO), ioWriteTags),
	)

	// ─── Storage ──────────────────────────────────────────────────────────────
	storageCtx, storageCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer storageCancel()

	if totalSize, err := p.querySingleFloat(storageCtx, totalSizeQuery); err == nil {
		points = append(points, p.dp("senhub.db.database.size", totalSize, now, string(dbcommon.MetricTypeStorage), allCommonTags))
	}
	if tableCount, err := p.querySingleFloat(storageCtx, tableCountQuery); err == nil {
		points = append(points, p.dp("senhub.db.mysql.table.count", tableCount, now, string(dbcommon.MetricTypeStorage), allCommonTags))
	}

	// Per-database sizes (opt-in).
	if p.cfg.PerDatabase {
		dbSizes, err := p.queryPerDatabaseSizes(storageCtx)
		if err == nil {
			for dbName, sz := range dbSizes {
				dbTags := append([]tags.Tag{{Key: "database", Value: dbName}}, allCommonTags...)
				points = append(points, p.dp("senhub.db.database.size.per_database", sz, now, string(dbcommon.MetricTypePerDatabase), dbTags))
			}
			// Per-table sizes (opt-in; requires per_database).
			if p.cfg.PerTable {
				tableSizes, err := p.queryPerTableSizes(storageCtx)
				if err == nil {
					for _, ts := range p.topNTables(tableSizes) {
						tbTags := append([]tags.Tag{
							{Key: "database", Value: ts.db},
							{Key: "table", Value: ts.table},
						}, allCommonTags...)
						points = append(points, p.dp("senhub.db.mysql.table.size", ts.size, now, string(dbcommon.MetricTypePerTable), tbTags))
					}
				}
			}
		}
	}

	// ─── Replication ──────────────────────────────────────────────────────────
	replCtx, replCancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer replCancel()

	role, replicaRows, replicaStatus := p.collectReplication(replCtx)
	p.entitySrc.updateRole(role)

	roleTags := append([]tags.Tag{{Key: "role", Value: role.String()}}, allCommonTags...)
	points = append(points,
		p.dp("senhub.db.replication.role", role.RoleValue(), now, string(dbcommon.MetricTypeReplication), roleTags),
	)

	health := float64(1) // standalone = always healthy
	if role == dbcommon.RolePrimary {
		points = append(points, p.dp("senhub.db.replication.replicas.connected", float64(replicaRows), now, string(dbcommon.MetricTypeReplication), allCommonTags))
		if replicaRows == 0 {
			health = 0
		}
	} else if role == dbcommon.RoleReplica {
		ioRunning := replicaStatus["Slave_IO_Running"]
		sqlRunning := replicaStatus["Slave_SQL_Running"]
		lag := asFloat(replicaStatus["Seconds_Behind_Master"])

		ioOK := float64(0)
		if strings.EqualFold(ioRunning, "yes") {
			ioOK = 1
		}
		sqlOK := float64(0)
		if strings.EqualFold(sqlRunning, "yes") {
			sqlOK = 1
		}
		if ioOK == 0 || sqlOK == 0 {
			health = 0
		}
		points = append(points,
			p.dp("senhub.db.mysql.replica.io_thread.running", ioOK, now, string(dbcommon.MetricTypeReplication), allCommonTags),
			p.dp("senhub.db.mysql.replica.sql_thread.running", sqlOK, now, string(dbcommon.MetricTypeReplication), allCommonTags),
			p.dp("mysql.replica.time_behind_source", lag, now, string(dbcommon.MetricTypeReplication), allCommonTags),
		)
	}
	points = append(points, p.dp("senhub.db.replication.health", health, now, string(dbcommon.MetricTypeReplication), allCommonTags))

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// dp creates a DataPoint with the given metric name, value, and tags,
// plus a metric_type tag so the PRTG sensor builder can chip it.
func (p *mysqlProbe) dp(name string, value float64, ts time.Time, metricType string, baseTags []tags.Tag) data_store.DataPoint {
	t := make([]tags.Tag, 0, len(baseTags)+1)
	t = append(t, tags.Tag{Key: "metric_type", Value: metricType})
	t = append(t, baseTags...)
	return data_store.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: t}
}

// collectStatus runs SHOW GLOBAL STATUS and returns a key→value map.
func (p *mysqlProbe) collectStatus(ctx context.Context) (map[string]string, error) {
	return p.queryKeyValue(ctx, statusQuery)
}

// collectVariables runs SHOW GLOBAL VARIABLES and returns a key→value map.
func (p *mysqlProbe) collectVariables(ctx context.Context) (map[string]string, error) {
	return p.queryKeyValue(ctx, variablesQuery)
}

// queryKeyValue executes a query that returns (Variable_name, Value) rows
// and returns the result as a map.
func (p *mysqlProbe) queryKeyValue(ctx context.Context, query string) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", query, err)
	}
	defer rows.Close()

	m := make(map[string]string, 256)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scanning %s: %w", query, err)
		}
		m[k] = v
	}
	return m, rows.Err()
}

// querySingleFloat executes a query that returns a single float64 in the
// first column of the first row.
func (p *mysqlProbe) querySingleFloat(ctx context.Context, query string) (float64, error) {
	var v float64
	if err := p.db.QueryRowContext(ctx, query).Scan(&v); err != nil {
		return 0, fmt.Errorf("%s: %w", query, err)
	}
	return float64(v), nil
}

// collectReplication returns the detected replication role, the number of
// connected replicas (for primary), and the replica status row (for replica).
func (p *mysqlProbe) collectReplication(ctx context.Context) (dbcommon.Role, int, map[string]string) {
	// Try SHOW REPLICA STATUS first (MySQL 8.0.22+), then SHOW SLAVE STATUS.
	replicaStatus, err := p.queryReplicaStatus(ctx)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Msg("mysql replication status query failed")
	}

	if len(replicaStatus) > 0 {
		// This server is a replica.
		return dbcommon.RoleReplica, 0, replicaStatus
	}

	// Check if this server is a primary (has downstream replicas).
	n := p.countReplicas(ctx)
	if n > 0 {
		return dbcommon.RolePrimary, n, nil
	}

	return dbcommon.RoleStandalone, 0, nil
}

// queryReplicaStatus tries SHOW REPLICA STATUS then SHOW SLAVE STATUS.
// Returns nil (not an error) when the server is not a replica.
func (p *mysqlProbe) queryReplicaStatus(ctx context.Context) (map[string]string, error) {
	for _, q := range []string{replicaStatusQuery, replicaStatusFallback} {
		rows, err := p.db.QueryContext(ctx, q)
		if err != nil {
			continue
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil || !rows.Next() {
			return nil, nil // no replica configured
		}

		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanning replica status: %w", err)
		}

		m := make(map[string]string, len(cols))
		for i, col := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[col] = string(v)
			case string:
				m[col] = v
			case int64:
				m[col] = strconv.FormatInt(v, 10)
			case nil:
				m[col] = ""
			default:
				m[col] = fmt.Sprintf("%v", v)
			}
		}
		return m, nil
	}
	return nil, nil
}

// countReplicas tries SHOW REPLICAS then SHOW SLAVE HOSTS, returning the
// number of rows (connected downstream replicas).
func (p *mysqlProbe) countReplicas(ctx context.Context) int {
	for _, q := range []string{replicaCountQuery, replicaCountFallback} {
		rows, err := p.db.QueryContext(ctx, q)
		if err != nil {
			continue
		}
		n := 0
		for rows.Next() {
			n++
		}
		rows.Close()
		return n
	}
	return 0
}

type tableSize struct {
	db    string
	table string
	size  float64
}

// queryPerDatabaseSizes returns a map of database name → total bytes.
func (p *mysqlProbe) queryPerDatabaseSizes(ctx context.Context) (map[string]float64, error) {
	rows, err := p.db.QueryContext(ctx, perDatabaseSizeQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]float64)
	for rows.Next() {
		var dbName string
		var sz float64
		if err := rows.Scan(&dbName, &sz); err != nil {
			return nil, err
		}
		m[dbName] = float64(sz)
	}
	return m, rows.Err()
}

// queryPerTableSizes returns all (db, table, bytes) rows from information_schema.
func (p *mysqlProbe) queryPerTableSizes(ctx context.Context) ([]tableSize, error) {
	rows, err := p.db.QueryContext(ctx, perTableSizeQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tableSize
	for rows.Next() {
		var dbName, tableName string
		var sz float64
		if err := rows.Scan(&dbName, &tableName, &sz); err != nil {
			return nil, err
		}
		out = append(out, tableSize{db: dbName, table: tableName, size: float64(sz)})
	}
	return out, rows.Err()
}

// topNTables returns the top-N largest tables (bounded cardinality).
func (p *mysqlProbe) topNTables(ts []tableSize) []tableSize {
	if p.cfg.TopNTables <= 0 || len(ts) <= p.cfg.TopNTables {
		return ts
	}
	sizes := make([]int64, len(ts))
	for i, t := range ts {
		sizes[i] = int64(t.size)
	}
	idx := dbcommon.TopNBySize(sizes, p.cfg.TopNTables)
	out := make([]tableSize, len(idx))
	for i, j := range idx {
		out[i] = ts[j]
	}
	return out
}

// queryVersionComment fetches @@version_comment for environment detection.
func (p *mysqlProbe) queryVersionComment(ctx context.Context) string {
	var v string
	_ = p.db.QueryRowContext(ctx, "SELECT @@version_comment").Scan(&v)
	return v
}

// queryServerUUID fetches @@server_uuid, the MySQL-persisted stable server
// identity. Returns "" on error (caller falls back to host:port).
func (p *mysqlProbe) queryServerUUID(ctx context.Context) string {
	var v string
	_ = p.db.QueryRowContext(ctx, serverUUIDQuery).Scan(&v)
	return v
}

// buildDSN returns the DSN for the go-sql-driver/mysql driver.
// password is kept in the DSN which the driver does not log by default.
func (p *mysqlProbe) buildDSN() (string, error) {
	mc := gomysql.NewConfig()
	mc.User = p.cfg.Username
	mc.Passwd = p.cfg.Password
	mc.Net = "tcp"
	mc.Addr = fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	mc.Timeout = defaultTimeout
	mc.ReadTimeout = defaultTimeout
	mc.WriteTimeout = defaultTimeout
	mc.ParseTime = false

	if p.cfg.TLS {
		mc.TLSConfig = "true"
	} else {
		mc.TLSConfig = "false"
	}
	return mc.FormatDSN(), nil
}

// asFloat parses a string value from SHOW GLOBAL STATUS into a float64.
// Returns 0 for empty / non-numeric strings.
func asFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0
	}
	return float64(v)
}
