<img src="https://cdn.simpleicons.org/haproxy" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8080/stats;csv` | HAProxy stats CSV endpoint URL |
| `username` | — | Basic-auth username (if the stats page is protected) |
| `password` | — | Basic-auth password |

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
| `haproxy.server.state` | 1 | Server operational state per backend server (1 = UP, 0 = DOWN) |

Metrics are tagged with `proxy` (proxy name) and `component` (FRONTEND / BACKEND / server name).

## Operational notes

- Enable the stats page in haproxy.cfg: `stats enable` + `stats uri /stats` inside a `listen stats` or `frontend` block.
- Adding `stats auth user:password` sets the credentials to pass in `username`/`password`.
- Metric names align with the OpenTelemetry Collector contrib `haproxyreceiver` where equivalents exist.
