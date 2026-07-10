<img src="https://cdn.simpleicons.org/postgresql" alt="" class="probe-page-logo probe-page-logo-si">

# PostgreSQL

Monitors PostgreSQL instances — self-hosted, AWS RDS / Aurora,
Cloud SQL, Azure Flexible, Supabase. Collects health, connections,
throughput, replication, buffer cache, locks (with long-running
transaction tracking), storage with **bloat estimate**, **backup
freshness** via the WAL archiver, and aggregate `pg_stat_statements`
when the extension is installed.

**License**: Free

## Prerequisites

- PostgreSQL 12+ (community), or the managed equivalent listed above
- Network access from the agent to the database (default port `5432`)
- A monitoring role with `pg_monitor` — see [GRANTs](#grants)

## Configuration

```yaml
# probes.d/20-postgresql.yaml — each file under probes.d/ is a YAML array of probes
- name: production-postgres
  type: postgresql
  params:
    host: db.example.com
    port: 5432
    username: senhub_monitor
    password: ${secret:production-postgres.password}   # OS secret store; inline plaintext is auto-sealed on install
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
| `password` | Yes | - | Role's password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `database` | No | `postgres` | Maintenance database to connect to |
| `interval` | No | `60` | Collection interval in seconds |
| `timeout` | No | `10` | Per-query timeout in seconds |
| `sslmode` | No | `prefer` | libpq sslmode (`disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`) |
| `sslrootcert` | No | `""` | CA certificate path (for `verify-ca` / `verify-full`) |
| `max_replication_lag_seconds` | No | `60` | Threshold for the composite `senhub.db.replication.health` channel |
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

To unlock the aggregate query metrics (`senhub.db.postgres.stat_statements.*`)
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

Every datapoint also carries the OTel resource attributes
`db.system.name` (`postgresql`), `server.address`, and `server.port`.

### Overview

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | bool | 1 = last ping reached the server |
| `senhub.db.uptime.seconds` | s | `now() - pg_postmaster_start_time()` |
| `senhub.db.version.info` | – | Always 1; version carried as label |
| `senhub.db.connections.utilization` | ratio | `count(pg_stat_activity) / max_connections` |
| `senhub.db.replication.role` | enum | 0=standalone, 1=primary, 2=replica |
| `senhub.db.replication.health` | bool | Composite: WAL receiver streaming AND lag below threshold |

### Connections

| Metric | Unit | Description |
|---|---|---|
| `postgresql.backends{state=active}` | count | `state='active'` |
| `postgresql.backends{state=idle}` | count | `state='idle'` |
| `postgresql.backends{state=idle_in_transaction}` | count | **Vacuum killer** — sessions holding open transactions while idle |
| `senhub.db.connections.max` | count | `max_connections` GUC |

### Throughput

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.queries.count` | counter | Commits + rollbacks across non-system DBs |
| `postgresql.commits` | counter | Sum of `pg_stat_database.xact_commit` |
| `postgresql.rollbacks` | counter | Sum of `pg_stat_database.xact_rollback` |

### Replication

Emitted only when role is primary or replica.

| Metric | Unit | Description |
|---|---|---|
| `postgresql.wal.lag{operation=replay}` | s | `now() - pg_last_xact_replay_timestamp()` |
| `senhub.db.replication.io_running` | bool | 1 if `pg_stat_wal_receiver.status='streaming'` |
| `senhub.db.replication.replicas.connected` | count | `count(pg_stat_replication)` (primary side) |

### Cache

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.buffer.hit_ratio` | ratio | `blks_hit / (blks_hit + blks_read)` across non-system DBs |

### Locks

| Metric | Unit | Description |
|---|---|---|
| `postgresql.deadlocks` | counter | Sum of `pg_stat_database.deadlocks` |
| `senhub.db.locks.waiting` | count | `pg_locks` where `granted=false` |
| `senhub.db.postgres.long_running_xact.seconds` | s | **Age of the oldest open transaction** — predicts vacuum starvation |

### Storage

| Metric | Unit | Description |
|---|---|---|
| `postgresql.db_size` | bytes | Sum of `pg_database_size()` across non-system DBs |
| `postgresql.table.count` | count | `pg_stat_user_tables` row count |
| `senhub.db.postgres.bloat.ratio{schema,relation}` | ratio | Dead / (live+dead) tuples — top-N tables by heap size |
| `senhub.db.postgres.bloat.bytes{schema,relation}` | bytes | Wasted bytes per top-N table |

### Backups

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.postgres.archiver.last_archived.age.seconds` | s | **DR canary** — staleness of WAL archiving |
| `senhub.db.postgres.archiver.failed.count` | counter | Cumulative archiver failures |

### Engine — `pg_stat_statements` (opt-in via extension)

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.postgres.stat_statements.calls.count` | counter | Aggregate `sum(calls)` |
| `senhub.db.postgres.stat_statements.exec_time.mean` | s | Aggregate mean exec time across all statements (was ms in ≤0.1.91) |

## Output formats

- **PRTG / Sensor Builder** — pick chips per family.
- **Nagios** — filter by family: `?tags=metric_type:replication`.
- **Prometheus** — `/api/{key}/prometheus/metrics`; metric names
  start with `senhub_db_*` and `postgresql_*`.
- **OTLP** — pushed as `senhub.db.*` and `postgresql.*`.

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
| `senhub.db.up = 0` | Network / firewall | Verify connectivity from agent to `host:port` |
| `senhub.db.postgres.stat_statements.*` missing | Extension not installed | See the [`pg_stat_statements`](#optional-pg_stat_statements) section |
| `senhub.db.postgres.archiver.*` missing | WAL archiving not configured | Expected — only emit when `archive_mode=on` and an archiver is running |
| All metrics missing | Role lacks `pg_monitor` | Re-run the `db-monitoring init` helper |
| `senhub.db.postgres.bloat.*` missing | Role cannot read `pg_stat_user_tables` | Same — needs `pg_monitor` |
