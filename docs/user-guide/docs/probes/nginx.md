<img src="../../assets/probe-logos/nginx.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Nginx

The `nginx` probe monitors Nginx by scraping the `ngx_http_stub_status_module`
status page, reporting active connections, request throughput and connection
state breakdown (reading, writing, waiting).

## Quick start

```yaml
# probes.d/20-nginx.yaml — each file under probes.d/ is a YAML array of probes
- name: nginx
  type: nginx
  params:
    endpoint: http://localhost/nginx_status
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:204e0f3d2adc1ce007882b35b7ce3512492caaccfe504ad46bea4ec09f400100 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost/nginx_status` | URL of the stub_status page |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this server; set it when two nginx probes run on one host |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nginx.up` | 1 | 1 when the stub_status page is reachable and parseable |
| `nginx.connections.current` | {connection} | Active connections currently being handled |
| `nginx.connections.accepted` | {connection} | Total connections accepted since nginx started (monotonic) |
| `nginx.connections.handled` | {connection} | Total connections handled since start |
| `nginx.requests` | {request} | Total HTTP requests handled since start |
| `nginx.connections.reading` | {connection} | Connections reading the request header |
| `nginx.connections.writing` | {connection} | Connections writing the response |
| `nginx.connections.waiting` | {connection} | Idle keep-alive connections waiting for a request |

## Operational notes

- Enable `ngx_http_stub_status_module` and expose it in `nginx.conf`:
  ```nginx
  location /nginx_status {
      stub_status;
      allow 127.0.0.1;
      deny all;
  }
  ```
- The module is included in most Nginx packages by default; verify with `nginx -V 2>&1 | grep stub_status`.
- `nginx.connections.accepted`, `nginx.connections.handled` and `nginx.requests` are monotonically increasing counters since the last Nginx reload.

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
| `senhub.nginx.up` | `senhub.nginx.up` | Nginx Up | # | 1 when the stub_status page is reachable and parseable, 0 otherwise |
| `nginx.connections.current` | `nginx.connections.current` | Nginx Active Connections | # | Number of client connections currently being handled (accepted + in-flight) |
| `nginx.connections.accepted` | `nginx.connections.accepted` | Nginx Accepted Connections | # | Total connections accepted since nginx start (monotonically increasing counter) |
| `nginx.connections.handled` | `nginx.connections.handled` | Nginx Handled Connections | # | Total connections handled since nginx start; equals accepted when no resource limit is hit |
| `nginx.requests` | `nginx.requests` | Nginx Total Requests | # | Total HTTP requests processed since nginx start |
| `nginx.connections.reading` | `nginx.connections.reading` | Nginx Reading Connections | # | Connections where nginx is reading the request header |
| `nginx.connections.writing` | `nginx.connections.writing` | Nginx Writing Connections | # | Connections where nginx is writing the response to the client |
| `nginx.connections.waiting` | `nginx.connections.waiting` | Nginx Waiting Connections | # | Idle keep-alive connections waiting for a request |

<!-- schema:metrics:end -->
