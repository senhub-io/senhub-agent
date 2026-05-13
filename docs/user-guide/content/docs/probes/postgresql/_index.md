---
title: "PostgreSQL"
weight: 16
---

# PostgreSQL

Monitors PostgreSQL instances — self-hosted, AWS RDS / Aurora,
Cloud SQL, Azure Flexible, Supabase. Collects health, connections,
throughput, replication, buffer cache, locks (with long-running
transaction tracking), storage with **bloat estimate**, **backup
freshness** via the WAL archiver, and aggregate `pg_stat_statements`
when the extension is installed.

**License**: Pro

## Prerequisites

- PostgreSQL 12+ (community), or the managed equivalent listed above
- Network access from the agent to the database (default port `5432`)
- A monitoring role with `pg_monitor` — see [GRANTs](#grants)

## Configuration

```yaml
probes:
  - name: production-postgres
    type: postgresql
    params:
      host: db.example.com
      port: 5432
      username: senhub_monitor
      password: ${env:PG_MONITOR_PASSWORD}
      database: postgres
      interval: 60
      timeout: 10
      sslmode: require
      max_replication_lag_seconds: 60
      bloat_top_n: 10
```

### Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `host` | Yes | - | Database hostname or IP |
| `port` | No | `5432` | TCP port |
| `username` | Yes | - | Monitoring role |
| `password` | Yes | - | Role's password (use `${env:VAR}` for secret) |
| `database` | No | `postgres` | Maintenance database to connect to |
| `interval` | No | `60` | Collection interval in seconds |
| `timeout` | No | `10` | Per-query timeout in seconds |
| `sslmode` | No | `prefer` | libpq sslmode (`disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`) |
| `sslrootcert` | No | `""` | CA certificate path (for `verify-ca` / `verify-full`) |
| `max_replication_lag_seconds` | No | `60` | Threshold for the composite `db_replication_health` channel |
| `bloat_top_n` | No | `10` | Top-N tables by heap size to track for bloat (hard cap 50) |
| `expose_per_database` | No | `false` | Emit per-database metrics |
| `expose_top_tables` | No | `0` | Emit per-table metrics; cardinality scales with N |

## GRANTs

```bash
senhub-agent db-monitoring init --engine postgresql --user senhub_monitor
```

The helper prints:

```sql
CREATE ROLE senhub_monitor LOGIN PASSWORD 'STRONG-PASSWORD-HERE';
GRANT pg_monitor TO senhub_monitor;
```

Available since PostgreSQL 10. For PG 9.x, per-view GRANTs are
required — out of scope for v1.

### Optional: `pg_stat_statements`

To unlock the aggregate query metrics (`db_postgres_stat_statements_*`)
enable the extension:

```sql
-- as superuser
ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';
-- restart the server, then:
CREATE EXTENSION pg_stat_statements;
```

The probe queries the right column projection automatically per
server version (PG 12, 13, 17 differ on column names).

## Collected Metrics

Every metric is tagged with `metric_type` so the PRTG Sensor
Builder splits them into family chips.

### Overview

| Metric | Unit | Description |
|---|---|---|
| `db_up` | bool | 1 = last ping reached the server |
| `db_uptime_seconds` | s | `now() - pg_postmaster_start_time()` |
| `db_version_info` | – | Always 1; version carried as label |
| `db_connections_utilization` | ratio | `count(pg_stat_activity) / max_connections` |
| `db_replication_role` | enum | 0=standalone, 1=primary, 2=replica |
| `db_replication_health` | bool | Composite: WAL receiver streaming AND lag below threshold |

### Connections

| Metric | Unit | Description |
|---|---|---|
| `db_connections_active` | count | `state='active'` |
| `db_connections_idle` | count | `state='idle'` |
| `db_connections_idle_in_transaction` | count | **Vacuum killer** — sessions holding open transactions while idle |
| `db_connections_max` | count | `max_connections` GUC |

### Throughput

| Metric | Unit | Description |
|---|---|---|
| `db_queries_count` | counter | Commits + rollbacks across non-system DBs |
| `db_transactions_committed` | counter | Sum of `pg_stat_database.xact_commit` |
| `db_transactions_rolled_back` | counter | Sum of `pg_stat_database.xact_rollback` |

### Replication

Emitted only when role is primary or replica.

| Metric | Unit | Description |
|---|---|---|
| `db_replication_lag_seconds` | s | `now() - pg_last_xact_replay_timestamp()` |
| `db_replication_io_running` | bool | 1 if `pg_stat_wal_receiver.status='streaming'` |
| `db_replication_replicas_connected` | count | `count(pg_stat_replication)` (primary side) |

### Cache

| Metric | Unit | Description |
|---|---|---|
| `db_buffer_hit_ratio` | ratio | `blks_hit / (blks_hit + blks_read)` across non-system DBs |

### Locks

| Metric | Unit | Description |
|---|---|---|
| `db_locks_deadlocks` | counter | Sum of `pg_stat_database.deadlocks` |
| `db_locks_waiting` | count | `pg_locks` where `granted=false` |
| `db_postgres_long_running_xact_seconds` | s | **Age of the oldest open transaction** — predicts vacuum starvation |

### Storage

| Metric | Unit | Description |
|---|---|---|
| `db_size_bytes` | bytes | Sum of `pg_database_size()` across non-system DBs |
| `db_tables_count` | count | `pg_stat_user_tables` row count |
| `db_postgres_bloat_ratio{schema,relation}` | ratio | Dead / (live+dead) tuples — top-N tables by heap size |
| `db_postgres_bloat_bytes{schema,relation}` | bytes | Wasted bytes per top-N table |

### Backups

| Metric | Unit | Description |
|---|---|---|
| `db_postgres_archiver_last_archived_age_seconds` | s | **DR canary** — staleness of WAL archiving |
| `db_postgres_archiver_failed_count` | counter | Cumulative archiver failures |

### Engine — `pg_stat_statements` (opt-in via extension)

| Metric | Unit | Description |
|---|---|---|
| `db_postgres_stat_statements_calls_count` | counter | Aggregate `sum(calls)` |
| `db_postgres_stat_statements_exec_time_mean_ms` | ms | Aggregate mean exec time across all statements |

## Output formats

- **PRTG / Sensor Builder** — pick chips per family.
- **Nagios** — filter by family: `?tags=metric_type:replication`.
- **Prometheus** — `/api/{key}/prometheus/metrics`; metric names
  start with `senhub_system_db_*` and `senhub_db_postgres_*`.
- **OTLP** — pushed as `system.db.*` and `senhub.db.postgres.*`.

## Auto-detected environments

`environment` tag set from `version()`:
`self_hosted`, `rds`, `aurora`, `cloudsql`, `azure_flexible`,
`supabase`. Detection is best-effort; a wrong guess never breaks
the probe.

## Cloud-managed instances

Tested against:

- AWS RDS for PostgreSQL, Aurora PostgreSQL
- GCP Cloud SQL for PostgreSQL — Insights wraps `pg_stat_statements`
- Azure Database for PostgreSQL Flexible Server
- Supabase

Filesystem-level metrics (WAL filesystem usage) are not available
on managed instances; the WAL position and archiver age suffice.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `db_up = 0` | Network / firewall | Verify connectivity from agent to `host:port` |
| `db_postgres_stat_statements_*` missing | Extension not installed | See the [`pg_stat_statements`](#optional-pg_stat_statements) section |
| `db_postgres_archiver_*` missing | WAL archiving not configured | Expected — only emit when `archive_mode=on` and an archiver is running |
| All `db_*` missing | Role lacks `pg_monitor` | Re-run the `db-monitoring init` helper |
| `db_postgres_bloat_*` missing | Role cannot read `pg_stat_user_tables` | Same — needs `pg_monitor` |
