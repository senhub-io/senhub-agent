<img src="https://api.iconify.design/mdi/cog-outline.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Process Monitor

The `process` probe monitors the local process table, reporting per-process
CPU utilization, memory usage, thread count, open file descriptors and uptime.
An optional aggregate metric rolls up counts across processes sharing the same
name. Equivalent to PRTG's "Top Processes" sensor.

## Quick start

```yaml
probes:
  - name: process
    type: process
```

Monitors all processes by default. Add a `filter` block to narrow the scope.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `filter.by_name` | — | Regex pattern to restrict monitoring to matching process names |
| `filter.by_user` | — | Restrict to processes owned by this OS user |
| `filter.top_n` | — | Keep only the N processes with highest CPU utilization |
| `aggregate.enabled` | `true` | Emit an additional rolled-up count per distinct process name |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `process.cpu.utilization` | 1 | CPU utilization ratio (0–1) per process, tagged with `process.name` / `process.pid` |
| `process.memory.physical_usage` | By | Resident set size (physical memory) per process |
| `process.memory.virtual_usage` | By | Virtual memory size per process |
| `process.threads` | {thread} | Thread count per process |
| `process.open_file_descriptors` | {fd} | Open file descriptors (Linux only) |
| `process.uptime` | s | Seconds since the process started |
| `process.count` | {process} | Aggregate process count per name (when `aggregate.enabled: true`) |

## Operational notes

- On Linux, the probe reads from `/proc`. Root privilege is required only if monitoring processes owned by other users.
- `process.open_file_descriptors` is Linux-only; not emitted on Windows.
- `filter.top_n` is applied after all other filters. It is useful for "monitor the 5 most CPU-hungry processes" scenarios.
