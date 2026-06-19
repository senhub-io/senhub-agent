<img src="https://cdn.simpleicons.org/nginx" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Nginx

The `nginx` probe monitors Nginx by scraping the `ngx_http_stub_status_module`
status page, reporting active connections, request throughput and connection
state breakdown (reading, writing, waiting).

## Quick start

```yaml
probes:
  - name: nginx
    type: nginx
    params:
      endpoint: http://localhost/nginx_status
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost/nginx_status` | URL to the stub_status page |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nginx.up` | 1 | 1 when the stub_status page is reachable and parseable |
| `nginx.connections.current` | {connection} | Active connections currently being handled |
| `nginx.connections.accepted` | {connection} | Total connections accepted since nginx started (monotonic) |
| `nginx.connections.handled` | {connection} | Total connections handled since start |
| `nginx.requests.total` | {request} | Total HTTP requests handled since start |
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
- `nginx.connections.accepted`, `nginx.connections.handled` and `nginx.requests.total` are monotonically increasing counters since the last Nginx reload.
