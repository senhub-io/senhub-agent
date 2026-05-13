# Database Probes — Metrics Catalog

Single source of truth for the metrics emitted by the `mysql` and
`postgresql` probes. Each metric carries its OTel-style name, unit,
type, the SQL source the probe queries, the `metric_type` family
it sits in, and a tier (must / should / nice — see DESIGN §1).

> The YAML transformer files (`mysql.yaml`, `postgresql.yaml`) are
> generated from this table. Update this doc first, then the YAML.

Conventions:

- All metrics are prefixed `senhub.db.` so the Prometheus name
  always starts with `senhub_db_…`.
- The unit follows the OTel/UCUM convention (`{connection}`,
  `By`, `s`, `1`).
- Type is `gauge`, `counter` (monotonic), or `updowncounter`.
- Each metric carries the systematic tags `probe_name`,
  `probe_type` (`mysql` or `postgresql`), `instance` (the host:port
  the probe was configured against), `engine` (`mysql` or
  `postgresql`), and `metric_type` (the family).
- Replication-only metrics carry `role` (`primary`/`replica`/
  `standalone`); per-database metrics carry `database`;
  per-table metrics carry `schema` and `relation`.

## Family: `overview`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.up` | `1` | gauge | 1 = the probe successfully queried the engine on the last cycle | must | ping check |
| `senhub.db.uptime.seconds` | `s` | gauge | How long the engine has been running | must | MySQL: `Uptime`; PG: `pg_postmaster_start_time()` |
| `senhub.db.version.info` | `1` | gauge | Always 1; carries the version string as a `version` label | must | MySQL: `@@version`; PG: `version()` |
| `senhub.db.connections.utilization` | `1` | gauge | active+idle / max — primary saturation alarm | must | computed |
| `senhub.db.replication.health` | `1` | gauge | Composite 0/1 (see DESIGN §5.2) | should (SenHub) | computed |

## Family: `connections`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.connections.active` | `{connection}` | gauge | Sessions currently running queries | must | MySQL: `Threads_running`; PG: `pg_stat_activity` state='active' |
| `senhub.db.connections.idle` | `{connection}` | gauge | Sessions holding a slot but idle | must | MySQL: `Threads_connected - Threads_running`; PG: state='idle' |
| `senhub.db.connections.idle_in_transaction` | `{connection}` | gauge | Vacuum killer (PG-specific) | should | PG: state='idle in transaction' |
| `senhub.db.connections.max` | `{connection}` | gauge | Engine cap | must | MySQL: `max_connections`; PG: `max_connections` GUC |
| `senhub.db.connections.aborted` | `{connection}` | counter | Auth failures + protocol errors | should | MySQL: `Aborted_clients + Aborted_connects`; PG: `pg_stat_database` (deaths) |
| `senhub.db.connections.refused` | `{connection}` | counter | Out-of-slots refusals | should | MySQL: `Connection_errors_max_connections` |

## Family: `throughput`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.queries.count` | `{query}` | counter | Total statements | must | MySQL: `Questions`; PG: `xact_commit+xact_rollback` |
| `senhub.db.transactions.committed` | `{txn}` | counter | Successful commits | must | MySQL: `Com_commit`; PG: `xact_commit` |
| `senhub.db.transactions.rolled_back` | `{txn}` | counter | Rollbacks (app errors / deadlocks) | must | MySQL: `Com_rollback`; PG: `xact_rollback` |
| `senhub.db.mysql.command.count` | `{op}` | counter | Per-verb rate, label=`command` | should | `Com_select`, `Com_insert`, etc. |
| `senhub.db.mysql.slow_queries.count` | `{query}` | counter | Crosses `long_query_time` | should | `Slow_queries` |
| `senhub.db.mysql.tmp_tables.disk.ratio` | `1` | gauge | Disk temp / total temp; >0.25 = tuning needed | nice | computed |

## Family: `replication`

Emitted only when role is `primary` or `replica` (see DESIGN §5.1).

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.replication.role` | `1` | gauge | 0=standalone, 1=primary, 2=replica | must (SenHub) | role detect |
| `senhub.db.replication.lag.seconds` | `s` | gauge | Wall-clock lag (replica only) | must | MySQL: pt-heartbeat or `Seconds_Behind_Master` fallback; PG: `now()-pg_last_xact_replay_timestamp()` |
| `senhub.db.replication.lag.bytes` | `By` | gauge | Bytes behind primary | should | PG: `pg_wal_lsn_diff(sent_lsn, replay_lsn)`; MySQL: log-position delta |
| `senhub.db.replication.io_running` | `1` | gauge | IO thread up | must | MySQL: `Slave_IO_Running`; PG: `pg_stat_wal_receiver.status='streaming'` |
| `senhub.db.replication.sql_running` | `1` | gauge | SQL thread up (MySQL) | must | `Slave_SQL_Running` |
| `senhub.db.replication.replicas.connected` | `{replica}` | gauge | Downstream replicas attached (primary only) | should | MySQL: `Slaves_connected`; PG: `count(pg_stat_replication)` |
| `senhub.db.postgres.replication.slot.lag.bytes` | `By` | gauge | Inactive logical slot = unbounded WAL growth | should | `pg_replication_slots` |

## Family: `cache`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.buffer.hit_ratio` | `1` | gauge | <0.99 = under-provisioned RAM | must | MySQL: `1 - (Innodb_buffer_pool_reads / _read_requests)`; PG: `blks_hit/(blks_hit+blks_read)` |
| `senhub.db.buffer.utilization` | `1` | gauge | Used pages / total pages | should | MySQL: InnoDB page counters; PG: `pg_buffercache` (opt-in extension) |
| `senhub.db.buffer.dirty.pages` | `{page}` | gauge | Pressure on checkpointer | should | MySQL: `Innodb_buffer_pool_pages_dirty`; PG: bgwriter counters |
| `senhub.db.mysql.qcache.hit_ratio` | `1` | gauge | MySQL 5.x legacy query cache; absent on 8.0+ | nice | `Qcache_hits / (Qcache_hits + Com_select)` |

