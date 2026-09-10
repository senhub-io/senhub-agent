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
# probes.d/10-process.yaml — each file under probes.d/ is a YAML array of probes
- name: process
  type: process
```

Monitors all processes by default. Add a `filter` block to narrow the scope.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `interval` | No | `30` | Seconds between collections |
| `filter` | No | - | Narrows the process table; empty watches every process |
| `filter.by_name` | No | - | Regular expression (RE2) a process name must match. Example: `^(nginx\|php-fpm)` |
| `filter.by_user` | No | - | OS user owning the processes; empty accepts every user. Example: `www-data` |
| `filter.top_n` | No | `0` | Keep only the N processes with the highest CPU usage; 0 keeps all |
| `aggregate` | No | - | Roll-up of the processes sharing a name |
| `aggregate.enabled` | No | `true` | Emit one process count per distinct process name |

<!-- schema:params:end -->

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
