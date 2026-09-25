<img src="../../assets/probe-logos/haproxy.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# HAProxy

The `haproxy` probe monitors HAProxy via its stats CSV HTTP endpoint, collecting
session counts, request throughput and error counters per frontend, backend
and server component.

## Quick start

```yaml
# probes.d/10-haproxy.yaml — each file under probes.d/ is a YAML array of probes
- name: haproxy
  type: haproxy
  params:
    endpoint: http://localhost:8080/stats;csv
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8080/stats;csv` | URL of the stats page in CSV form |
| `username` | No | - | Basic-auth user when the stats page is protected; empty for none |
| `password` | No | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this load balancer; set it when two haproxy probes run on one host |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.haproxy.up` | 1 | 1 when the stats endpoint is reachable and returns valid CSV |
| `haproxy.sessions.count` | {session} | Current sessions per proxy/component |
| `haproxy.sessions.total` | {session} | Total sessions since last reload, per proxy/component |
| `haproxy.bytes.input` | By | Bytes received per proxy/component |
| `haproxy.bytes.output` | By | Bytes sent per proxy/component |
| `haproxy.requests.errors` | {error} | HTTP request errors per proxy/component |
| `haproxy.connections.errors` | {error} | Connection errors per proxy/component |

Metrics are tagged with `proxy` (proxy name) and `component` (FRONTEND / BACKEND / server name).

## Operational notes

- Enable the stats page in haproxy.cfg: `stats enable` + `stats uri /stats` inside a `listen stats` or `frontend` block.
- Adding `stats auth user:password` sets the credentials to pass in `username`/`password`.
- Metric names align with the OpenTelemetry Collector contrib `haproxyreceiver` where equivalents exist.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.haproxy.up` | `haproxy_up` | # | 1 when the HAProxy stats endpoint is reachable and returns valid CSV |
| `haproxy.sessions.count` | `haproxy_sessions_{proxy}_{component}` | # | Current number of active sessions (scur) |
| `haproxy.sessions.total` | `haproxy_sessions_total_{proxy}_{component}` | # | Total number of sessions since last reset (stot) |
| `haproxy.bytes.input` | `haproxy_bytes_in_{proxy}_{component}` | B | Total bytes received (bin) |
| `haproxy.bytes.output` | `haproxy_bytes_out_{proxy}_{component}` | B | Total bytes sent (bout) |
| `haproxy.connections.errors` | `haproxy_econ_{proxy}_{component}` | # | Total connection errors (econ) |
| `haproxy.requests.errors` | `haproxy_ereq_{proxy}_{component}` | # | Total request errors (ereq) — frontends only |
| `haproxy.responses.errors` | `haproxy_eresp_{proxy}_{component}` | # | Total response errors (eresp) |
| `haproxy.requests.rate` | `haproxy_req_rate_{proxy}_{component}` | #/s | Current request rate in requests per second (req_rate) — frontends only |

<!-- schema:metrics:end -->
