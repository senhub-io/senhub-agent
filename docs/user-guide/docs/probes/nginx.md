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

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.nginx.up` | `nginx_up` | # | 1 when the stub_status page is reachable and parseable, 0 otherwise |
| `nginx.connections.current` | `nginx_connections_current` | # | Number of client connections currently being handled (accepted + in-flight) |
| `nginx.connections.accepted` | `nginx_connections_accepted` | # | Total connections accepted since nginx start (monotonically increasing counter) |
| `nginx.connections.handled` | `nginx_connections_handled` | # | Total connections handled since nginx start; equals accepted when no resource limit is hit |
| `nginx.requests` | `nginx_requests` | # | Total HTTP requests processed since nginx start |
| `nginx.connections.reading` | `nginx_connections_reading` | # | Connections where nginx is reading the request header |
| `nginx.connections.writing` | `nginx_connections_writing` | # | Connections where nginx is writing the response to the client |
| `nginx.connections.waiting` | `nginx_connections_waiting` | # | Idle keep-alive connections waiting for a request |

<!-- schema:metrics:end -->
