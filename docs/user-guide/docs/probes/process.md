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
    when you narrow the scope with `by_name` or `by_user`. `top_n` ranks process names and reports the roll-up of the top.

    That is deliberate. A process id is part of the identity of a per-process
    series, so an unfiltered view creates a new set of series every time any
    program starts, and they are never fed again. Measured on a machine with
    837 processes, the unfiltered per-process view produced 4596 series and
    grew by about 50 a minute, indefinitely. The roll-up is stable, because a
    process name is. The one exception is Linux's kernel workqueue threads,
    which rename themselves as they work (`kworker/1:1-ata_sff`, then another
    name under the same process id): they are reported as `kworker`, in the
    roll-up and in the per-process detail. A `by_name` filter still matches
    the real name.

    PRTG receives the roll-up only. It keeps every channel it has ever seen
    on a sensor, and the per-process channels are keyed on the process id,
    so each restart of a program would leave one more dead channel there.
    The per-process detail stays on the Prometheus, OTLP, Nagios and Zabbix
    outputs.

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
<!-- sha256:96d2ffc12b924ab3d10200cf03760cbce7a590a4b16cb2bd251085fda2a5617b -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `interval` | No | `30` | Seconds between collections |
| `filter` | No | - | Narrows the process table; by_name and by_user turn on the per-process detail; empty reports the per-name roll-up over every process |
| `filter.by_name` | No | - | Regular expression (RE2) a process name must match. Example: `^(nginx\|php-fpm)` |
| `filter.by_user` | No | - | OS user owning the processes; empty accepts every user. Example: `www-data` |
| `filter.top_n` | No | `0` | Keep only the N processes ranking highest on top_by; 0 keeps all. Ranks process names, summing the measure over processes sharing a name, and reports the per-name roll-up of the N highest; per-process series need detail.per_pid |
| `filter.top_by` | No | `cpu` | What top_n ranks on: cpu, or memory (resident set, the working set on Windows). Example: `memory` |
| `detail` | No | - | Per-process series under top_n |
| `detail.per_pid` | No | `false` | Under top_n, emit per-process series tagged with the process id and creation time. The series set grows with every process that enters the top: only for short observations |
| `aggregate` | No | - | Roll-up of the processes sharing a name |
| `aggregate.enabled` | No | `true` | Emit one process count per distinct process name |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `process.cpu.utilization` | 1 | CPU utilization ratio (0–1) per process, tagged with `process.name` / `process.pid` |
| `process.memory.usage` | By | Resident set size (physical memory) per process |
| `process.memory.virtual_memory_usage` | By | Virtual memory size per process |
| `process.threads` | {thread} | Thread count per process |
| `process.open_file_descriptors` | {fd} | Open file descriptors (Linux only) |
| `process.uptime` | s | Seconds since the process started |
| `process.count` | {process} | Aggregate process count per name (when `aggregate.enabled: true`) |
| `senhub.system.kernel.max_files` | {file} | Ceiling the kernel puts on open file descriptors for the whole machine, which the per-process counts are measured against (Linux only) |
| `senhub.system.kernel.max_processes` | {process} | Highest process id the kernel will assign, which is the ceiling on how many processes can exist at once (Linux only) |
| `senhub.system.users.count` | {session} | Open login sessions, one per login rather than per account: four terminals on the same account count four |
| `senhub.system.kernel.open_files` | {file} | File handles the kernel has allocated for the whole machine, read from `/proc/sys/fs/file-nr` (Linux only) |
| `senhub.system.passwd.checksum` | 1 | CRC32 of `/etc/passwd`, as a number that changes whenever the file changes (Linux only) |
| `senhub.system.passwd.modified_timestamp` | s | Unix time of the last modification of `/etc/passwd` (Linux only) |

## Operational notes

- **Detecting a change of `/etc/passwd`.** The checksum is a CRC32 of
  the file content, sent as a number so that every output carries it as
  a plain gauge. It is not a cryptographic hash and does not say what
  changed; it says that something did. The Zabbix template generated for
  this probe on Linux carries a warning trigger, `Passwd Checksum has
  changed`, with the expression `change(/<template>/<item key>)<>0`: it
  raises when the value differs from the previous one and closes by
  itself at the next poll that repeats the new value. On other outputs,
  alert on a change of the value.
