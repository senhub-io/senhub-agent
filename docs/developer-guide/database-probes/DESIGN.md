# Database Probes — Design (v1)

Two probes that monitor relational databases: `mysql` (covers
MySQL and MariaDB) and `postgresql` (covers community PostgreSQL
plus the managed flavours that speak the same wire protocol).

This doc is the contract the implementation refers back to. It
fixes the scope, the authentication model, the metric catalog and
the eight differentiators we commit to ship in v1.

## 1 — Scope and non-goals

### In scope (v1)

- **Engines**: MySQL 5.7+, MariaDB 10.3+, PostgreSQL 12+.
- **Cloud-managed**: AWS RDS / Aurora, GCP Cloud SQL, Azure Database
  for PostgreSQL/MySQL Flexible Server, Supabase. Probe connects
  with username + password (or TLS), queries the same views as on
  self-hosted, gracefully degrades when an OS-level metric is
  unavailable.
- **Output sinks**: PRTG, Nagios, Prometheus, OTLP. The probe emits
  the **complete** catalog into the cache; sinks consume what they
  want via the existing `metric_type` tag mechanism (same pattern as
  the Veeam probe).
- **License tier**: Pro (consistent with the other vendor probes).

### Out of scope (v1)

- **Query-level analytics** (per-statement latency, EXPLAIN
  capture). That is DBM territory and requires a query-store backend
  SenHub does not have.
- **Slow-query log parsing**. Impossible on managed databases where
  file access is denied.
- **Schema diffing / migration tracking**. Out of monitoring's lane.
- **Per-table metrics by default**. Opt-in only, capped via top-N
  (Prometheus cardinality is a real constraint, not a PRTG-only
  one).
- **PlanetScale**. Vitess-based; replication is hidden behind a
  control plane and the per-keyspace abstraction breaks our model.
  Documented as v2 candidate.

## 2 — Architecture

### Two probes, one shared support package

```
internal/agent/probes/mysql/
internal/agent/probes/postgresql/
internal/agent/probes/dbcommon/      ← helpers shared between the two
```

Decision: **two probes, not one with a `driver:` parameter**. MySQL
and PostgreSQL share enough at the *concept* level (connections,
replication, cache) that a unified abstraction is tempting, but at
the *query* level everything differs (`SHOW GLOBAL STATUS` vs
`pg_stat_*` views), and forcing a common shape would leak the
differences into every line of the metric YAML. Keeping the two
probes separate keeps each YAML readable.

`dbcommon` only carries pure helpers that don't depend on the
engine: connection-string parsing, retry/backoff timing, the
metric_type tag set, the top-N cap helper, and the `role_kind`
constants (`primary`, `replica`, `standalone`).

### Probe lifecycle

The probes embed `types.BaseProbe` like every other probe. The
collection loop is:

1. `OnStart`: validate config, open a single connection, ping it,
   determine engine version once and cache it. Bail out with a
   descriptive error if the user is missing the required GRANTs.
2. `Collect(ctx)`: every `interval` seconds — open or reuse the
   connection, detect role, fan out the family queries in parallel,
   merge the result into a `[]DataPoint`, return.
3. `OnStop`: close the connection.

No connection pool. One connection per probe instance is enough
and avoids surprising the DBA with extra sessions. The connection
is kept open between collects (idle timeout depends on the engine's
default `wait_timeout` / `idle_in_transaction_session_timeout`, the
probe re-pings before each cycle).

### Driver choice

- MySQL: `github.com/go-sql-driver/mysql` (canonical, pure Go, no
  CGO).
- PostgreSQL: `github.com/jackc/pgx/v5` (modern, pure Go, more
  type-safe than `lib/pq`).

Both expose `database/sql` so the probe code stays driver-agnostic
where it can.

## 3 — Authentication

### Connection parameters (config YAML)

