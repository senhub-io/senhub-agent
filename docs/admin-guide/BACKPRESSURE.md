# OTLP Backpressure & Resilience

The OTLP strategy buffers metrics in an in-memory last-writer-wins (LWW)
store and exports them on a timer. Four mechanisms keep that buffer
bounded and the agent healthy when the backend is slow, unreachable, or
the metric volume explodes — so a monitoring agent never becomes the
thing that takes a host down.

This page is the **configuration & tuning** guide. For reading the
runtime counters these mechanisms expose, see
[OTLP Pipeline Observability](./OTLP-OBSERVABILITY.md).

## The four mechanisms

| Mechanism | Protects against | Drop reason (in `/info/otlp`) |
|---|---|---|
| Global cardinality cap | Unbounded series growth across the whole agent | `store_cap` |
| Per-probe cardinality budget | One probe (e.g. multi-instance Citrix/NetScaler) exploding series | `probe_cardinality` |
| Memory limiter | Heap blow-up during a prolonged backend outage | `memory_soft_limit`, `memory_hard_limit` |
| Persistent checkpoint (metrics) | Losing the **metric** store across an agent restart while the backend is down | — (no loss) |
| Logs dead-letter queue | Losing **event logs** during a backend outage (queued to disk, replayed at boot and on recovery) | `logs_queue_full` (only when the disk cap is hit) |
| Endpoint failover | The primary ingress being down (switch to a standby ingress, return to primary on recovery) | — (no loss; switch is logged + counted) |

A fifth, **parallel export** (`max_concurrent_exports`), splits a large
snapshot into per-probe sub-batches exported concurrently — throughput,
not backpressure — and is covered briefly at the end.

## Configuration reference

All keys live under an `otlp` storage's `params`:

```yaml
storage:
  - name: otlp
    params:
      endpoint: "otel-collector.internal:4317"
      fallback_endpoints:                 # standby ingresses for failover (#217); empty = none
        - "otel-collector-dr.internal:4317"
      signals:
        metrics: { enabled: true, interval: 30s }

      # --- backpressure / resilience ---
      max_store_size: 50000               # global series cap (0 = unbounded)
      max_active_series_per_probe: 10000  # per-probe series budget (0 = unbounded)

      memory_limit:
        soft_mib: 200      # refuse NEW series above this heap; keep updating existing (0 = off)
        hard_mib: 400      # refuse ALL inserts + force GC above this heap (0 = off)
        interval: 5s       # heap poll cadence

      persistence:
        path: "/var/lib/senhub-agent/otlp-checkpoint"  # DIRECTORY; empty = persistence off
        interval: 30s                                   # metric checkpoint save cadence
        logs_queue_max_bytes: 134217728                 # logs dead-letter disk cap (0 = default 128 MiB)

      max_concurrent_exports: 4   # parallel per-probe export fan-out (1..64)
```

When `persistence.path` is set and the logs signal is enabled, event-log
records that fail to export (backend down past the SDK retry) are written
to a dead-letter queue under `<path>/logqueue/` and replayed automatically
at boot and the moment the backend recovers — so a backend outage no
longer loses event logs (linux_logs / syslog / snmp_trap / filetail /
windows_eventlog). Entity events are NOT queued: they are a state stream
re-emitted in full at every heartbeat, so an outage is caught up on the
next sweep. Past `logs_queue_max_bytes` the oldest batches are evicted
(`logs_queue_full`). Residual loss window: a hard crash (`kill -9`) while
records sit in the in-memory batch never handed to the exporter — the
graceful shutdown flush covers normal stops.

Defaults if a key is omitted: `max_store_size` 50000, `max_active_series_per_probe`
10000, `memory_limit` 200/400 MiB @ 5s, `persistence` **off**, `max_concurrent_exports` 4.

## Degradation behaviour

The checks fire in this order on every insert (cheapest first):

1. **Memory hard limit** → all inserts dropped, `runtime.GC()` forced (`memory_hard_limit`).
2. **Memory soft limit** → new series dropped, existing series keep updating (`memory_soft_limit`).
3. **Per-probe budget** → new series for that probe dropped (`probe_cardinality`).
4. **Global cap** → new series dropped (`store_cap`).

In every case the agent **logs a warning, increments a counter, and keeps
running** — existing series continue to flow. Nothing crashes, nothing
leaks. (Validated end-to-end: a soft limit below the baseline heap refuses
all new series cleanly; a per-probe budget of 3 caps the store at 3 series
per probe with the rest counted under `probe_cardinality`.)

## Tuning by scenario

### Unstable / intermittent backend connectivity

Enable the persistent checkpoint so a restart during an outage does not
lose the buffered series:

```yaml
persistence:
  path: "/var/lib/senhub-agent/otlp-checkpoint"
  interval: 15s
```

The LWW store is snapshotted to `<path>/snapshot.json` (atomic
`.tmp`+rename) every `interval`, and restored at boot **before the first
export**. On reconnect the restored series are exported — no loss.
(Validated: with the backend down, 33 series were checkpointed; after an
agent restart they were restored from disk and exported once the backend
came back.) `path` is a **directory**, created if missing, and must be
writable by the user the agent runs as (see the path table in
[OTLP-OBSERVABILITY](./OTLP-OBSERVABILITY.md#cross-os-notes)).

### High-cardinality fleet (multi-instance Citrix / NetScaler / DB)

A single probe monitoring many instances can emit thousands of series.
Cap per probe so one noisy probe cannot starve the others or blow the
global cap:

```yaml
max_active_series_per_probe: 5000
max_store_size: 50000
```

Watch `dropped_by_reason.probe_cardinality`: a sustained non-zero rate
means a probe is over budget — raise the budget deliberately or narrow
the probe's filters.

### Memory-constrained host

Lower the limiter so the agent sheds new series before it pressures the
host:

```yaml
memory_limit:
  soft_mib: 100
  hard_mib: 200
  interval: 5s
```

Soft is the graceful stage (existing series keep flowing); hard is the
emergency brake (everything dropped + GC) and should sit comfortably
above steady-state heap so it only triggers under genuine runaway.

### Large fleet / high throughput

Raise `max_concurrent_exports` so a big snapshot is split per probe and
exported in parallel over the shared gRPC connection:

```yaml
max_concurrent_exports: 8
```

Below ~100 records the strategy stays on the single-batch path; above it,
the snapshot fans out by `probe_name`, bounded by this semaphore.

## Observability

Every mechanism is visible at `/api/<agent-key>/info/otlp`, via
`agent status --otlp`, and on the web dashboard's OTLP card:

- `store.size` — current distinct series held.
- `pipeline.dropped_by_reason` — per-reason drop counters (the table above).
- `checkpoint.restored_entries` / `last_save_age_seconds` / `errors_total`.
- `logs.queue.records` / `logs.queue.bytes` — current depth of the logs
  dead-letter queue (gauges); `logs.queued_total` / `logs.replayed_total`
  — cumulative records persisted vs re-emitted. A queue depth that never
  drains means the backend is still unreachable for the logs signal.
- `failover.active_endpoint_index` (0 = primary) / `failover.switches_total`
  — endpoint failover state. A non-zero index means the agent is on a
  standby ingress; a rising switch count means the primary is flapping.

Full field reference and suggested alerts: [OTLP Pipeline
Observability](./OTLP-OBSERVABILITY.md).

## Related

- [OTLP Pipeline Observability](./OTLP-OBSERVABILITY.md) — reading the counters.
- [OTLP user guide](../user-guide/content/docs/otlp/_index.md) — the push pipeline.
- [Logging](./LOGGING.md) — where the warnings land.