## Family: `locks`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.locks.deadlocks` | `{event}` | counter | App bug indicator; should be near zero | must | MySQL: `Innodb_deadlocks` or perf_schema; PG: `pg_stat_database.deadlocks` |
| `senhub.db.locks.waiting` | `{lock}` | gauge | Sessions blocked right now | should | MySQL: `Innodb_row_lock_current_waits`; PG: `pg_locks` not granted |
| `senhub.db.locks.row_lock_time.avg_ms` | `ms` | gauge | Average lock wait | should | MySQL: `Innodb_row_lock_time_avg` |
| `senhub.db.postgres.long_running_xact.seconds` | `s` | gauge | Oldest open transaction age | should (SenHub) | max(now()-xact_start) from `pg_stat_activity` |

## Family: `io`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.io.read.bytes` | `By` | counter | Engine-level reads | should | MySQL: `Innodb_data_read`; PG: `blks_read * block_size` |
| `senhub.db.io.write.bytes` | `By` | counter | Engine-level writes | should | MySQL: `Innodb_data_written`; PG: derived from bgwriter |
| `senhub.db.postgres.wal.bytes` | `By` | counter | WAL generated | must (PG) | `pg_stat_wal.wal_bytes` (PG14+) / lsn delta |
| `senhub.db.postgres.checkpoints.requested` | `{event}` | counter | Forced checkpoints — `max_wal_size` tuning issue | should | `pg_stat_bgwriter.checkpoints_req` |
| `senhub.db.postgres.checkpoints.timed` | `{event}` | counter | Scheduled checkpoints (healthy) | should | `pg_stat_bgwriter.checkpoints_timed` |
| `senhub.db.postgres.bgwriter.buffers_backend` | `{buffer}` | counter | Backends flushing themselves — bgwriter undersized | nice | `pg_stat_bgwriter.buffers_backend` |
| `senhub.db.mysql.binlog.size.bytes` | `By` | gauge | Total binlog disk used | should | `SHOW BINARY LOGS` |

## Family: `storage`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.size.bytes` | `By` | gauge | Total DB on disk (all user databases) | must | sum across non-system DBs |
| `senhub.db.tables.count` | `{table}` | gauge | Schema sprawl signal | nice | info_schema.tables / pg_class |
| `senhub.db.postgres.bloat.ratio` | `1` | gauge | Top-N tables dead-tuple ratio, label=`relation` | should (SenHub) | `pgstattuple_approx` or estimate |
| `senhub.db.postgres.bloat.bytes` | `By` | gauge | Wasted bytes per top-N table | should (SenHub) | same |

## Family: `per_database` (opt-in)

Emitted when `expose_per_database: true`. Carries a `database`
label. System databases (`mysql`, `performance_schema`,
`information_schema`, `sys`, `postgres`, `template0`, `template1`)
are skipped unless `include_system_databases: true`.

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.database.size.bytes` | `By` | gauge | Per-database size | should | MySQL: info_schema; PG: `pg_database_size()` |
| `senhub.db.database.transactions.committed` | `{txn}` | counter | Per-DB commits | nice | PG: `pg_stat_database.xact_commit` |
| `senhub.db.database.transactions.rolled_back` | `{txn}` | counter | Per-DB rollbacks | nice | PG: `pg_stat_database.xact_rollback` |

## Family: `per_table` (opt-in, top-N capped)

Emitted when `expose_top_tables: N > 0`. Top-N by storage size on
each scrape. Carries `schema` + `relation` labels.

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.table.size.bytes` | `By` | gauge | Per-table size | nice | info_schema / `pg_relation_size()` |
| `senhub.db.postgres.table.dead_tuples` | `{tuple}` | gauge | Bloat candidate | nice (SenHub) | `pg_stat_user_tables.n_dead_tup` |
| `senhub.db.postgres.table.last_autovacuum.age.seconds` | `s` | gauge | Vacuum lag per top-N table | nice (SenHub) | `pg_stat_user_tables.last_autovacuum` |

