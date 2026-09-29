<img src="../../assets/probe-logos/apache.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:55260c3017a6f508aac2f4febee9f210116b8617f88c4672ae31e9a988c426fd -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost/server-status?auto` | URL of the mod_status page, with ?auto |
| `username` | No | - | Basic-auth user when the status page is protected; empty for none |
| `password` | No | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this server; set it when two apache probes run on one host |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.apache.up` | 1 | 1 when mod_status responded, 0 otherwise |
| `apache.uptime` | s | Seconds since the server started |
| `apache.current_connections` | {connection} | Total open connections (Active, Waiting) |
| `apache.workers` | {worker} | Worker count by state (busy / idle) — tagged with `state` |
| `apache.requests` | {request} | Total requests handled since start |
| `apache.workers` | {worker} | Workers by state: busy (serving a request) or idle |
| `apache.traffic` | By | Total bytes transferred since start |

## Operational notes

- The `endpoint` must end with `?auto` (machine-readable text format). The HTML format is not supported.
- To protect the status page, add `Require ip 127.0.0.1` in the `<Location /server-status>` block and provide credentials here if an additional password layer is used.
- Metrics align with the OpenTelemetry Collector contrib `apachereceiver` naming convention.

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
| `senhub.apache.up` | `senhub.apache.up` | Apache Up | # | 1 when mod_status responded successfully, 0 otherwise |
| `apache.uptime` | `apache.uptime` | Apache Uptime | s | Time in seconds since the Apache server was started |
| `apache.current_connections` | `apache.current_connections` | Apache Current Connections | # | Total number of connections currently served by Apache (ConnsTotal) |
| `apache.workers` | `apache.workers` | Apache Workers {state} | # | Number of Apache workers in each state: busy (serving requests) or idle (waiting) |
| `apache.requests` | `apache.requests` | Apache Requests | # | Cumulative number of HTTP requests served since Apache started (Total Accesses) |
| `apache.traffic` | `apache.traffic` | Apache Traffic | B | Cumulative bytes transferred since Apache started (Total kBytes * 1024) |

<!-- schema:metrics:end -->
