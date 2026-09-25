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

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.clickhouse.up` | `clickhouse_up` | # | 1 when the ClickHouse /metrics endpoint answered successfully, 0 otherwise |
| `clickhouse.queries.active` | `clickhouse_queries_active` | # | Number of queries currently being processed (ClickHouseMetrics_Query) |
| `clickhouse.connections` | `clickhouse_connections` | # | Number of open client connections (ClickHouseMetrics_Connection) |
| `clickhouse.memory.used` | `clickhouse_memory_used` | B | Memory tracked by the query tracker (ClickHouseMetrics_MemoryTracking) |
| `clickhouse.parts.active` | `clickhouse_parts_active` | # | Active data parts in MergeTree tables (ClickHouseMetrics_Parts) |
| `clickhouse.merges.active` | `clickhouse_merges_active` | # | MergeTree background merges currently running (ClickHouseMetrics_Merge) |
| `clickhouse.uptime` | `clickhouse_uptime` | s | Server uptime in seconds (ClickHouseAsyncMetrics_Uptime) |
| `clickhouse.queries.total` | `clickhouse_queries_total` | # | Cumulative number of queries executed (ClickHouseProfileEvents_Query) |
| `clickhouse.queries.select` | `clickhouse_queries_select` | # | Cumulative SELECT queries executed (ClickHouseProfileEvents_SelectQuery) |
| `clickhouse.queries.insert` | `clickhouse_queries_insert` | # | Cumulative INSERT queries executed (ClickHouseProfileEvents_InsertQuery) |
| `clickhouse.inserted.rows` | `clickhouse_inserted_rows` | # | Cumulative rows inserted (ClickHouseProfileEvents_InsertedRows) |
| `clickhouse.inserted.data` | `clickhouse_inserted_data` | B | Cumulative bytes inserted (ClickHouseProfileEvents_InsertedBytes) |
| `clickhouse.read.data` | `clickhouse_read_data` | B | Cumulative compressed bytes read from storage (ClickHouseProfileEvents_ReadCompressedBytes) |
| `clickhouse.written.data` | `clickhouse_written_data` | B | Cumulative compressed bytes written to storage (ClickHouseProfileEvents_WriteCompressedBytes) |

<!-- schema:metrics:end -->
