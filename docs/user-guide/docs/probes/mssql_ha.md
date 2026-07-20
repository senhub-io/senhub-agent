<img src="https://cdn.simpleicons.org/microsoftsqlserver" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The SQL Server AlwaysOn probe monitors the health of SQL Server AlwaysOn Availability Groups through the SQL Server Dynamic Management Views (DMVs). It reports per-replica role, synchronization health and connectivity, and per-database lag, queue sizes and throughput rates. One probe instance monitors one SQL Server instance; add more instances for additional servers.

**Collected data:**

- Server reachability
- Per-replica role (primary / secondary), synchronization health and connectivity
- Per-database secondary lag
- Per-database log-send and redo queue sizes
- Per-database log-send and redo throughput rates

All metrics are emitted under the `senhub.mssql_ha.*` namespace. Replica series
carry an availability-group and replica attribute; database series carry an
availability-group and database attribute. These attributes also act as filters
in the Web UI Sensor Builder.

The probe also emits a `db` entity for the monitored SQL Server instance so that
the topology view can build a node from it — surfaced even when the server is
unreachable or has no Availability Group configured.

# Quick Start

## Basic Configuration

```yaml
# probes.d/20-mssql_ha.yaml — each file under probes.d/ is a YAML array of probes
- name: mssql-ha-prod
  type: mssql_ha
  params:
    host: sqlnode1.example.com
    port: 1433
    username: monitoring
    password: "${secret:mssql-ha-prod.password}"   # OS secret store; inline plaintext is auto-sealed on install
    encrypt: "true"
    trust_server_cert: false
    interval: 60
```

The `${secret:...}` reference resolves the password from the OS-native secret
store (see [Configuration](../configuration.md)).

## Multiple Instances

Monitor several SQL Server instances with separate probe instances:

```yaml
# probes.d/20-mssql_ha.yaml
- name: mssql-ha-dc1
  type: mssql_ha
  params:
    host: sqlnode1.example.com
    username: monitoring
    password: "${secret:mssql-ha-dc1.password}"

- name: mssql-ha-dc2
  type: mssql_ha
  params:
    host: sqlnode2.example.com
    username: monitoring
    password: "${secret:mssql-ha-dc2.password}"
    encrypt: "disable"   # test/lab instance with no TLS
```

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `host` | string | Yes | - | SQL Server hostname or IP. For a named instance use `host\InstanceName` |
| `port` | integer | No | `1433` | SQL Server TCP port |
| `username` | string | Yes | - | SQL Server login with read access to the AlwaysOn DMVs |
| `password` | string | No | - | Password for the SQL login — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `encrypt` | string | No | `true` | TLS negotiation: `true`, `false` or `disable` (go-mssqldb convention) |
| `trust_server_cert` | boolean | No | `false` | Trust the server TLS certificate without validation (set `true` for self-signed server certificates) |
| `interval` | integer | No | `60` | Collection interval in seconds |
| `timeout` | integer | No | `30` | Per-cycle connection and query timeout in seconds |

# Metrics Collected

## Overview

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.mssql_ha.up` | `1` | `1` when the last ping reached the server this cycle, else `0` |

## Replicas

Each replica series carries an `ag.name` (availability group) and `replica.name`
attribute.

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.mssql_ha.replica.role` | `1` | Replica role (Primary=1, Secondary=0) |
| `senhub.mssql_ha.replica.health` | `1` | Synchronization health (Healthy=1, otherwise 0) |
| `senhub.mssql_ha.replica.connected` | `1` | Connectivity state (Connected=1, Disconnected=0) |

## Databases

Each database series carries an `ag.name` (availability group) and
`database.name` attribute.

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.mssql_ha.database.lag` | `s` | Estimated secondary-replica lag behind the primary |
| `senhub.mssql_ha.log_send_queue` | `By` | Log records on the primary not yet sent to the secondary |
| `senhub.mssql_ha.redo_queue` | `By` | Log records received by the secondary not yet redone |
| `senhub.mssql_ha.log_send_rate` | `By/s` | Rate at which log is sent from the primary to the secondary |
| `senhub.mssql_ha.redo_rate` | `By/s` | Rate at which received log is redone on the secondary |

## Filtering (Web UI Sensor Builder)

The PRTG/Web UI Sensor Builder exposes filters for this probe:

- **Metric Type** (category) — overview, replicas, databases
- **Availability Group** — pick a specific AG
- **Replica** — pick a specific replica
- **Database** — pick a specific availability database

# Requirements

- **SQL Server with AlwaysOn Availability Groups** enabled and reachable from the agent host (default TCP port 1433).
- A **SQL Server login** with permission to read the AlwaysOn DMVs — `VIEW SERVER STATE` is sufficient. A minimal-privilege account is recommended over `sa`.
- The probe reads `sys.dm_hadr_availability_replica_states`, `sys.availability_replicas`, `sys.availability_groups` and `sys.dm_hadr_database_replica_states`.

!!! note "AlwaysOn required"
    This probe reports Availability Group replication health only. For general
    SQL Server health and throughput (batch/transaction rates, buffer cache,
    per-database I/O), use the [SQL Server](mssql.md) probe — the two are
    complementary and can run side by side against the same instance.

# Outputs

SQL Server AlwaysOn metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/mssql-ha-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/mssql-ha-prod"
```
