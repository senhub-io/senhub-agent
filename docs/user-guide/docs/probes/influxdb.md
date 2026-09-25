<img src="../../assets/probe-logos/influxdb.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# InfluxDB

The `influxdb` probe monitors InfluxDB 2.x availability and performance via the
standard `/health`, `/metrics` (Prometheus text format) and `/api/v2/buckets`
endpoints. No proprietary API calls are required beyond an optional read token
for the buckets endpoint.

## Quick start

```yaml
# probes.d/10-influxdb.yaml — each file under probes.d/ is a YAML array of probes
- name: influxdb
  type: influxdb
  params:
    endpoint: http://localhost:8086
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:b95542ecd5e9bcece71be2442b283136f8b45825463442cfeef95ee82189850e -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8086` | Base URL of the server. Example: `http://influx01:8086` |
| `token` | In practice | - | API token; empty skips the bucket count, /health and /metrics need none. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `org` | No | - | Organisation the bucket listing is scoped to; empty lists every bucket the token sees |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this server |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.influxdb.up` | 1 | 1 when GET `/health` returns `status=pass` |
| `influxdb.storage.reads` | {read} | Cumulative storage read operations |
| `influxdb.storage.writes` | {write} | Cumulative storage write operations |
| `influxdb.query.requests` | {request} | Query API requests received |
| `influxdb.storage.writes` | # | Storage write operations (cumulative) |
| `go.goroutines` | # | Goroutines running in the InfluxDB process |
| `influxdb.buckets` | # | Buckets in the organisation the token can read |

## Operational notes

- Without a `token`, the probe cannot query `/api/v2/buckets` and `influxdb.bucket.count` will not be emitted. All `/metrics` data remains available without authentication.
- Supports InfluxDB 2.x only. InfluxDB 1.x uses a different metrics format and is not covered by this probe.

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
| `senhub.influxdb.up` | `senhub.influxdb.up` | InfluxDB Up | # | 1 when GET /health returns status=pass, 0 otherwise |
| `influxdb.storage.reads` | `influxdb.storage.reads` | InfluxDB Storage Reads | # | Cumulative storage read operations (storage_reads_total or boltdb_reads_total) |
| `influxdb.storage.writes` | `influxdb.storage.writes` | InfluxDB Storage Writes | # | Cumulative storage write operations (storage_writes_total or boltdb_writes_total) |
| `influxdb.query.requests` | `influxdb.query.requests` | InfluxDB Query Requests | # | Cumulative HTTP query requests (http_query_request_bytes_total or query_requests_total) |
| `influxdb.tasks.runs.active` | `influxdb.tasks.runs.active` | InfluxDB Active Task Runs | # | Number of task scheduler runs currently executing (task_scheduler_currently_running_tasks) |
| `influxdb.tasks.runs.complete` | `influxdb.tasks.runs.complete` | InfluxDB Completed Task Runs | # | Cumulative completed task scheduler runs (task_scheduler_total_runs_complete) |
| `influxdb.tasks.runs.failed` | `influxdb.tasks.runs.failed` | InfluxDB Failed Task Runs | # | Cumulative failed task scheduler runs (task_scheduler_total_runs_failed) |
| `go.goroutines` | `go.goroutines` | InfluxDB Goroutines | # | Number of goroutines currently running in the InfluxDB process (go_goroutines) |
| `go.memory.heap.used` | `go.memory.heap.used` | InfluxDB Heap In Use | B | Bytes of heap memory in use by the InfluxDB process (go_memstats_heap_inuse_bytes) |
| `influxdb.buckets` | `influxdb.buckets` | InfluxDB Buckets | # | Total number of buckets in the InfluxDB organisation (requires token) |

<!-- schema:metrics:end -->
