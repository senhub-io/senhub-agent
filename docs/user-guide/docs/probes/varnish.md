<img src="https://api.iconify.design/mdi/cached.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Varnish Cache

The `varnish` probe monitors Varnish Cache via `varnishstat -j -1`, reporting
cache hit/miss rates, client request throughput, backend connections, thread
lifecycle, session counts, object counts and memory allocation.

## Quick start

```yaml
probes:
  - name: varnish
    type: varnish
```

No parameters are required for a single-instance Varnish installation. The probe
runs `varnishstat` as the agent user — ensure it has access to the Varnish
shared memory file.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `instance_name` | — | Varnish instance name (`-n` flag for `varnishstat`). Required when multiple Varnish instances run on the same host |
| `varnishstat_path` | `varnishstat` | Path to the `varnishstat` binary if not in PATH |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.varnish.up` | 1 | 1 when `varnishstat` completed successfully |
| `varnish.cache.operations` | {operation} | Cache lookups by result (hit/miss/hitpass), tagged with `result` |
| `varnish.client.requests.received` | {request} | Client requests received |
| `varnish.backend.connections.failed` | {connection} | Failed backend connection attempts |
| `varnish.threads.created` | {thread} | Threads created since start |
| `varnish.threads.failed` | {thread} | Failed thread creation attempts |
| `varnish.threads.destroyed` | {thread} | Threads destroyed |
| `varnish.sessions.accepted` | {session} | Client sessions accepted |
| `varnish.sessions.dropped` | {session} | Sessions dropped due to overflow |
| `varnish.objects.stored` | {object} | Objects currently stored in cache |
| `varnish.memory.allocated` | By | Memory allocated for cache storage |

## Operational notes

- The `varnishstat` command reads from the Varnish shared memory log; the user running the agent must be in the `varnish` group or run as root.
- `instance_name` maps to `varnishstat -n <name>` and is needed when you run multiple Varnish instances (different working directories via `varnishd -n`).
- Metric names align with the OpenTelemetry Collector contrib `varnishreceiver` where equivalents exist.
