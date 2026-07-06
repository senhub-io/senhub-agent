<img src="https://cdn.simpleicons.org/apache" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache HTTP Server

The `apache` probe monitors Apache HTTP Server via mod_status, collecting
request throughput, worker counts, active connections, traffic and uptime.
Requires `mod_status` enabled with the `?auto` format.

## Quick start

```yaml
# probes.d/10-apache.yaml — each file under probes.d/ is a YAML array of probes
- name: apache
  type: apache
  params:
    endpoint: http://localhost/server-status?auto
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost/server-status?auto` | URL to the mod_status endpoint (must include `?auto`) |
| `username` | — | Basic-auth username (if the status page is protected) |
| `password` | — | Basic-auth password — reference via `${secret:apache.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.apache.up` | 1 | 1 when mod_status responded, 0 otherwise |
| `apache.uptime` | s | Seconds since the server started |
| `apache.current_connections` | {connection} | Total open connections (Active, Waiting) |
| `apache.workers` | {worker} | Worker count by state (busy / idle) — tagged with `state` |
| `apache.requests.total` | {request} | Total requests handled since start |
| `apache.scoreboard` | {slot} | Scoreboard slot counts by state |
| `apache.traffic.total` | By | Total bytes transferred since start |

## Operational notes

- The `endpoint` must end with `?auto` (machine-readable text format). The HTML format is not supported.
- To protect the status page, add `Require ip 127.0.0.1` in the `<Location /server-status>` block and provide credentials here if an additional password layer is used.
- Metrics align with the OpenTelemetry Collector contrib `apachereceiver` naming convention.
