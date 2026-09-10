<img src="https://cdn.simpleicons.org/php" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# PHP-FPM

The `phpfpm` probe monitors a PHP-FPM pool via its JSON status page, reporting
pool uptime, process counts, queue depth, slow request counts and aggregate
request and connection statistics.

## Quick start

```yaml
# probes.d/20-php-fpm.yaml — each file under probes.d/ is a YAML array of probes
- name: phpfpm
  type: phpfpm
  params:
    endpoint: http://localhost/fpm-status
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | No | `http://localhost/fpm-status` | URL of the pool status page, answering in JSON |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this pool; set it when two phpfpm probes run on one host |

<!-- schema:params:end -->

The status page must answer in JSON: add `?json` to the endpoint or configure the web server to pass it.

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