```yaml
probes:
  - name: production-mysql
    type: mysql
    params:
      host: db.example.com
      port: 3306
      username: senhub_monitor
      password: ${env:MYSQL_MONITOR_PASSWORD}
      database: ""              # optional, default "" = server-level only
      interval: 60
      timeout: 10
      tls:
        enabled: false
        skip_verify: false
        ca_file: ""
      # Differentiator switches (see §5)
      expose_per_database: false
      expose_top_tables: 0        # 0 = disabled; integer N = top-N by size
      bloat_top_n: 10              # PG only
```

Postgres mirrors the same shape with `port: 5432` default and a
`sslmode: require|verify-ca|verify-full|disable` field instead of
the structured `tls` block (Postgres expects libpq-style names).

### Required GRANTs (documented + helper CLI — see §5.8)

**MySQL/MariaDB**:
```sql
CREATE USER 'senhub_monitor'@'agent_host' IDENTIFIED BY '...';
GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO 'senhub_monitor'@'agent_host';
-- MariaDB 10.5.8+:
GRANT SLAVE MONITOR ON *.* TO 'senhub_monitor'@'agent_host';
FLUSH PRIVILEGES;
```

**PostgreSQL 10+**:
```sql
CREATE ROLE senhub_monitor LOGIN PASSWORD '...';
GRANT pg_monitor TO senhub_monitor;
-- PG 9.x: per-view selects on pg_stat_database, pg_stat_replication, ...
```

The helper CLI in §5.8 prints the right block for the detected
engine version.

## 4 — Metric catalog (split by `metric_type`)

The probe emits every metric with two tag axes that downstream
sinks filter on:

- `metric_type`: one of the family values below. PRTG sensors and
  Sensor Builder chips are organised around this.
- `engine`: `mysql` or `postgresql` — useful when both probes run
  on the same agent.

### Family matrix

