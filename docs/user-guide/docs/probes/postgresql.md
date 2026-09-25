<img src="../../assets/probe-logos/postgresql.svg" alt="" class="probe-page-logo probe-page-logo-si">

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
    sslmode: verify-full
    sslrootcert: /etc/ssl/db-ca.pem
```

### Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:3d1f282d261dd88916ede8b7de1a122a810bea754666b79ae6a5a89efe6d0f42 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Server hostname or address |
| `port` | In practice | `5432` | Server port |
| `username` | Yes | - | Monitoring role |
| `password` | Yes | - | Role's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `database` | No | `postgres` | Database the connection opens on. Also accepted: `databases` |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10s` | Query timeout, seconds or a duration |
| `max_replication_lag_seconds` | No | `300s` | Replay lag past which a replica counts as unhealthy; 0 turns the lag term off |
| `sslmode` | No | - | libpq SSL mode; prefer by default. One of `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full` |
| `sslrootcert` | No | - | CA certificate path, libpq's name for tls.ca_file |
| `tls` | No | - | TLS settings; the block alone selects verify-full |
| `tls.skip_verify` | No | `false` | Accept the server certificate without verifying it (selects require). Also accepted: `insecure_skip_verify` |
| `tls.ca_file` | No | - | CA certificate the server is verified against. Also accepted: `ca_cert` |
| `instance_name` | No | - | Stable identity override for this cluster |

<!-- schema:params:end -->

- The probe reads cluster-wide views, so `database` only selects the connection. The list form `databases` uses its first entry.
- `sslmode` is passed to the driver as written and wins over the `tls` block. An unknown value stops the probe instead of reaching the server.
- Without `sslmode`, the connection negotiates TLS opportunistically (`prefer`). Setting a `tls` block selects `verify-full`; setting `tls.skip_verify` selects `require`, which is for a lab, not for production.
- `sslrootcert`, `tls.ca_file` and `tls.ca_cert` are the same setting.
- Set `instance_name` when the same server is reachable under several names, so the entity does not split.

#### Parameters this probe does not read

The paid probe this one replaced produced per-database and per-table
breakdowns. This one reports **cluster-wide totals** — `pg_stat_database`
is already summed — so three of its parameters have nothing to map onto
and are reported as errors by `agent config check` rather than accepted
and ignored:

| Parameter | Why |
|---|---|
| `expose_per_database` | there is no per-database breakdown to turn on |
| `expose_top_tables` | no per-table metrics are emitted |
| `bloat_top_n` | table bloat is not measured |

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

- **PRTG / Sensor URLs tab** — pick chips per family.
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

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.db.up` | `senhub.db.up` | Database Up | # | 1 if the agent's most recent ping reached the server, 0 otherwise |
| `senhub.db.postgresql.uptime` | `senhub.db.postgresql.uptime` | Uptime | s | Seconds since postmaster start (now() - pg_postmaster_start_time()). Contrib postgresqlreceiver n'expose pas l'uptime — extension. |
| `senhub.db.version.info` | `senhub.db.version.info` | Version | # | Engine version banner — value=1, version string carried in attribute db.system.version |
| `postgresql.backends` | `postgresql.backends` | Backends ({state}) | # | Client backends grouped by connection state (active / idle / idle_in_transaction). One datapoint per state value; discriminated by the 'state' tag. |
| `postgresql.connection.max` | `postgresql.connection.max` | Connections Max | # | max_connections GUC (instantaneous cap) |
| `senhub.db.connection.utilization` | `senhub.db.connection.utilization` | Connections Used % | % | (active + idle + idle_in_transaction) / max_connections, percent (0-100) |
| `postgresql.commits` | `postgresql.commits` | Transactions Committed | # | Cumulative commits across all databases (pg_stat_database.xact_commit sum) |
| `postgresql.rollbacks` | `postgresql.rollbacks` | Transactions Rolled Back | # | Cumulative rollbacks across all databases (pg_stat_database.xact_rollback sum) |
| `senhub.db.postgresql.buffer.hit_ratio` | `senhub.db.postgresql.buffer.hit_ratio` | Buffer Hit Ratio | % | blks_hit / (blks_hit + blks_read) over pg_stat_database. Dashboards 'santé' veulent le ratio dérivé — contrib n'expose que les compteurs bruts. |
| `postgresql.deadlocks` | `postgresql.deadlocks` | Deadlocks | # | Cumulative deadlocks detected (pg_stat_database.deadlocks) |
| `senhub.db.postgresql.lock.waiting` | `senhub.db.postgresql.lock.waiting` | Locks Waiting | # | Instantaneous count of granted=false rows in pg_locks |
| `senhub.db.postgresql.long_running_xact` | `senhub.db.postgresql.long_running_xact` | Long-Running Transaction Age | s | Age in seconds of the oldest active transaction (max(now() - xact_start) over pg_stat_activity where state IN ('active','idle in transaction')) |
| `postgresql.db_size` | `postgresql.db_size` | Database Size | B | Sum of pg_database_size() over all non-template databases |
| `postgresql.table.count` | `postgresql.table.count` | Tables Count | # | Count of relations across user schemas (pg_class where relkind='r') |
| `senhub.db.postgresql.archiver.failed` | `senhub.db.postgresql.archiver.failed` | Archiver Failures | # | Cumulative WAL archiver failures (pg_stat_archiver.failed_count) |
| `senhub.db.postgresql.archiver.last_archived.age` | `senhub.db.postgresql.archiver.last_archived.age` | Last Archived WAL Age | s | Seconds since last_archived_time. Only emitted when archive_mode is enabled. |
| `senhub.db.replication.role` | `senhub.db.replication.role` | Replication Role | # | Detected replication role of this instance |
| `senhub.db.replication.health` | `senhub.db.replication.health` | Replication Health | # | Composite gauge: 1 if replication looks healthy (or instance is standalone), 0 if degraded |
| `senhub.db.replication.replicas.connected` | `senhub.db.replication.replicas.connected` | Replicas Connected | # | On primary: number of streaming replicas connected (pg_stat_replication count) |
| `postgresql.wal.lag` | `postgresql.wal.lag` | Replication Lag | s | On replica: replay lag in seconds (now() - pg_last_xact_replay_timestamp()). Attribute operation=replay (contrib canon). |
| `senhub.db.postgresql.replica.io.running` | `senhub.db.postgresql.replica.io.running` | Replica IO Running | # | On replica: 1 if WAL receiver is connected (pg_stat_wal_receiver.status='streaming'), 0 otherwise |

<!-- schema:metrics:end -->
