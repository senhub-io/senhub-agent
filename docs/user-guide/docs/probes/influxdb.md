<img src="https://cdn.simpleicons.org/influxdb" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | No | `http://localhost:8086` | Base URL of the server. Example: `http://influx01:8086` |
| `token` | No | - | API token; empty skips the bucket count, /health and /metrics need none. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `org` | No | - | Organisation the bucket listing is scoped to; empty lists every bucket the token sees |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this server |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8086` | InfluxDB base URL |
| `token` | — | InfluxDB API token (needed for `/api/v2/buckets`; omit for metrics-only monitoring) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.influxdb.up` | 1 | 1 when GET `/health` returns `status=pass` |
| `influxdb.storage.reads` | {read} | Cumulative storage read operations |
| `influxdb.storage.writes` | {write} | Cumulative storage write operations |
| `influxdb.query.requests` | {request} | Query API requests received |
| `influxdb.write.requests` | {request} | Write API requests received |
| `influxdb.go.goroutines` | {goroutine} | Active Go goroutines in the InfluxDB process |
| `influxdb.bucket.count` | {bucket} | Number of buckets visible to the configured token |

## Operational notes

- Without a `token`, the probe cannot query `/api/v2/buckets` and `influxdb.bucket.count` will not be emitted. All `/metrics` data remains available without authentication.
- Supports InfluxDB 2.x only. InfluxDB 1.x uses a different metrics format and is not covered by this probe.