| `metric_type` | Cardinality | Probe activation | Notes |
|---|---|---|---|
| `overview` | low | always | one composite row per instance |
| `connections` | low | always | active, idle, idle-in-tx (PG), max, refused, aborted |
| `throughput` | low | always | QPS, TPS, commits, rollbacks, slow queries |
| `replication` | low | always (auto role-detect, see §5.1) | role, lag (s + bytes), threads, replicas connected, composite health |
| `cache` | low | always | buffer hit ratio, utilization, dirty pages |
| `locks` | low | always | deadlocks, waiting, row_lock_time, long-running tx |
| `io` | low | always | reads, writes, WAL/checkpoints (PG), binlog (MySQL) |
| `storage` | low | always | total DB size, bloat ratio (PG, top-N), table count |
| `per_database` | medium (= #databases) | opt-in `expose_per_database: true` | size, commits, rollbacks per DB |
| `per_table` | high | opt-in `expose_top_tables: N` | top-N by size; capped to prevent cardinality blow-up |
| `backups` | low | always when source exists | archiver age (PG), binlog mtime (MySQL) |
| `autovacuum` | low (or N if per-table opt-in) | PG only, always | workers active, lag, dead tuples |
| `engine` | low | always | InnoDB/MyISAM (MySQL) or replication-slot specifics (PG) |

### Full catalog (reference)

The metrics list, OTel names, units, sources and tier (must / should
/ nice) live in [METRICS.md](METRICS.md). That table is the single
source of truth — the YAML transformer derives directly from it.

### `metric_type` tag values — final list

```
overview
connections
throughput
replication
cache
locks
io
storage
per_database
per_table
backups
autovacuum
engine
```

## 5 — Differentiators

### 5.1 Auto-detect primary / replica role

On each `Collect`, the probe runs:

- **MySQL**: `SHOW SLAVE STATUS` (or `SHOW REPLICA STATUS` on MySQL
  8.0.22+). Empty result → primary or standalone. Non-empty →
  replica; the result row carries IO/SQL thread state and lag.
- **PostgreSQL**: `SELECT pg_is_in_recovery()`. `true` → replica;
  `false` → primary or standalone.

A primary with no downstream replicas is reported as
`role="standalone"`; a primary with at least one connected replica
is `role="primary"`. Encoded as a numeric gauge via the
`senhub.db.replication.role` lookup (0=standalone, 1=primary,
2=replica) so PRTG can colour the channel.

Effect: replica-only metrics (lag, IO/SQL thread states) are
**not emitted** on a standalone or primary; users do not have to
turn them off by hand and Prometheus stops seeing `NaN` series.

### 5.2 Composite `replication.health` gauge

A single 0/1 gauge derived from:

```
healthy = io_thread_ok AND sql_thread_ok
       AND lag_seconds < max_replication_lag_seconds  (config; default 60)
       AND heartbeat_age_seconds < max_heartbeat_age_seconds  (default 300)
```

For PG, `io_thread_ok` reads `pg_stat_wal_receiver.status='streaming'`,
`sql_thread_ok` is always true.

One green/red value in PRTG, one alert rule in Prometheus, instead
of having to express the same compound check in five places.

### 5.3 PostgreSQL bloat estimate (top-N tables)

Uses the `pgstattuple_approx()` function when the extension is
available, falling back to the lightweight estimate from
`pg_class.reltuples` and average row width. Capped to the top N
tables by size (default 10, configurable via `bloat_top_n`). Two
metrics: `senhub.db.postgres.bloat.ratio` and `…bloat.bytes`,
tagged with `relation` and `schema`.

No OSS exporter ships this today.

### 5.4 Version-aware `pg_stat_statements`

The exporter community has been bitten repeatedly by column
renames (`pg_stat_statements` shifted columns in PG 13 and again
in PG 17). The probe queries `server_version_num` once at
`OnStart` and selects the matching column projection from a small
map keyed by major version.

Aggregate-only metrics in v1 (total calls, mean exec time across
all statements). Per-statement metrics are explicitly out of scope.

### 5.5 Backup freshness

- PG: `senhub.db.postgres.archiver.last_archived.age.seconds` from
  `pg_stat_archiver.last_archived_time`.
- MySQL: `senhub.db.mysql.last_binlog.age.seconds` derived from
  `SHOW BINARY LOGS` mtime of the newest file (where the agent has
  filesystem access; managed instances fall back to "unavailable"
  + WARN log).

Both surfaces as `metric_type: backups`.

### 5.6 Saturation as ratios

For every paired counter (`active/max`, `used/total`, `disk_temp/
total_temp`, `buffer_used/buffer_total`), the probe computes the
ratio probe-side and exposes it as a `_ratio` (unit `1`, gauge in
[0,1]). The raw absolute numbers stay available too — but the ratio
is what dashboards and PRTG thresholds bind to.

### 5.7 Idle-in-transaction as a first-class channel

`senhub.db.connections.idle_in_transaction` is a top-tier metric,
not buried in a per-state breakdown. PG-only.

A long-running variant —
`senhub.db.postgres.long_running_xact.seconds` — exposes the age of
the **oldest** transaction currently open. This is the metric that
predicts vacuum starvation.

### 5.8 GRANT helper CLI

New `senhub-agent` subcommand:

```bash
senhub-agent db-monitoring init --engine mysql --host db.example.com --user senhub_monitor
```

Connects with the credentials given, detects engine + version, and
prints a paste-ready SQL block with the exact privileges needed.
Does not execute anything itself — DBA paste, audit, run.

Optional `--dry-run` prints without connecting (uses a static
default version).

## 6 — Cardinality controls

Per-database and per-table metrics are opt-in. Even when enabled,
the probe applies caps:

- `expose_per_database: true` — fanned over all user databases.
  Internal/system databases (`mysql`, `performance_schema`,
  `information_schema`, `sys`, `postgres`, `template0`, `template1`)
  are skipped unless `include_system_databases: true`.
- `expose_top_tables: N` — top-N by storage size on each scrape.
  Stable ranking is good enough; this is for capacity tracking,
  not real-time per-row throughput.
- `bloat_top_n: N` — PG only, capped at 50 to keep the
  `pgstattuple_approx` cost bounded.

The cardinality contract is documented in the probe's user-facing
doc so operators choose with eyes open.

## 7 — Cloud-managed degradation

The probe emits `senhub.db.environment` tag (`self_hosted`,
`rds`, `aurora`, `cloudsql`, `azure_flexible`, `supabase`,
`unknown`). Detection is heuristic — based on `version()` output
plus `current_setting('cluster_name')` for Postgres and
`@@version_comment` for MySQL. A wrong guess never breaks the
probe; the tag is informational.

Metrics that are unavailable on managed instances:

| Metric | Reason | Probe behaviour |
|---|---|---|
| OS-level CPU/IO (host stats) | Not exposed by managed DBs | Not emitted (use the cloud provider's CloudWatch / Stackdriver / Azure Monitor) |
| Binlog mtime (MySQL) | No filesystem access | `last_binlog.age.seconds` not emitted; one-shot WARN |
| WAL filesystem | No filesystem access | Use `pg_stat_wal` instead (PG14+); pre-14 managed: emit only WAL position |

All other metrics work identically.

## 8 — Testing strategy

- **Unit**: pure helpers in `dbcommon/` (connection string parsing,
  role-kind enum, ratio computation, top-N cap).
- **Integration with engine in Docker**: spin up MySQL 5.7 / 8.0,
  MariaDB 10.5 / 11.x, PostgreSQL 12 / 14 / 16 / 17 via testcontainers
  or a `docker-compose` fixture; assert the probe collects N
  expected metrics, role-detect flips correctly when replication is
  configured, and the GRANT helper output is accepted by the engine.
- **Cardinality regression**: with `expose_top_tables: 20` and 1000
  synthetic tables, the probe must emit exactly 20 per-table series
  (not 1000).
- **Cloud-managed simulation**: an integration test where the test
  fixture revokes filesystem access mid-scrape; the probe must
  degrade gracefully (no panic, channel disappears).

CI: the Docker-based integration tests run as a separate job
because they take longer than the existing `go test` set. Gated
behind a `make test-database` target so the standard `make test`
stays fast.

## 9 — Implementation order

1. **Design** (this doc) — done.
2. **METRICS.md** — full catalog table with OTel names, units, types,
   sources, tier. Single source of truth for the YAML.
3. **`dbcommon/` package** — connection string parser, role-kind,
   cap helpers, tests.
4. **MySQL probe skeleton** — connect + ping + version detect +
   role detect + emit overview family. End-to-end through cache to
   `/metrics` and PRTG JSON.
5. **MySQL must-have catalog** — `connections`, `throughput`,
   `replication`, `cache`, `locks`, `storage`, `io`.
6. **PostgreSQL probe skeleton** — same shape, `pgx`.
7. **PostgreSQL must-have catalog**.
8. **Differentiators 5.1–5.4**: role-detect, composite health,
   bloat, version-aware pg_stat_statements.
9. **Differentiators 5.5–5.7**: backup freshness, saturation
   ratios, idle-in-tx.
10. **Differentiator 5.8** — GRANT helper CLI.
11. **Per-database + per-table opt-in** with cardinality tests.
12. **Cloud-managed adaptation** + integration test.
13. **YAML transformer files** — `mysql.yaml`, `postgresql.yaml`.
14. **PRTG lookups** — `senhub.db.replication.role`, etc.
15. **User documentation** under `docs/user-guide/content/docs/probes/`
    (mysql + postgresql pages, family-organised like Veeam).
16. **Grafana dashboards** — one per engine, drop into the
    `docs/grafana/` catalog.

## 10 — License-tier implications

`mysql` and `postgresql` join the Pro tier. The free-tier wildcard
in `/scripts/license-generator/` does not change.

The `db-monitoring init` CLI helper is **available for free** — it
does not run the probe, only prints SQL. Useful as a marketing
hook ("try the GRANT helper before buying Pro").

## 11 — Open questions

- TLS authentication using client certificates: target v1 or v2?
  RDS IAM token rotation (15-min expiry) — probe v2 or build now?
- Aurora Serverless v2 idle pauses — does the probe need to handle
  the 401-like wake-up window gracefully? Likely yes, but evidence
  required from a real Aurora SV2 test target.

These do not block the v1 build but should be answered before
declaring v1 GA.
