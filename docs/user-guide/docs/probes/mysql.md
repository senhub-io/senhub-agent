<img src="../../assets/probe-logos/mysql.svg" alt="" class="probe-page-logo probe-page-logo-si">

# MySQL / MariaDB

Monitors MySQL and MariaDB instances — self-hosted, AWS RDS, Aurora,
Cloud SQL, Azure Database, Supabase. Collects health, connections,
throughput, replication, buffer pool, locks, IO and storage from
`SHOW GLOBAL STATUS`, `SHOW GLOBAL VARIABLES`, `SHOW REPLICA
STATUS` and `information_schema`.

**License**: Free

## Prerequisites

- MySQL 5.7+ or MariaDB 10.3+
- Network access from the agent host to the database (default port `3306`)
- A monitoring user with the right grants — see [GRANTs](#grants) below

## Configuration

```yaml
# probes.d/20-mysql.yaml — each file under probes.d/ is a YAML array of probes
- name: production-mysql
  type: mysql
  params:
    host: db.example.com
    port: 3306
    username: senhub_monitor
    password: ${secret:production-mysql.password}   # OS secret store; inline plaintext is auto-sealed on install
    interval: 60
    timeout: 10
    tls: true
    per_database: false
```

### Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | In practice | `127.0.0.1` | Server hostname or address |
| `port` | In practice | `3306` | Server port |
| `username` | In practice | - | Monitoring user |
| `password` | In practice | - | Monitoring user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `database` | No | - | Database the connection opens on; optional |
| `tls` | No | - | TLS settings, or simply true |
| `tls.enabled` | No | `false` | Use TLS |
| `tls.skip_verify` | No | `false` | Accept the server certificate without verifying it. Also accepted: `insecure_skip_verify` |
| `tls.ca_file` | No | - | CA certificate (PEM) the server is verified against. Also accepted: `ca_cert` |
| `timeout` | No | `10s` | Query timeout, seconds or a duration |
| `interval` | No | `60` | Seconds between collections |
| `max_replication_lag_seconds` | No | `300s` | Lag past which a replica counts as unhealthy; 0 turns the lag term off |
| `per_database` | No | `false` | Emit per-database metrics. Also accepted: `expose_per_database` |
| `per_table` | No | `false` | Emit per-table metrics for the largest tables; needs per_database |
| `top_n_tables` | No | `20` | How many tables per_table covers. Also accepted: `expose_top_tables` |
| `instance_name` | No | - | Stable identity override for this server |

<!-- schema:params:end -->

`per_database` scales the series count with the number of databases;
`per_table` adds the largest tables on top of it. A replica whose two
threads are both running still counts as unhealthy once its lag passes
`max_replication_lag_seconds`; set it to `0` for a deliberately delayed
replica.

#### TLS

`tls: true` verifies the server's certificate against the system roots.
For a database behind an internal authority, or a lab with a self-signed
certificate, write a block instead:

```yaml
    tls:
      ca_file: /etc/ssl/db-ca.pem   # verify against this authority
      skip_verify: false            # true accepts any certificate — labs only
```

Naming a `ca_file`, or asking to skip verification, turns TLS on: there
is no reading of "configure how TLS behaves, then don't use it". A
`ca_file` the agent cannot read, or that holds no certificate, stops the
probe with that reason — it never falls back to the system roots, which
would verify against the wrong authority and look like it worked.

#### Renamed parameters

Two names changed when this probe replaced the paid one. Both still
work, and `agent config check` names the current spelling:

| Old | Now |
|---|---|
| `expose_per_database: true` | `per_database: true` |
| `expose_top_tables: 25` | `per_table: true` + `top_n_tables: 25` |

## GRANTs

The monitoring user needs read access to engine status, replication
state, and `information_schema`. Use the agent's helper command to
print the exact GRANT block for your version:

```bash
senhub-agent db-monitoring init --engine mysql --user senhub_monitor --host '%'
```

The helper does **not** connect to the database. Copy the SQL it
prints, review it, paste it into a DBA shell.

For MySQL 8.0:
```sql
CREATE USER 'senhub_monitor'@'%' IDENTIFIED BY 'STRONG-PASSWORD-HERE';
GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO 'senhub_monitor'@'%';
FLUSH PRIVILEGES;
```

For MariaDB 10.5.8+ add `SLAVE MONITOR ON *.*`.

## Collected Metrics

Every metric is tagged with `metric_type` so the PRTG Sensor
Builder splits them into family chips. The categories below match
those chips.

Every datapoint also carries the OTel resource attributes
`db.system.name` (`mysql`), `server.address`, and `server.port`.

### Overview

The 6 metrics every dashboard wants at a glance.

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | bool | 1 = last ping reached the server |
| `mysql.uptime` | s | Engine uptime |
| `senhub.db.version.info` | – | Always 1; version carried as label |
| `senhub.db.connections.utilization` | ratio | `(active+idle) / max_connections` |
| `senhub.db.replication.role` | enum | 0=standalone, 1=primary, 2=replica |
| `senhub.db.replication.health` | bool | Composite: io+sql threads running AND lag below threshold |

### Connections

| Metric | Unit | Description |
|---|---|---|
| `mysql.threads{kind=running}` | count | Sessions running a query |
| `senhub.db.connection.idle` | count | Connected but doing nothing |
| `senhub.db.connections.max` | count | Engine cap |
| `mysql.connection.errors{error=aborted_clients}` | counter | Client-side connection drops |
| `mysql.connection.errors{error=aborted_connects}` | counter | Auth failures + protocol errors |
| `mysql.connection.errors{error=max_connections}` | counter | Out-of-slots events |

### Throughput

| Metric | Unit | Description |
|---|---|---|
| `mysql.query.count` | counter | Total statements (rate = QPS) |
| `senhub.db.mysql.transaction.count{state=committed}` | counter | Successful commits |
| `senhub.db.mysql.transaction.count{state=rolled_back}` | counter | Rollbacks (app errors / deadlocks) |
| `senhub.db.mysql.slow_queries.count` | counter | Queries above `long_query_time` |
| `mysql.commands{command=…}` | counter | Per-verb rate (select/insert/update/delete/replace) |
| `senhub.db.mysql.tmp_tables.disk.ratio` | ratio | Disk temp / total temp — >25% suggests tuning |

### Replication

Emitted only when role is primary or replica. The probe auto-detects
role via `SHOW REPLICA STATUS` — no per-host configuration needed.

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.replication.lag.seconds` | s | Replica `Seconds_Behind_Source` |
| `senhub.db.replication.io_running` | bool | 1 = IO thread Yes |
| `senhub.db.replication.sql_running` | bool | 1 = SQL thread Yes |
| `senhub.db.replication.replicas.connected` | count | Downstream replicas (primary side) |

### Cache

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.mysql.buffer_pool.hit_ratio` | ratio | InnoDB buffer pool hit ratio — <99% under-provisioned |
| `senhub.db.buffer.utilization` | ratio | Pages data / pages total |
| `mysql.buffer_pool.data_pages{status=dirty}` | count | Pressure on the checkpointer |

### Locks

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.mysql.lock.deadlocks` | counter | App-level deadlocks — should be near zero |
| `senhub.db.locks.waiting` | count | Sessions currently blocked |
| `senhub.db.locks.row_lock_time.avg` | s | Average InnoDB row lock wait (was ms in ≤0.1.91) |

### IO

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.mysql.io{io.direction=read}` | counter (bytes) | InnoDB engine-side reads |
| `senhub.db.mysql.io{io.direction=write}` | counter (bytes) | InnoDB engine-side writes |

### Storage

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.database.size` | bytes | Total user-schema size |
| `senhub.db.mysql.table.count` | count | User tables across non-system schemas |

## Output formats

The probe emits the catalog above; each sink picks the metrics it
wants via the `metric_type` tag.

- **PRTG / Sensor URLs tab** — pick chips per family. A typical setup
  uses one "MySQL Overview" sensor (6 channels) and a "MySQL
  Replication" sensor (4 channels) per instance.
- **Nagios** — `/api/{key}/nagios/metrics/{probe-name}?tags=metric_type:overview`
- **Prometheus** — all metrics exposed under `senhub_db_mysql_*` and
  `mysql_*` names at `/api/{key}/prometheus/metrics`.
- **OTLP** — pushed as `senhub.db.mysql.*` and `mysql.*` to any
  OTel receiver.

## Auto-detected environments

The probe sets the `environment` tag from `@@version_comment` /
`version()`:

- `self_hosted` (default)
- `rds`, `aurora`
- `cloudsql`
- `azure_flexible`
- `supabase`

OS-level metrics that managed databases do not expose (e.g. data
directory mtime) are dropped silently.

## Cloud-managed instances

Tested against:

- AWS RDS for MySQL (8.0), Aurora MySQL — works as-is
- GCP Cloud SQL for MySQL — works as-is
- Azure Database for MySQL Flexible Server — works as-is

PlanetScale is out of scope for v1 (Vitess hides replication
behind the control plane).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `senhub.db.up = 0` | Network / firewall | Verify connectivity from the agent to `host:port` |
| `senhub.db.replication.*` all missing on a primary | `role=standalone` detected because no replica connected | Expected — replication family is replica-side |
| All metrics missing | GRANTs incomplete | Re-run the `db-monitoring init` helper |
| Empty per-table data | `expose_top_tables: 0` | Set `expose_top_tables: 20` (or another N) and reload |

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.db.up` | `db_up` | # | 1 if the agent's most recent ping reached the server, 0 otherwise |
| `mysql.uptime` | `db_uptime` | s | Seconds since the engine started (SHOW GLOBAL STATUS LIKE 'Uptime') |
| `senhub.db.version.info` | `db_version` | # | Engine version banner — value=1, version string carried in attribute db.system.version |
| `mysql.threads` | `mysql_threads_connected` | # | Currently open client sessions (Threads_connected) |
| `mysql.threads` | `mysql_threads_running` | # | Threads not sleeping — includes InnoDB background threads on MariaDB (Threads_running) |
| `senhub.db.connection.idle` | `db_connection_idle` | # | Idle client sessions — clamped max(0, Threads_connected − Threads_running) |
| `senhub.db.mysql.connection.max` | `mysql_connection_max` | # | max_connections variable (instantaneous cap) |
| `senhub.db.connection.utilization` | `db_connection_utilization` | % | Threads_connected / max_connections, percent (0-100) |
| `mysql.connection.errors` | `mysql_connection_errors_aborted_clients` | # | Connections aborted because the client died without closing properly (Aborted_clients) |
| `mysql.connection.errors` | `mysql_connection_errors_aborted_connects` | # | Failed connection attempts — auth or protocol errors (Aborted_connects) |
| `mysql.connection.errors` | `mysql_connection_errors_max_connections` | # | Connections rejected due to max_connections cap (Connection_errors_max_connections) |
| `mysql.query.count` | `mysql_query_count` | # | Statements executed (Questions counter) |
| `mysql.query.slow.count` | `mysql_query_slow_count` | # | Queries exceeding long_query_time (Slow_queries counter) |
| `mysql.commands` | `mysql_commands_{command}` | # | Statements executed per verb (Com_select, Com_insert, Com_update, Com_delete, Com_replace) |
| `senhub.db.mysql.transaction.count` | `mysql_transaction_committed` | # | Commits cumulatif (Com_commit). MySQL contrib n'expose pas les transactions — extension SenHub homogène avec mysql.commands. |
| `senhub.db.mysql.transaction.count` | `mysql_transaction_rolled_back` | # | Rollbacks cumulatif (Com_rollback). |
| `senhub.db.mysql.tmp_tables.disk_ratio` | `mysql_tmp_tables_disk_ratio` | % | Created_tmp_disk_tables / Created_tmp_tables — `Created_tmp_tables` already includes the disk-spilled subset on MySQL 8.0 and MariaDB; > 25% suggests increasing tmp_table_size. |
| `senhub.db.mysql.buffer_pool.hit_ratio` | `mysql_buffer_pool_hit_ratio` | % | 1 − (Innodb_buffer_pool_reads / Innodb_buffer_pool_read_requests). Derived ratio — contrib expose les compteurs bruts. |
| `senhub.db.mysql.buffer_pool.utilization` | `mysql_buffer_pool_utilization` | % | Innodb_buffer_pool_pages_data / Innodb_buffer_pool_pages_total |
| `mysql.buffer_pool.data_pages` | `mysql_buffer_pool_data_pages_dirty` | # | InnoDB pages waiting to be flushed to disk (Innodb_buffer_pool_pages_dirty) |
| `senhub.db.mysql.lock.deadlocks` | `mysql_lock_deadlocks` | # | Cumulative deadlocks detected (Innodb_deadlocks) — silently absent on MariaDB (variable not exposed) |
| `senhub.db.mysql.lock.waiting` | `mysql_lock_waiting` | # | Instantaneous count of transactions waiting on a row lock (Innodb_row_lock_current_waits) |
| `senhub.db.mysql.row_lock.time.avg` | `mysql_row_lock_time_avg` | ms | Average row lock wait time. Source Innodb_row_lock_time_avg in ms — converted to seconds for OTel (mapper ÷ 1000). |
| `senhub.db.mysql.io` | `mysql_io_read` | B | Bytes read by InnoDB (Innodb_data_read) |
| `senhub.db.mysql.io` | `mysql_io_write` | B | Bytes written by InnoDB (Innodb_data_written) |
| `senhub.db.database.size.total` | `db_database_size` | B | Total size of all user databases (information_schema.tables sum) |
| `senhub.db.mysql.table.count` | `mysql_table_count` | # | Number of user tables across all databases (excludes information_schema, performance_schema, mysql, sys) |
| `senhub.db.replication.role` | `db_replication_role` | # | Detected replication role of this instance |
| `senhub.db.replication.health` | `db_replication_health` | # | Composite gauge: 1 if replication looks healthy (or instance is standalone), 0 if degraded |
| `senhub.db.replication.replicas.connected` | `db_replication_replicas_connected` | # | On primary: number of replicas currently connected (SHOW REPLICAS / pg_stat_replication count) |
| `mysql.replica.time_behind_source` | `mysql_replica_time_behind_source` | s | On replica: seconds behind source (Seconds_Behind_Master / Seconds_Behind_Source) |
| `senhub.db.mysql.replica.io_thread.running` | `mysql_replica_io_thread_running` | # | On replica: 1 if Slave_IO_Running='Yes', 0 otherwise |
| `senhub.db.mysql.replica.sql_thread.running` | `mysql_replica_sql_thread_running` | # | On replica: 1 if Slave_SQL_Running='Yes', 0 otherwise |
| `senhub.db.database.size` | `db_database_size_per_database_{database}` | B | Size of an individual database (opt-in via per_database flag) |
| `senhub.db.mysql.table.size` | `mysql_table_size_{database}_{table}` | B | Size of an individual table (opt-in via per_table flag) |

<!-- schema:metrics:end -->
