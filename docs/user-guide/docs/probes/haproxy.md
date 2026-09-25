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

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.haproxy.up` | `senhub.haproxy.up` | HAProxy Up | # | 1 when the HAProxy stats endpoint is reachable and returns valid CSV |
| `haproxy.sessions.count` | `haproxy.sessions.count` | HAProxy {proxy} {component} Current Sessions | # | Current number of active sessions (scur) |
| `haproxy.sessions.total` | `haproxy.sessions.total` | HAProxy {proxy} {component} Total Sessions | # | Total number of sessions since last reset (stot) |
| `haproxy.bytes.input` | `haproxy.bytes.input` | HAProxy {proxy} {component} Bytes In | B | Total bytes received (bin) |
| `haproxy.bytes.output` | `haproxy.bytes.output` | HAProxy {proxy} {component} Bytes Out | B | Total bytes sent (bout) |
| `haproxy.connections.errors` | `haproxy.connections.errors` | HAProxy {proxy} {component} Connection Errors | # | Total connection errors (econ) |
| `haproxy.requests.errors` | `haproxy.requests.errors` | HAProxy {proxy} {component} Request Errors | # | Total request errors (ereq) — frontends only |
| `haproxy.responses.errors` | `haproxy.responses.errors` | HAProxy {proxy} {component} Response Errors | # | Total response errors (eresp) |
| `haproxy.requests.rate` | `haproxy.requests.rate` | HAProxy {proxy} {component} Request Rate | #/s | Current request rate in requests per second (req_rate) — frontends only |

<!-- schema:metrics:end -->
