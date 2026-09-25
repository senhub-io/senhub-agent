<img src="../../assets/probe-logos/php-fpm.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost/fpm-status` | URL of the pool status page, answering in JSON |
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
| `phpfpm.accepted_connections` | {connection} | Total accepted connections since start |
| `phpfpm.listen_queue.current` | {connection} | Connections currently waiting in the listen queue |
| `phpfpm.listen_queue.max` | {connection} | Maximum observed listen queue length |
| `phpfpm.processes.active` | {process} | PHP-FPM worker processes currently serving a request |
| `phpfpm.processes.idle` | {process} | Idle worker processes |
| `phpfpm.processes.total` | {process} | Total worker processes in the pool |
| `phpfpm.slow_requests` | # | Requests exceeding the slow request threshold (cumulative) |
| `phpfpm.slow_requests` | {request} | Requests that exceeded `request_slowlog_timeout` |

## Operational notes

- Enable the status page in `php-fpm.conf`: `pm.status_path = /fpm-status`. The probe expects JSON output — configure Nginx or Apache to pass `?json` automatically, or set the endpoint to include `?json`.
- For multi-pool setups, create one probe instance per pool, each pointing to its own pool's status URL.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.phpfpm.up` | `phpfpm_up` | # | 1 when the PHP-FPM status endpoint is reachable, 0 otherwise |
| `phpfpm.uptime` | `phpfpm_uptime` | s | Number of seconds since the PHP-FPM pool started |
| `phpfpm.accepted_connections` | `phpfpm_accepted_connections` | # | Total number of accepted connections since pool start |
| `phpfpm.slow_requests` | `phpfpm_slow_requests` | # | Total number of requests exceeding the slow request threshold |
| `phpfpm.listen_queue.current` | `phpfpm_listen_queue_current` | # | Current number of requests waiting in the listen queue |
| `phpfpm.listen_queue.max` | `phpfpm_listen_queue_max` | # | Maximum number of requests observed in the listen queue since pool start |
| `phpfpm.processes.active` | `phpfpm_processes_active` | # | Number of active (currently serving requests) processes |
| `phpfpm.processes.idle` | `phpfpm_processes_idle` | # | Number of idle processes waiting for requests |
| `phpfpm.processes.total` | `phpfpm_processes_total` | # | Total number of processes (active + idle) |
| `phpfpm.max_children_reached` | `phpfpm_max_children_reached` | # | Total number of times the max_children limit was reached since pool start |

<!-- schema:metrics:end -->
