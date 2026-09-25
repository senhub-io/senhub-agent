<img src="../../assets/probe-logos/varnish.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Varnish Cache

The `varnish` probe monitors Varnish Cache via `varnishstat -j -1`, reporting
cache hit/miss rates, client request throughput, backend connections, thread
lifecycle, session counts, object counts and memory allocation.

## Quick start

```yaml
# probes.d/10-varnish.yaml — each file under probes.d/ is a YAML array of probes
- name: varnish
  type: varnish
```

No parameters are required for a single-instance Varnish installation. The probe
runs `varnishstat` as the agent user — ensure it has access to the Varnish
shared memory file.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `varnishstat_path` | No | `varnishstat` | Path of the varnishstat binary when it is not on the PATH. Example: `/usr/bin/varnishstat` |
| `instance_name` | No | - | Varnish instance name passed as -n; needed when several instances run on the host |
| `interval` | No | `60` | Seconds between collections |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.varnish.up` | 1 | 1 when `varnishstat` completed successfully |
| `varnish.cache.operations` | {operation} | Cache lookups by result (hit/miss/hitpass), tagged with `result` |
| `varnish.client.requests.received` | {request} | Client requests received |
| `varnish.backend.connections.fail` | {connection} | Failed backend connection attempts |
| `varnish.thread.operations` | # | Thread lifecycle events, tagged with `operation` (created, destroyed, failed) |
| `varnish.session.connections` | # | Client sessions accepted |
| `varnish.session.dropped` | {session} | Sessions dropped due to overflow |
| `varnish.objects.stored` | {object} | Objects currently stored in cache |
| `varnish.memory.allocated` | By | Memory allocated for cache storage |

## Operational notes

- The `varnishstat` command reads from the Varnish shared memory log; the user running the agent must be in the `varnish` group or run as root.
- `instance_name` maps to `varnishstat -n <name>` and is needed when you run multiple Varnish instances (different working directories via `varnishd -n`).
- Metric names align with the OpenTelemetry Collector contrib `varnishreceiver` where equivalents exist.

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
| `senhub.varnish.up` | `senhub.varnish.up` | Varnish Up | # | 1 when varnishstat completed successfully, 0 otherwise |
| `varnish.cache.operations` | `varnish.cache.operations` | Varnish Cache {result} | # | Number of cache lookup results by type (hit, miss, hitpass) |
| `varnish.client.requests.received` | `varnish.client.requests.received` | Varnish Client Requests | # | Total client requests received |
| `varnish.backend.connections.success` | `varnish.backend.connections.success` | Varnish Backend Connections Success | # | Backend connections successfully established |
| `varnish.backend.connections.fail` | `varnish.backend.connections.fail` | Varnish Backend Connections Failed | # | Backend connection attempts that failed |
| `varnish.backend.connections.reused` | `varnish.backend.connections.reused` | Varnish Backend Connections Reused | # | Backend connections reused from keep-alive pool |
| `varnish.thread.operations` | `varnish.thread.operations` | Varnish Threads {operation} | # | Thread lifecycle events by operation (created, destroyed, failed) |
| `varnish.session.connections` | `varnish.session.connections` | Varnish Session Connections | # | Accepted client sessions |
| `varnish.session.dropped` | `varnish.session.dropped` | Varnish Session Dropped | # | Client sessions dropped because the session queue was full |
| `varnish.objects.stored` | `varnish.objects.stored` | Varnish Objects Stored | # | Number of HTTP objects currently stored in the cache |
| `varnish.memory.allocated` | `varnish.memory.allocated` | Varnish Memory Allocated | bytes | Total bytes currently allocated across all storage allocators (SMA/SMF) |

<!-- schema:metrics:end -->
