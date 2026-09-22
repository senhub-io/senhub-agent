<img src="../../assets/probe-logos/process.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Process Monitor

The `process` probe monitors the local process table, reporting per-process
CPU utilization, memory usage, thread count, open file descriptors and uptime,
plus a roll-up over the processes sharing a name. Equivalent to PRTG's "Top
Processes" sensor.

!!! important "Name what you want to watch"
    Without a `filter`, the probe reports the **roll-up only**: the count, the
    CPU and the memory of each process name. The per-process detail appears
    when you narrow the scope with `by_name`, `by_user` or `top_n`.

    That is deliberate. A process id is part of the identity of a per-process
    series, so an unfiltered view creates a new set of series every time any
    program starts, and they are never fed again. Measured on a machine with
    837 processes, the unfiltered per-process view produced 4596 series and
    grew by about 50 a minute, indefinitely. The roll-up is stable, because a
    process name is.

## Quick start

```yaml
# probes.d/10-process.yaml — each file under probes.d/ is a YAML array of probes
- name: process
  type: process
```

Reports the per-name roll-up for every process by default. Add a `filter` block to narrow the scope and get the per-process detail.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `interval` | No | `30` | Seconds between collections |
| `filter` | No | - | Narrows the process table and turns on the per-process detail; empty reports the per-name roll-up over every process |
| `filter.by_name` | No | - | Regular expression (RE2) a process name must match. Example: `^(nginx\|php-fpm)` |
| `filter.by_user` | No | - | OS user owning the processes; empty accepts every user. Example: `www-data` |
| `filter.top_n` | No | `0` | Keep only the N processes with the highest CPU usage; 0 keeps all. A positive value also turns on the per-process detail, because it bounds how many processes carry it |
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
| `senhub.system.kernel.max_files` | {file} | Ceiling the kernel puts on open file descriptors for the whole machine, which the per-process counts are measured against (Linux only) |
| `senhub.system.kernel.max_processes` | {process} | Highest process id the kernel will assign, which is the ceiling on how many processes can exist at once (Linux only) |
| `senhub.system.users.count` | {session} | Open login sessions, one per login rather than per account: four terminals on the same account count four |

## Operational notes

- On Linux, the probe reads from `/proc`. Root privilege is required only if monitoring processes owned by other users.
- `process.open_file_descriptors` is Linux-only; not emitted on Windows.
- The three machine-wide values above belong to the machine, not to a
  process, so they carry no `process.name` and are reported once
  whatever the filter selects.
- `senhub.system.users.count` reads the login accounting file on Linux
  and the terminal services sessions on Windows, where a session whose
  client is detached still counts. A machine whose libc is musl keeps no
  such file, so the value is absent rather than zero — which is the case
  inside a container built on Alpine.
- **Naming processes gives the per-process detail, and keeps it.** A
  filter reports one series per process, identified by its process id,
  beside the roll-up over the processes sharing a name — you get both.
  A process id is not stable: when a named program restarts, its
  workers come back under new ones. On a sink that creates what it is
  sent, Zabbix among them, the items of the processes that are gone
  stay on the host and stop being fed. That is what per-process
  monitoring is, and the filter is what bounds it: only the programs
  you named can produce those series. An unfiltered view reports the
  roll-up alone for that reason.
- `filter.top_n` is applied after all other filters. It is useful for "monitor the 5 most CPU-hungry processes" scenarios, and it turns on the per-process detail because it bounds how many processes can carry it.
- The processes you name with `by_name` or `by_user` also become entities on the topology graph, with an edge to their host. A `top_n` or unfiltered view does not, because its membership changes every cycle.
