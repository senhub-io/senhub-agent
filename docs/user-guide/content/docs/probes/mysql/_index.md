---
title: "MySQL / MariaDB"
weight: 15
---

# MySQL / MariaDB

Monitors MySQL and MariaDB instances — self-hosted, AWS RDS, Aurora,
Cloud SQL, Azure Database, Supabase. Collects health, connections,
throughput, replication, buffer pool, locks, IO and storage from
`SHOW GLOBAL STATUS`, `SHOW GLOBAL VARIABLES`, `SHOW REPLICA
STATUS` and `information_schema`.

**License**: Pro

## Prerequisites

- MySQL 5.7+ or MariaDB 10.3+
- Network access from the agent host to the database (default port `3306`)
- A monitoring user with the right grants — see [GRANTs](#grants) below

## Configuration

```yaml
probes:
  - name: production-mysql
    type: mysql
    params:
      host: db.example.com
      port: 3306
      username: senhub_monitor
      password: ${env:MYSQL_MONITOR_PASSWORD}
      interval: 60
      timeout: 10
      tls:
        enabled: true
        skip_verify: false
        ca_file: /etc/ssl/db-ca.pem
      max_replication_lag_seconds: 60
```

### Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `host` | Yes | - | Database hostname or IP |
| `port` | No | `3306` | TCP port |
| `username` | Yes | - | Monitoring user |
| `password` | Yes | - | Monitoring user's password (use `${env:VAR}` for secret) |
| `database` | No | `""` | Default database (optional; leave empty for server-level only) |
| `interval` | No | `60` | Collection interval in seconds |
| `timeout` | No | `10` | Per-query timeout in seconds |
| `tls.enabled` | No | `false` | Connect over TLS |
| `tls.skip_verify` | No | `false` | Skip server certificate verification |
| `tls.ca_file` | No | `""` | Path to CA certificate |
| `max_replication_lag_seconds` | No | `60` | Threshold used by the composite `db_replication_health` channel |
| `expose_per_database` | No | `false` | Emit per-database metrics (cardinality scales with #databases) |
| `expose_top_tables` | No | `0` | Emit metrics for the top-N tables by size; `0` disables this feature |

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

### Overview

The 6 metrics every dashboard wants at a glance.

| Metric | Unit | Description |
|---|---|---|
| `db_up` | bool | 1 = last ping reached the server |
| `db_uptime_seconds` | s | Engine uptime |
| `db_version_info` | – | Always 1; version carried as label |
| `db_connections_utilization` | ratio | `(active+idle) / max_connections` |
| `db_replication_role` | enum | 0=standalone, 1=primary, 2=replica |
| `db_replication_health` | bool | Composite: io+sql threads running AND lag below threshold |

### Connections

| Metric | Unit | Description |
|---|---|---|
| `db_connections_active` | count | Sessions running a query |
| `db_connections_idle` | count | Connected but doing nothing |
| `db_connections_max` | count | Engine cap |
| `db_connections_aborted` | counter | Auth failures + protocol errors |
| `db_connections_refused` | counter | Out-of-slots events |

### Throughput

| Metric | Unit | Description |
|---|---|---|
| `db_queries_count` | counter | Total statements (rate = QPS) |
| `db_transactions_committed` | counter | Successful commits |
| `db_transactions_rolled_back` | counter | Rollbacks (app errors / deadlocks) |
| `db_mysql_slow_queries_count` | counter | Queries above `long_query_time` |
| `db_mysql_command_count{command=...}` | counter | Per-verb rate (select/insert/update/delete/replace) |
| `db_mysql_tmp_tables_disk_ratio` | ratio | Disk temp / total temp — >25% suggests tuning |

### Replication

Emitted only when role is primary or replica. The probe auto-detects
role via `SHOW REPLICA STATUS` — no per-host configuration needed.

| Metric | Unit | Description |
|---|---|---|
| `db_replication_lag_seconds` | s | Replica `Seconds_Behind_Source` |
| `db_replication_io_running` | bool | 1 = IO thread Yes |
| `db_replication_sql_running` | bool | 1 = SQL thread Yes |
| `db_replication_replicas_connected` | count | Downstream replicas (primary side) |

### Cache

| Metric | Unit | Description |
|---|---|---|
| `db_buffer_hit_ratio` | ratio | InnoDB buffer pool hit ratio — <99% under-provisioned |
| `db_buffer_utilization` | ratio | Pages data / pages total |
| `db_buffer_dirty_pages` | count | Pressure on the checkpointer |

### Locks

| Metric | Unit | Description |
|---|---|---|
| `db_locks_deadlocks` | counter | App-level deadlocks — should be near zero |
| `db_locks_waiting` | count | Sessions currently blocked |
| `db_locks_row_lock_time_avg_ms` | ms | Innodb_row_lock_time_avg |

### IO

| Metric | Unit | Description |
|---|---|---|
| `db_io_read_bytes` | counter (bytes) | InnoDB engine-side reads |
| `db_io_write_bytes` | counter (bytes) | InnoDB engine-side writes |

### Storage

| Metric | Unit | Description |
|---|---|---|
| `db_size_bytes` | bytes | Total user-schema size |
| `db_tables_count` | count | User tables across non-system schemas |

## Output formats

The probe emits the catalog above; each sink picks the metrics it
wants via the `metric_type` tag.

- **PRTG / Sensor Builder** — pick chips per family. A typical setup
  uses one "MySQL Overview" sensor (6 channels) and a "MySQL
  Replication" sensor (4 channels) per instance.
- **Nagios** — `/api/{key}/nagios/metrics/{probe-name}?tags=metric_type:overview`
- **Prometheus** — all metrics exposed under `senhub_system_db_*`
  and `senhub_db_mysql_*` names at `/api/{key}/prometheus/metrics`.
- **OTLP** — pushed as `system.db.*` and `senhub.db.mysql.*` to any
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
| `db_up = 0` | Network / firewall | Verify connectivity from the agent to `host:port` |
| `db_replication_*` all missing on a primary | `role=standalone` detected because no replica connected | Expected — replication family is replica-side |
| All `db_*` missing | GRANTs incomplete | Re-run the `db-monitoring init` helper |
| Empty per-table data | `expose_top_tables: 0` | Set `expose_top_tables: 20` (or another N) and reload |
