<img src="https://cdn.simpleicons.org/clickhouse" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# ClickHouse

The `clickhouse` probe monitors a ClickHouse server by scraping the standard
Prometheus `/metrics` endpoint (available since ClickHouse 20.1), mapping the
key instantaneous gauges, async metrics, and cumulative profile-event counters.

## Quick start

```yaml
probes:
  - name: clickhouse
    type: clickhouse
    params:
      endpoint: http://localhost:8123
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8123` | ClickHouse HTTP interface base URL |
| `username` | `default` | ClickHouse user (must have SELECT access to system tables) |
| `password` | — | User password |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.clickhouse.up` | 1 | 1 when the `/metrics` endpoint answered successfully |
| `clickhouse.queries.active` | {query} | Queries currently executing (`ClickHouseMetrics_Query`) |
| `clickhouse.connections.tcp` | {connection} | Open TCP connections from clients |
| `clickhouse.connections.http` | {connection} | Open HTTP connections from clients |
| `clickhouse.merges.active` | {merge} | Background merge operations currently running |
| `clickhouse.parts.total` | {part} | Total data parts across all tables |
| `clickhouse.memory.used` | By | Process memory allocated by the ClickHouse server |
| `clickhouse.queries.inserted_rows` | {row} | Rows inserted since server start (profile counter) |
| `clickhouse.queries.select_count` | {query} | SELECT queries since server start |
| `clickhouse.queries.insert_count` | {query} | INSERT queries since server start |

## Operational notes

- The probe uses the Prometheus text format at `/metrics`, not the SQL interface. No extra user grant is required unless metrics access is restricted.
- ClickHouse 20.1+ exposes the Prometheus endpoint by default on the HTTP port (8123). Older installations require `prometheus.port` in the server config.
