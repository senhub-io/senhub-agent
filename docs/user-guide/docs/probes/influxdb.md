!!! info
    **License: Free** — part of the universal collection tier.

# InfluxDB

The `influxdb` probe monitors InfluxDB 2.x availability and performance via the
standard `/health`, `/metrics` (Prometheus text format) and `/api/v2/buckets`
endpoints. No proprietary API calls are required beyond an optional read token
for the buckets endpoint.

## Quick start

```yaml
probes:
  - name: influxdb
    type: influxdb
    params:
      endpoint: http://localhost:8086
```

## Parameters

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