- `senhub.system.kernel.max_processes` is read from
  `/proc/sys/kernel/pid_max`, the highest process id the kernel assigns,
  not from `threads-max`.

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
- `filter.top_n` is applied after all other filters. It ranks process names, summing `top_by` over the processes that share a name, and reports the per-name roll-up (`process.count`, `process.group.*`) of the N highest. The kept names change from cycle to cycle, but each series is identified by its name alone, so the series count stays bounded. Set `filter.top_by: memory` to rank on resident memory instead (the working set on Windows): it names the process behind a low-memory condition.
- `detail.per_pid: true` restores per-process series under `top_n`, tagged with `process.pid` and `process.creation.time`. Leave it off for anything that runs for long: every process entering the top creates series that are never reused. On a Windows host following about 24 processes, about 125 series were active at any instant while about 65 000 distinct series accumulated over 7 days. `agent config check` warns when the two are combined.
- The processes you name with `by_name` or `by_user` also become entities on the topology graph, with an edge to their host. A `top_n` or unfiltered view does not, because its membership changes every cycle.

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
| `process.cpu.utilization` | `process.cpu.utilization` | Process {process.name} ({process.pid}) CPU | % | CPU utilization percentage (0-100) for the process averaged over the last collection interval |
| `process.memory.usage` | `process.memory.usage` | Process {process.name} ({process.pid}) RSS | bytes | Resident set size (RSS) of the process in bytes |
| `process.memory.virtual_memory_usage` | `process.memory.virtual_memory_usage` | Process {process.name} ({process.pid}) VMS | bytes | Virtual memory size (VMS) of the process in bytes |
| `process.threads` | `process.threads` | Process {process.name} ({process.pid}) Threads | # | Number of threads used by the process |
| `process.open_file_descriptors` | `process.open_file_descriptors` | Process {process.name} ({process.pid}) FDs | # | Number of open file descriptors (Linux only) |
| `process.uptime` | `process.uptime` | Process {process.name} ({process.pid}) Uptime | s | Seconds elapsed since the process was created |
| `senhub.process.group.cpu.utilization` | `process.group.cpu.utilization` | Processes {process.name} CPU | % | CPU utilization summed over the processes sharing this name; reported whatever the filter, and the only CPU series when no filter narrows the process table |
| `senhub.process.group.memory.usage` | `process.group.memory.usage` | Processes {process.name} Memory | B | Resident memory summed over the processes sharing this name; reported whatever the filter, and the only memory series when no filter narrows the process table |
| `senhub.system.kernel.max_files` | `kernel_max_files` | Kernel Max Open Files | # | Ceiling the kernel puts on open file descriptors for the whole machine; the per-process counts above are measured against it |
| `senhub.system.kernel.max_processes` | `kernel_max_processes` | Kernel Max Processes | # | Highest process id the kernel will assign, which is the ceiling on how many processes can exist at once |
| `senhub.system.users.count` | `users_logged_in` | Logged-in Users | # | Open login sessions on the machine, one per login rather than per account: four terminals opened on the same account count four. Read from the login accounting file on Linux and from the terminal services sessions on Windows, where a session whose client is detached still counts |
| `senhub.system.kernel.open_files` | `kernel_open_files` | Kernel Open Files | # | File handles the kernel has allocated for the whole machine, the first field of /proc/sys/fs/file-nr; compare with the ceiling in Kernel Max Open Files |
| `senhub.system.passwd.checksum` | `passwd_checksum` | Passwd Checksum | # | CRC32 of the content of /etc/passwd, as a number. It changes whenever the file changes, so a server that alerts on a change of value sees an account added, removed or edited; it is not a cryptographic hash and does not say what changed |
| `senhub.system.passwd.modified_timestamp` | `passwd_modified_time` | Passwd Modified Time | s | Unix time of the last modification of /etc/passwd |
| `process.count` | `process.count` | Process {process.name} Count | # | Number of running instances of this process name |
| `senhub.probe.check.result` | `senhub.probe.check.result` | Check Result | # | Why the last check failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |

<!-- schema:metrics:end -->