## Family: `backups`

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.postgres.archiver.last_archived.age.seconds` | `s` | gauge | DR canary — staleness of WAL archiving | should (SenHub) | `pg_stat_archiver.last_archived_time` |
| `senhub.db.postgres.archiver.failed.count` | `{event}` | counter | Archive failures | should | `pg_stat_archiver.failed_count` |
| `senhub.db.mysql.last_binlog.age.seconds` | `s` | gauge | Newest binlog mtime — write-activity + DR proxy | should (SenHub) | `SHOW BINARY LOGS` mtime (self-hosted only) |

## Family: `autovacuum` (PostgreSQL only)

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.postgres.autovacuum.workers.active` | `{worker}` | gauge | How many autovacuum processes are running | should | `pg_stat_activity` backend_type='autovacuum worker' |
| `senhub.db.postgres.autovacuum.lag.seconds` | `s` | gauge | Age of the most-overdue table since last vacuum (top-N) | should (SenHub) | `pg_stat_user_tables.last_autovacuum` |
| `senhub.db.postgres.dead_tuples.total` | `{tuple}` | gauge | Total dead rows in user tables (sum, low card.) | should | sum of `n_dead_tup` |

## Family: `engine`

### MySQL / InnoDB

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.mysql.innodb.row_lock.waits` | `{wait}` | counter | Contention counter | should | `Innodb_row_lock_waits` |
| `senhub.db.mysql.innodb.log.waits` | `{wait}` | counter | Redo-log buffer pressure | nice | `Innodb_log_waits` |
| `senhub.db.mysql.open_files` | `{file}` | gauge | vs `open_files_limit` — fd exhaustion proxy | nice | `Open_files` |
| `senhub.db.mysql.connections.aborted.count` | `{conn}` | counter | Bad creds / firewall storms | should | `Aborted_connects` |

### PostgreSQL (engine-specific)

| OTel name | Unit | Type | Description | Tier | Source |
|---|---|---|---|---|---|
| `senhub.db.postgres.stat_statements.calls.count` | `{call}` | counter | Aggregate calls across all statements | nice | `sum(pg_stat_statements.calls)` (version-aware) |
| `senhub.db.postgres.stat_statements.exec_time.mean_ms` | `ms` | gauge | Aggregate mean exec time | nice | weighted mean from `pg_stat_statements` |
| `senhub.db.postgres.temp_files.count` | `{file}` | counter | Tmp file spills — needs `work_mem` tuning | should | `pg_stat_database.temp_files` |
| `senhub.db.postgres.temp_files.bytes` | `By` | counter | Tmp file bytes written | should | `pg_stat_database.temp_bytes` |

## Lookups (PRTG)

PRTG operators get the same severity colouring as Veeam through
custom `.ovl` lookup files:

| Lookup ID | Values |
|---|---|
| `senhub.db.replication.role` | 0=STANDALONE (ok), 1=PRIMARY (ok), 2=REPLICA (ok) |
| `senhub.db.replication.health` | 0=UNHEALTHY (error), 1=HEALTHY (ok) |
| `senhub.db.up` | 0=DOWN (error), 1=UP (ok) |

Other gauges with native thresholds (utilization ratios, hit
ratios) do not need lookups — PRTG channel thresholds suffice.

## Channel breakdown — PRTG sensor templates

The agent's Sensor Builder offers per-`metric_type` chips. The
recommended sensor split for an operator who wants to import "all
of MySQL" is:

| Sensor (channel chip) | Channel count (typical) |
|---|---|
| `mysql/overview` | 5 |
| `mysql/connections` | 6 |
| `mysql/throughput` | 6 |
| `mysql/replication` (replica only) | 7 |
| `mysql/cache` | 4 |
| `mysql/locks` | 4 |
| `mysql/io` | 3 + binlog |
| `mysql/storage` | 2 |
| `mysql/engine` (InnoDB) | 4 |
| `mysql/backups` | 1 |

Postgres mirrors the same shape with the autovacuum / WAL specifics
in place of InnoDB.

## Differentiator summary table

| Differentiator | Metric(s) | Tier |
|---|---|---|
| Auto role-detect | `senhub.db.replication.role` (+ conditional emission of the family) | must |
| Composite replication health | `senhub.db.replication.health` | should |
| PG bloat estimate | `senhub.db.postgres.bloat.{ratio,bytes}` | should |
| Version-aware `pg_stat_statements` | `senhub.db.postgres.stat_statements.*` | nice |
| Backup freshness | `senhub.db.postgres.archiver.last_archived.age.seconds`, `senhub.db.mysql.last_binlog.age.seconds` | should |
| Saturation ratios | `senhub.db.connections.utilization`, `senhub.db.buffer.hit_ratio`, `senhub.db.buffer.utilization`, `senhub.db.mysql.tmp_tables.disk.ratio` | must |
| Idle-in-tx + long-running tx | `senhub.db.connections.idle_in_transaction`, `senhub.db.postgres.long_running_xact.seconds` | should |
| GRANT helper CLI | not a metric; ships as `senhub-agent db-monitoring init` | n/a |
