<img src="../../assets/probe-logos/clickhouse.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# ClickHouse

The `clickhouse` probe monitors a ClickHouse server by scraping the standard
Prometheus `/metrics` endpoint (available since ClickHouse 20.1), mapping the
key instantaneous gauges, async metrics, and cumulative profile-event counters.

## Quick start

```yaml
# probes.d/10-clickhouse.yaml — each file under probes.d/ is a YAML array of probes
- name: clickhouse
  type: clickhouse
  params:
    endpoint: http://localhost:8123
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:6a078ca86c2c4b5b7a0ae445a6a7a5925f174f41f42f556e714f894a90edc5f4 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8123` | Base URL of the HTTP interface. Example: `http://clickhouse01:8123` |
| `username` | In practice | `default` | User with SELECT on the system tables |
| `password` | In practice | - | User's password; empty for a password-less user. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `database` | No | `system` | Accepted and stored but unused: every query the probe issues names the system database explicitly |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this server |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.clickhouse.up` | 1 | 1 when the `/metrics` endpoint answered successfully |
| `clickhouse.queries.active` | {query} | Queries currently executing (`ClickHouseMetrics_Query`) |
| `clickhouse.connections` | {connection} | Open client connections |
| `clickhouse.merges.active` | {merge} | Background merge operations currently running |
| `clickhouse.parts.active` | {part} | Total data parts across all tables |
| `clickhouse.memory.used` | By | Process memory allocated by the ClickHouse server |
| `clickhouse.inserted.rows` | {row} | Rows inserted since server start (profile counter) |
| `clickhouse.queries.select` | {query} | SELECT queries since server start |
| `clickhouse.queries.insert` | {query} | INSERT queries since server start |

## Operational notes

- The probe uses the Prometheus text format at `/metrics`, not the SQL interface. No extra user grant is required unless metrics access is restricted.
- ClickHouse 20.1+ exposes the Prometheus endpoint by default on the HTTP port (8123). Older installations require `prometheus.port` in the server config.

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
| `senhub.clickhouse.up` | `senhub.clickhouse.up` | ClickHouse {instance} Up | # | 1 when the ClickHouse /metrics endpoint answered successfully, 0 otherwise |
| `clickhouse.queries.active` | `clickhouse.queries.active` | ClickHouse {instance} Active Queries | # | Number of queries currently being processed (ClickHouseMetrics_Query) |
| `clickhouse.connections` | `clickhouse.connections` | ClickHouse {instance} Connections | # | Number of open client connections (ClickHouseMetrics_Connection) |
| `clickhouse.memory.used` | `clickhouse.memory.used` | ClickHouse {instance} Memory Used | B | Memory tracked by the query tracker (ClickHouseMetrics_MemoryTracking) |
| `clickhouse.parts.active` | `clickhouse.parts.active` | ClickHouse {instance} Active Parts | # | Active data parts in MergeTree tables (ClickHouseMetrics_Parts) |
| `clickhouse.merges.active` | `clickhouse.merges.active` | ClickHouse {instance} Active Merges | # | MergeTree background merges currently running (ClickHouseMetrics_Merge) |
| `clickhouse.uptime` | `clickhouse.uptime` | ClickHouse {instance} Uptime | s | Server uptime in seconds (ClickHouseAsyncMetrics_Uptime) |
| `clickhouse.queries.total` | `clickhouse.queries.total` | ClickHouse {instance} Total Queries | # | Cumulative number of queries executed (ClickHouseProfileEvents_Query) |
| `clickhouse.queries.select` | `clickhouse.queries.select` | ClickHouse {instance} SELECT Queries | # | Cumulative SELECT queries executed (ClickHouseProfileEvents_SelectQuery) |
| `clickhouse.queries.insert` | `clickhouse.queries.insert` | ClickHouse {instance} INSERT Queries | # | Cumulative INSERT queries executed (ClickHouseProfileEvents_InsertQuery) |
| `clickhouse.inserted.rows` | `clickhouse.inserted.rows` | ClickHouse {instance} Inserted Rows | # | Cumulative rows inserted (ClickHouseProfileEvents_InsertedRows) |
| `clickhouse.inserted.data` | `clickhouse.inserted.data` | ClickHouse {instance} Inserted Bytes | B | Cumulative bytes inserted (ClickHouseProfileEvents_InsertedBytes) |
| `clickhouse.read.data` | `clickhouse.read.data` | ClickHouse {instance} Read Compressed Bytes | B | Cumulative compressed bytes read from storage (ClickHouseProfileEvents_ReadCompressedBytes) |
| `clickhouse.written.data` | `clickhouse.written.data` | ClickHouse {instance} Written Compressed Bytes | B | Cumulative compressed bytes written to storage (ClickHouseProfileEvents_WriteCompressedBytes) |

<!-- schema:metrics:end -->
