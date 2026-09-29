<img src="../../assets/probe-logos/clickhouse.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# ClickHouse

The `clickhouse` probe monitors a ClickHouse server through its HTTP
interface (port 8123). It reads `system.metrics`, `system.events` and
`system.asynchronous_metrics` in one query per collection: active queries,
connections, memory, parts and merges, and the query, insert and I/O
counters since the server started.

## Quick start

```yaml
# probes.d/10-clickhouse.yaml — each file under probes.d/ is a YAML array of probes
- name: clickhouse
  type: clickhouse
  params:
    endpoint: http://localhost:8123
    username: senhub
    password: ${secret:clickhouse.password}
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

Active queries, open client connections, memory tracked by the server,
active parts and running merges, uptime, and the cumulative query, insert
and read and write counters. The full list is in the
[metric reference](#metric-reference) below.

## Operational notes

- The probe needs a user that can read the `system` tables, nothing more.
  A read-only user is enough:
  `CREATE USER senhub IDENTIFIED BY '…' SETTINGS readonly = 1; GRANT SELECT ON system.* TO senhub;`
- It does not use the Prometheus endpoint, which ClickHouse leaves off
  unless a `<prometheus>` block gives it a port of its own. `/metrics` on
  port 8123 answers 404 on a default install.
- A counter the server has never incremented, such as the INSERT count on
  a server that has received none, is reported as 0.

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
| `senhub.clickhouse.up` | `senhub.clickhouse.up` | ClickHouse {instance} Up | # | 1 when the ClickHouse HTTP interface answered the system-table query, 0 otherwise |
| `clickhouse.queries.active` | `clickhouse.queries.active` | ClickHouse {instance} Active Queries | # | Number of queries currently being processed (system.metrics Query) |
| `clickhouse.connections` | `clickhouse.connections` | ClickHouse {instance} Connections | # | Open client connections: TCP, HTTP, MySQL and PostgreSQL interfaces (system.metrics) |
| `clickhouse.memory.used` | `clickhouse.memory.used` | ClickHouse {instance} Memory Used | B | Memory tracked by the query tracker (system.metrics MemoryTracking) |
| `clickhouse.parts.active` | `clickhouse.parts.active` | ClickHouse {instance} Active Parts | # | Active data parts in MergeTree tables (system.metrics PartsActive) |
| `clickhouse.merges.active` | `clickhouse.merges.active` | ClickHouse {instance} Active Merges | # | MergeTree background merges currently running (system.metrics Merge) |
| `clickhouse.uptime` | `clickhouse.uptime` | ClickHouse {instance} Uptime | s | Server uptime in seconds (system.asynchronous_metrics Uptime) |
| `clickhouse.queries.total` | `clickhouse.queries.total` | ClickHouse {instance} Total Queries | # | Cumulative number of queries executed (system.events Query) |
| `clickhouse.queries.select` | `clickhouse.queries.select` | ClickHouse {instance} SELECT Queries | # | Cumulative SELECT queries executed (system.events SelectQuery) |
| `clickhouse.queries.insert` | `clickhouse.queries.insert` | ClickHouse {instance} INSERT Queries | # | Cumulative INSERT queries executed (system.events InsertQuery) |
| `clickhouse.inserted.rows` | `clickhouse.inserted.rows` | ClickHouse {instance} Inserted Rows | # | Cumulative rows inserted (system.events InsertedRows) |
| `clickhouse.inserted.data` | `clickhouse.inserted.data` | ClickHouse {instance} Inserted Bytes | B | Cumulative bytes inserted (system.events InsertedBytes) |
| `clickhouse.read.data` | `clickhouse.read.data` | ClickHouse {instance} Read Compressed Bytes | B | Cumulative compressed bytes read from storage (system.events ReadCompressedBytes) |
| `clickhouse.written.data` | `clickhouse.written.data` | ClickHouse {instance} Written Compressed Bytes | B | Cumulative compressed bytes written to MergeTree parts by inserts (system.events MergeTreeDataWriterCompressedBytes) |

<!-- schema:metrics:end -->
