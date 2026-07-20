<img src="https://cdn.simpleicons.org/oracle" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The Oracle Enterprise probe monitors Oracle Database instances that run **Enterprise Edition with the Diagnostics Pack**, collecting performance and availability metrics from the in-memory diagnostic views: `v$sysmetric` (AWR-equivalent 1-minute rates), `v$active_session_history` (ASH), the `gv$` cluster views for **Real Application Clusters (RAC)**, and `v$dataguard_stats` for **Data Guard** standby lag.

One probe instance monitors one Oracle service (SID / PDB); add more instances for additional databases. The monitored instance is also emitted as a `db` entity so it appears in the topology view, anchored from the moment the probe starts even while the database is unreachable.

!!! note "Diagnostics Pack licensing"
    `v$sysmetric` and `v$active_session_history` are part of the Oracle
    **Diagnostics Pack**, a separately licensed option of Enterprise Edition.
    For a Standard Edition or unlicensed instance, use the Free
    [Oracle Database](oracle.md) probe instead, which reads only the always-free
    dynamic performance views.

**Collected data:**

- Instance reachability
- AWR-equivalent per-second rates: DB time, DB CPU, hard/soft parses, logical and physical reads/writes, executions
- Active Session History: active sessions by wait class, and sessions on CPU
- RAC (optional): open instance count and per-instance SQL*Net traffic and global-cache blocks received
- Data Guard: redo apply lag and transport lag on the standby

All metrics are emitted under the `senhub.oracle_enterprise.*` namespace. Wait-class and per-RAC-instance series carry attributes (`wait_class`, `rac.instance`) that keep instances distinct in OTLP/Prometheus and act as filters in the Web UI Sensor Builder.

# Quick Start

## Basic Configuration

```yaml
# probes.d/40-oracle-enterprise.yaml — each file under probes.d/ is a YAML array of probes
- name: oracle-enterprise-prod
  type: oracle_enterprise
  params:
    dsn: "oracle://monitoring:${secret:oracle-prod.password}@oracle.company.com:1521/ORCLPDB1"
    interval: 300
    timeout: 30
    collect_rac: true
```

The `dsn` is a standard Oracle connection URL — `oracle://user:password@host:port/service`. The `${secret:...}` reference resolves the password from the OS-native secret store (see [Configuration](../configuration.md)); inline plaintext is auto-sealed on install.

## Single-instance database (no RAC)

Disable the `gv$` cluster queries on a non-clustered instance to avoid needless work:

```yaml
# probes.d/40-oracle-enterprise.yaml
- name: oracle-enterprise-single
  type: oracle_enterprise
  params:
    dsn: "oracle://monitoring:${secret:oracle-single.password}@db1.company.com:1521/XEPDB1"
    interval: 300
    collect_rac: false
```

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `dsn` | string | Yes | - | Oracle connection string, `oracle://user:password@host:port/service`. Reference the password via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. |
| `interval` | integer | No | `300` | Collection interval in seconds |
| `timeout` | integer | No | `30` | Per-cycle query timeout in seconds |
| `collect_rac` | boolean | No | `true` | Query the `gv$` cluster views for RAC metrics (set `false` on single-instance databases) |

# Metrics Collected

All metrics carry `engine`, `instance`, `server.address`, `server.port` and `service` attributes identifying the database. Families that split by a dimension (wait class, RAC instance) use an OTel attribute rather than separate metric names.

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.oracle_enterprise.up` | `1` | `1` when the instance answered this cycle, else `0` |

## AWR (v$sysmetric)

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.oracle_enterprise.awr.db_time` | `s` | Database time consumed per second |
| `senhub.oracle_enterprise.awr.db_cpu` | `s` | Database CPU time consumed per second |
| `senhub.oracle_enterprise.awr.parse.hard` | `{parse}/s` | Hard parses per second |
| `senhub.oracle_enterprise.awr.parse.soft` | `{parse}/s` | Soft parses per second |
| `senhub.oracle_enterprise.awr.logical_reads` | `{read}/s` | Logical reads per second |
| `senhub.oracle_enterprise.awr.physical_reads` | `{read}/s` | Physical reads per second |
| `senhub.oracle_enterprise.awr.physical_writes` | `{write}/s` | Physical writes per second |
| `senhub.oracle_enterprise.awr.executions` | `{execution}/s` | SQL executions per second |

## ASH (v$active_session_history)

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.oracle_enterprise.ash.active_sessions` | `{session}` | Active sessions over the last 5 minutes, split by `wait_class` |
| `senhub.oracle_enterprise.ash.cpu_sessions` | `{session}` | Active sessions on CPU over the last 5 minutes |

## RAC (gv$ views — when `collect_rac: true`)

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.oracle_enterprise.rac.instances` | `{instance}` | Number of open cluster instances |
| `senhub.oracle_enterprise.rac.network.io` | `By` | Cumulative SQL*Net bytes, per RAC instance (`rac.instance` attribute) |
| `senhub.oracle_enterprise.rac.gc.blocks_received` | `{block}` | Cumulative global-cache CR blocks received, per RAC instance (`rac.instance` attribute) |

`rac.network.io` and `rac.gc.blocks_received` are cumulative **counters** — consumers should apply a `rate()` (or PRTG delta) to read per-second throughput.

## Data Guard (v$dataguard_stats)

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.oracle_enterprise.dataguard.apply_lag` | `s` | Redo apply lag on the standby |
| `senhub.oracle_enterprise.dataguard.transport_lag` | `s` | Redo transport lag to the standby |

!!! note "Optional families"
    RAC metrics are only collected when `collect_rac: true` **and** the `gv$`
    views are accessible. Data Guard metrics are only emitted when
    `v$dataguard_stats` returns rows (a standby / Data Guard configuration);
    a primary-only instance simply reports no Data Guard series. Each family is
    independent — a failure in one does not prevent the others from emitting.

# Requirements

- **Oracle Database Enterprise Edition** with the **Diagnostics Pack** option licensed (required for `v$sysmetric` and `v$active_session_history`).
- A **database user** with `CREATE SESSION` and `SELECT` on the diagnostic views: `v$sysmetric`, `v$active_session_history`, `v$version`, `v$instance`, `v$database`, `v$dataguard_stats`, and — for RAC — `gv$instance` and `gv$sysstat`. Granting `SELECT_CATALOG_ROLE` (or `SELECT ANY DICTIONARY`) covers all of them; no administrative rights are required.
- Network path from the agent host to the database listener (default TCP port 1521).

# Outputs

Oracle Enterprise metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/oracle-enterprise-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/oracle-enterprise-prod"
```
