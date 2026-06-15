package mssql

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// perfCounter is one row of sys.dm_os_performance_counters we care about.
// counter_name + (optional) instance_name select the value; cntr_type tells us
// whether the raw cntr_value is a per-second counter or an instantaneous gauge.
type perfCounter struct {
	counter  string
	instance string
	value    float64
}

// performanceCountersQuery pulls only the rows the probe maps. Filtering in SQL
// keeps the result set tiny (a full scan of the DMV returns thousands of rows).
const performanceCountersQuery = `
SELECT RTRIM(counter_name), RTRIM(instance_name), cntr_value
FROM sys.dm_os_performance_counters
WHERE counter_name IN (
    'Batch Requests/sec',
    'Buffer cache hit ratio',
    'Buffer cache hit ratio base',
    'Page life expectancy',
    'Lock Waits/sec',
    'Processes blocked',
    'User Connections',
    'Transactions/sec'
)
AND (instance_name = '' OR instance_name = '_Total')`

// collectPerformanceCounters maps the perf-counter DMV onto the
// sqlserverreceiver-parity scalar metrics.
func (p *MSSQLProbe) collectPerformanceCounters(ctx context.Context, now time.Time) []data_store.DataPoint {
	rows, err := p.db.QueryContext(ctx, performanceCountersQuery)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("mssql performance-counter query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	counters := map[string]float64{}
	for rows.Next() {
		var c perfCounter
		if err := rows.Scan(&c.counter, &c.instance, &c.value); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("mssql performance-counter scan failed")
			continue
		}
		counters[c.counter] = c.value
	}
	if err := rows.Err(); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("mssql performance-counter rows error")
	}

	var points []data_store.DataPoint
	emit := func(name string, value float64, metricType string) {
		points = append(points, data_store.DataPoint{
			Name: name, Value: float64(value), Timestamp: now, Tags: p.baseTags(metricType),
		})
	}

	if v, ok := counters["Batch Requests/sec"]; ok {
		emit("sqlserver.batch_request.rate", v, metricTypeThroughput)
	}
	if v, ok := counters["Transactions/sec"]; ok {
		emit("sqlserver.transaction_rate", v, metricTypeThroughput)
	}
	if v, ok := counters["Lock Waits/sec"]; ok {
		emit("sqlserver.lock_wait_rate", v, metricTypeLocks)
	}
	if v, ok := counters["Processes blocked"]; ok {
		emit("sqlserver.processes.blocked", v, metricTypeLocks)
	}
	if v, ok := counters["User Connections"]; ok {
		emit("sqlserver.user.connection.count", v, metricTypeConnections)
	}
	if v, ok := counters["Page life expectancy"]; ok {
		emit("sqlserver.page_life_expectancy", v, metricTypeCache)
	}

	// Buffer cache hit ratio is a ratio of two counters: SQL Server exposes the
	// numerator and its base separately, and the wire value is a percentage of
	// hit/base. Guard the divide; a zero base means no reads yet this lifetime.
	hit, hitOK := counters["Buffer cache hit ratio"]
	base, baseOK := counters["Buffer cache hit ratio base"]
	if hitOK && baseOK && base > 0 {
		emit("sqlserver.page_buffer_cache.hit_ratio", hit/base*100, metricTypeCache)
	}

	return points
}

// databaseIORow is one row of the per-database I/O aggregation.
type databaseIORow struct {
	database string
	read     float64
	write    float64
}

// databaseIOQuery aggregates sys.dm_io_virtual_file_stats (bytes read/written)
// per database. The DMV is keyed by database_id + file_id; we sum across files
// and resolve the name via sys.databases.
const databaseIOQuery = `
SELECT DB_NAME(vfs.database_id) AS database_name,
       SUM(vfs.num_of_bytes_read)    AS bytes_read,
       SUM(vfs.num_of_bytes_written) AS bytes_written
FROM sys.dm_io_virtual_file_stats(NULL, NULL) AS vfs
WHERE DB_NAME(vfs.database_id) IS NOT NULL
GROUP BY vfs.database_id`

// collectDatabaseIO emits sqlserver.database.io as a multi_instance metric
// discriminated by database + direction (read|write).
func (p *MSSQLProbe) collectDatabaseIO(ctx context.Context, now time.Time) []data_store.DataPoint {
	rows, err := p.db.QueryContext(ctx, databaseIOQuery)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("mssql database-io query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var points []data_store.DataPoint
	for rows.Next() {
		var r databaseIORow
		var name sql.NullString
		if err := rows.Scan(&name, &r.read, &r.write); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("mssql database-io scan failed")
			continue
		}
		if !name.Valid || name.String == "" {
			continue
		}
		r.database = name.String

		points = append(points,
			data_store.DataPoint{
				Name: "sqlserver.database.io", Value: float64(r.read), Timestamp: now,
				Tags: p.taggedWith(metricTypeIO,
					tags.Tag{Key: "database", Value: r.database},
					tags.Tag{Key: "direction", Value: "read"}),
			},
			data_store.DataPoint{
				Name: "sqlserver.database.io", Value: float64(r.write), Timestamp: now,
				Tags: p.taggedWith(metricTypeIO,
					tags.Tag{Key: "database", Value: r.database},
					tags.Tag{Key: "direction", Value: "write"}),
			},
		)
	}
	if err := rows.Err(); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("mssql database-io rows error")
	}
	return points
}

// databaseStatusQuery reads every database's online state. state is the
// canonical sys.databases.state code (0=ONLINE, 1=RESTORING, 2=RECOVERING,
// 3=RECOVERY_PENDING, 4=SUSPECT, 5=EMERGENCY, 6=OFFLINE, 7=COPYING).
const databaseStatusQuery = `SELECT name, state FROM sys.databases`

// collectDatabaseStatus emits sqlserver.database.status as a multi_instance
// gauge per database (the numeric sys.databases.state code).
func (p *MSSQLProbe) collectDatabaseStatus(ctx context.Context, now time.Time) []data_store.DataPoint {
	rows, err := p.db.QueryContext(ctx, databaseStatusQuery)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("mssql database-status query failed")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var points []data_store.DataPoint
	for rows.Next() {
		var name string
		var state int
		if err := rows.Scan(&name, &state); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("mssql database-status scan failed")
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		points = append(points, data_store.DataPoint{
			Name: "sqlserver.database.status", Value: float64(state), Timestamp: now,
			Tags: p.taggedWith(metricTypeStorage, tags.Tag{Key: "database", Value: name}),
		})
	}
	if err := rows.Err(); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("mssql database-status rows error")
	}
	return points
}
