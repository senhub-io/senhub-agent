<img src="https://cdn.simpleicons.org/mysql" alt="" class="probe-page-logo probe-page-logo-si">

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
| `password` | Yes | - | Monitoring user's password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `database` | No | `""` | Default database (optional; leave empty for server-level only) |
| `interval` | No | `60` | Collection interval in seconds |
| `timeout` | No | `10` | Per-query timeout in seconds |
| `tls.enabled` | No | `false` | Connect over TLS |
| `tls.skip_verify` | No | `false` | Skip server certificate verification |
| `tls.ca_file` | No | `""` | Path to CA certificate |
| `max_replication_lag_seconds` | No | `60` | Threshold used by the composite `senhub.db.replication.health` channel |
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

- **PRTG / Sensor Builder** — pick chips per family. A typical setup
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
