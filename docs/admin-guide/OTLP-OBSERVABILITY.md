# OTLP Pipeline Observability

This page documents the agent's OTLP self-metric surfaces — the operator-side reference for every counter and gauge exposed by:

- `GET /api/{agentkey}/info/otlp` (HTTP endpoint)
- `senhub-agent status --otlp` (CLI block)
- The **OTLP Pipeline** card on the web dashboard

All three surfaces read the same in-process snapshot, so values are identical (modulo poll timing). End users get a high-level overview in the [OTLP user guide](../user-guide/content/docs/otlp/_index.md#monitoring-the-otlp-pipeline); this page is the field-level reference for operators alerting on the data.

## The `/info/otlp` endpoint

```
GET /api/{agentkey}/info/otlp
```

Authentication: agentkey in the URL, same as every other `/api/{agentkey}/info/*` route.

Response shape (truncated values):

```json
{
  "pipeline": {
    "metrics_pushed_total": 10815,
    "logs_pushed_total": 2825,
    "export_errors_total": 0,
    "dropped_total": 464,
    "dropped_by_reason": { "probe_cardinality": 464 }
  },
  "store": {
    "size": 10106,
    "log_buffer_fill_ratio": 0.0
  },
  "export_duration": {
    "last_ms": 36013.4,
    "mean_ms": 35922.7
  },
  "checkpoint": {
    "size_bytes": 4665341,
    "last_save_age_seconds": 6.25,
    "restored_entries": 10106,
    "errors_total": 0,
    "errors_by_stage": {}
  },
  "parallel": {
    "sub_batches": 6
  }
}
```

Counter maps (`dropped_by_reason`, `checkpoint.errors_by_stage`) are always returned as a JSON object — empty `{}` rather than `null` — so frontends can iterate without a guard.

## Field reference

### `pipeline`

| Field | Type | Source | Meaning |
|---|---|---|---|
| `metrics_pushed_total` | uint64 | `senhub.agent.otlp.metrics.pushed` | Number of metric data-points successfully accepted by the OTLP exporter since the agent started. Monotonic. |
| `logs_pushed_total` | uint64 | `senhub.agent.otlp.logs.pushed` | Same, for log records. |
| `export_errors_total` | uint64 | `senhub.agent.otlp.export.errors` | Number of failed OTLP export calls (after retry exhaustion). Signal independent — both metrics and logs failures count here. |
| `dropped_total` | uint64 | sum of `dropped_by_reason` | Aggregate count of OTLP data-points discarded **before** the export call. |
| `dropped_by_reason` | map[string]uint64 | `senhub.agent.otlp.dropped{reason=…}` | Per-reason breakdown. Reason set is a stable, small enum: |

| Reason | When the counter advances |
|---|---|
| `store_cap` | The strategy-local LWW store hit its global cap (`max_store_size`, default 50 000 series). The oldest entry is replaced; the displaced data-point is counted here. |
| `probe_cardinality` | A single probe tried to insert a new series while it was already at `max_active_series_per_probe` (default 10 000). |
| `memory_soft_limit` | `runtime.MemStats.HeapAlloc` crossed `memory_limit.soft_mib` (default 200 MiB). The exporter drops new series under soft pressure but keeps updating existing ones. |
| `memory_hard_limit` | Same, for `memory_limit.hard_mib` (default 400 MiB). At this stage **all** new and existing inserts are dropped until pressure recedes. |

### `store`

| Field | Type | Source | Meaning |
|---|---|---|---|
| `size` | int64 | `senhub.agent.otlp.store_size` | Number of distinct active series in the strategy-local LWW store at the last push tick. |
| `log_buffer_fill_ratio` | float | `senhub.agent.otlp.buffer.fill_ratio` | Worst-case fill ratio across all active log subscribers (0..1). `0` when there is no subscriber. |

### `export_duration`

| Field | Type | Source | Meaning |
|---|---|---|---|
| `last_ms` | float | `senhub.agent.otlp.export.duration{window=last}` | Wall-clock duration of the most recent successful metric export, in milliseconds. |
| `mean_ms` | float | `senhub.agent.otlp.export.duration{window=mean}` | Running mean across every successful export since boot. |

Both are reset to `0` until the first successful export.

### `checkpoint`

The checkpoint section reflects the persistent on-disk LWW snapshot enabled via `otlp.persistence` in the strategy YAML.

| Field | Type | Source | Meaning |
|---|---|---|---|
| `size_bytes` | int64 | `senhub.agent.otlp.checkpoint.size` | Size of the most recent checkpoint file in bytes. `0` until the first save. |
| `last_save_age_seconds` | float | `senhub.agent.otlp.checkpoint.last_save_age` | Wall-clock age of the most recent successful save. `0` when persistence is disabled or no save has occurred yet. |
| `restored_entries` | int64 | `senhub.agent.otlp.checkpoint.restored_entries` | Number of series restored at boot from a previous checkpoint. `0` for a fresh start (no checkpoint file, or restore failed). |
| `errors_total` | uint64 | sum of `errors_by_stage` | Aggregate count of checkpoint operation failures. |
| `errors_by_stage` | map[string]uint64 | `senhub.agent.otlp.checkpoint.errors{stage=…}` | Per-stage breakdown. Stages are stable enums: |

| Stage | Meaning |
|---|---|
| `read` | Could not open the checkpoint file for reading. |
| `parse` | JSON decoder error on the checkpoint envelope. |
| `version_mismatch` | The checkpoint was written by an incompatible format version. |
| `mkdir` | Could not create the checkpoint directory. |
| `create_tmp` | Could not create the temporary `.tmp` file. |
| `encode` | JSON encoder error while writing entries. |
| `fsync` | `fsync(2)` syscall failed on the `.tmp` file. |
| `close` | `close(2)` syscall failed. |
| `rename` | The atomic `rename(2)` from `.tmp` to the final path failed. |

### `parallel`

| Field | Type | Source | Meaning |
|---|---|---|---|
| `sub_batches` | int32 | `senhub.agent.otlp.parallel.sub_batches` | Number of sub-batches the last push fanned out across. `1` when the single-batch code path was taken (batch below the `SplitBatchThreshold` of 100 records, or `max_concurrent_exports=1`). `> 1` means parallel per-probe fan-out fired. |

## Alerting

Reasonable starter alerts (express them in your preferred alerting tool — these are just relations between fields):

| Symptom | Condition | Severity |
|---|---|---|
| Sink is down or unreachable | `pipeline.export_errors_total` rate > 0 sustained for 5 min | high |
| Cardinality cap is hitting | `pipeline.dropped_by_reason.probe_cardinality` rate > 0 sustained for 10 min | medium |
| Memory pressure | `pipeline.dropped_by_reason.memory_hard_limit` rate > 0 | high |
| Export starting to lag | `export_duration.mean_ms` > 60 % of the configured `timeout` | medium |
| Persistence broken | `checkpoint.errors_total` rate > 0 | medium |
| Restart restored nothing | `checkpoint.restored_entries == 0` while `checkpoint.size_bytes > 0` was observed before reboot | medium (data loss across restart) |

The same fields are exposed under the Prometheus bridge as `senhub_agent_otlp_*` — pick whichever query path your alerting stack already understands.

## Cross-OS notes

The endpoint, the CLI flag and the dashboard card behave identically on Windows, Linux and macOS — they read in-process state, not OS-specific paths. The only OS-shaped concern is the checkpoint file path under `otlp.persistence.path`:

| OS | Recommended checkpoint path |
|---|---|
| Linux | `/var/lib/senhub-agent/otlp-checkpoint` |
| Windows | `C:\SenHub\state\otlp-checkpoint` |
| macOS | `/usr/local/senhub/state/otlp-checkpoint` |

The directory must be writable by the user the agent runs as. On systemd, that is typically the `senhub-agent` system user — make sure `/var/lib/senhub-agent/` is owned by it.

## Related

- [HTTP Strategy](./HTTP-STRATEGY.md) — how the HTTP endpoint that serves `/info/otlp` is wired up.
- [OTLP user guide](../user-guide/content/docs/otlp/_index.md) — end-user view of the OTLP push pipeline.
- [Logging](./LOGGING.md) — log levels and where to look when these counters indicate something is wrong.
