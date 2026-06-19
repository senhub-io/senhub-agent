!!! info
    **License: Free** — part of the universal collection tier.

# PHP-FPM

The `phpfpm` probe monitors a PHP-FPM pool via its JSON status page, reporting
pool uptime, process counts, queue depth, slow request counts and aggregate
request and connection statistics.

## Quick start

```yaml
probes:
  - name: phpfpm
    type: phpfpm
    params:
      endpoint: http://localhost/fpm-status
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost/fpm-status` | URL to the PHP-FPM status page (must return JSON: add `?json` or configure `pm.status_path` with the right format) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.phpfpm.up` | 1 | 1 when the status endpoint is reachable |
| `phpfpm.uptime` | s | Seconds since the PHP-FPM pool started, tagged with `pool` |
| `phpfpm.connections.accepted` | {connection} | Total accepted connections since start |
| `phpfpm.connections.queued` | {connection} | Connections currently waiting in the listen queue |
| `phpfpm.connections.queue_max` | {connection} | Maximum observed listen queue length |
| `phpfpm.processes.active` | {process} | PHP-FPM worker processes currently serving a request |
| `phpfpm.processes.idle` | {process} | Idle worker processes |
| `phpfpm.processes.total` | {process} | Total worker processes in the pool |
| `phpfpm.request.max_duration` | s | Duration of the longest-running request |
| `phpfpm.slow_requests` | {request} | Requests that exceeded `request_slowlog_timeout` |

## Operational notes

- Enable the status page in `php-fpm.conf`: `pm.status_path = /fpm-status`. The probe expects JSON output — configure Nginx or Apache to pass `?json` automatically, or set the endpoint to include `?json`.
- For multi-pool setups, create one probe instance per pool, each pointing to its own pool's status URL.
