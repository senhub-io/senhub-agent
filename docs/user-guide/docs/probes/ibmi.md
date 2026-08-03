<img src="https://cdn.simpleicons.org/ibm" alt="" class="probe-page-logo probe-page-logo-si">

!!! warning
    **License: Pro** - Requires a Pro or Enterprise license.

# Overview

The IBM i probe monitors IBM i / Power Systems partitions (formerly OS/400, AS/400) through the Db2 for i SQL services (`QSYS2`, `SYSTOOLS`, `TABLE()` functions). It provides a broad partition health picture — CPU and memory pools, ASP and disk storage, jobs and subsystems, job/output queues, spool, database tables and index advisories, journals, network listeners, HTTP servers, hardware resources, user profiles, system values and compliance. One probe instance monitors one partition; add more instances for additional LPARs.

**Collected data:**

- System CPU utilization, configured CPU count and current processing capacity
- Main storage, memory pools (size, threads, ineligible) and ASP / system ASP utilization, capacity and threshold
- Disk units: busy, space used, read/write throughput and operations, capacity and availability
- Jobs: totals, active, by status, by subsystem, plus per-job detail (top-N by CPU) — CPU, temp storage, disk I/O, page faults, threads, priority
- Job queues and scheduled jobs (active/held/released/scheduled depth, last-run age)
- Subsystems (active jobs), output queues and spooled files (counts, oldest age)
- User storage by profile (used, quota, ratio, over-threshold)
- Database: table row counts and activity, missing-index advisories, journals and journal receivers (size, remote lag)
- Network: TCP connections by state, listeners, interfaces, HTTP server threads and responses
- Hardware resources (operational vs non-operational), user profiles, system values (QSECURITY, QAUDLVL)
- Compliance: PTF group levels, watch sessions, licensed-product usage
- Probe self-observability: per-collector success/failure counters, duration and last-success timestamp

All metrics are emitted under the `senhub.ibmi.*` namespace. Partition-level aggregates are complemented by **per-resource series** (per ASP, disk unit, job, queue, memory pool, table, journal, interface, HTTP server, …), each carrying an `ibmi.*` attribute that also acts as a filter in the Web UI Sensor Builder.

# Quick Start

## Basic Configuration

```yaml
# probes.d/40-ibmi.yaml — each file under probes.d/ is a YAML array of probes
- name: ibmi-prod
  type: ibmi
  params:
    host: "ibmi.company.com"
    user: "MONITOR"
    password: "${secret:ibmi-prod.password}"   # OS secret store; inline plaintext is auto-sealed on install
    interval: 30
    bridge_runner_dir: "/opt/senhub/ibmi-bridge"
```

The probe talks to Db2 for i over the JTOpen (JT400) toolbox. Point `bridge_runner_dir` at the directory that holds the bundled `Jt400Runner.class` and `jt400.jar`; the probe launches the bridge with the JRE on the agent host (override with `java_home` if needed). The `${secret:...}` reference resolves the password from the OS-native secret store (see [Configuration](../configuration.md)).

## Native runner (no JRE)

Deployments that don't want a JRE on the runtime path can use a GraalVM native-image build of the bridge. When `native_runner` is set, `bridge_runner_dir` and `java_home` become optional:

```yaml
# probes.d/40-ibmi.yaml
- name: ibmi-prod
  type: ibmi
  params:
    host: "ibmi.company.com"
    user: "MONITOR"
    password: "${secret:ibmi-prod.password}"
    interval: 30
    native_runner: "/opt/senhub/ibmi-bridge/jt400runner"
```

## Multiple Partitions

Monitor several LPARs with separate probe instances:

```yaml
# probes.d/40-ibmi.yaml
- name: ibmi-prod
  type: ibmi
  params:
    host: "ibmi-prod.company.com"
    user: "MONITOR"
    password: "${secret:ibmi-prod.password}"
    bridge_runner_dir: "/opt/senhub/ibmi-bridge"
    environment: "production"

- name: ibmi-qa
  type: ibmi
  params:
    host: "ibmi-qa.company.com"
    user: "MONITOR"
    password: "${secret:ibmi-qa.password}"
    bridge_runner_dir: "/opt/senhub/ibmi-bridge"
    environment: "qa"
    disabled_collectors: ["index_advisor", "table"]   # skip heavy DB queries in QA
```

# Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `host` | string | Yes | - | IBM i hostname or IP the JT400 bridge connects to |
| `user` | string | Yes | - | IBM i user profile used for the SQL session |
| `password` | string | Yes | - | User password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `bridge_runner_dir` | string | Conditional | - | Directory containing `Jt400Runner.class` and `jt400.jar`. Required unless `native_runner` is set. |
| `native_runner` | string | No | - | Path to a GraalVM native-image `jt400runner` binary. When set, the bridge runs it directly — no JRE required — and `bridge_runner_dir` / `java_home` become optional. |
| `java_home` | string | No | - | Override for `JAVA_HOME` used to launch the JT400 bridge |
| `interval` | integer | No | `30` | Collection interval in seconds |
| `query_timeout_s` | integer | No | `10` | Per-query timeout in seconds |
| `startup_timeout_s` | integer | No | `15` | Bridge startup timeout in seconds |
| `enabled_collectors` | list | No | all | Allowlist of collector names to activate. Empty = every collector runs. |
| `disabled_collectors` | list | No | - | Denylist applied on top of the enabled set — e.g. drop heavy DB queries |
| `message_queues` | list | No | `QSYSOPR` | Message queues to watch. Each entry: `library` (default `QSYS`), `name` (required), `min_severity` (default `0`). |
| `environment` | string | No | - | Deployment environment name (e.g. `production`, `qa`); rides the partition entity as `deployment.environment.name` |
| `db_instance_name` | string | No | derived | Overrides the Db2 for i entity identity (`db.instance.id`). Derived from `CURRENT SERVER` when empty. |

# Metrics Collected

All metrics carry the standard host / probe attributes. Metric families that split by resource (ASP, disk unit, job, queue, pool, table, journal, interface, …) or by state carry an `ibmi.*` OTel attribute rather than exposing separate metric names. Metrics defined with a probe-side unit of `%`, `KB`/`MB`/`GB`/`B` or `ms` are converted by the OTel mapper to a `0..1` ratio, `By` and `s` respectively; the PRTG/Nagios pull views display ratios as percentages.

## System — CPU

| Metric | Unit | Description |
|--------|------|-------------|
| `senhub.ibmi.cpu.utilization` | `1` | Percentage of CPU used over the elapsed interval (ratio; `%` in pull views) |
| `senhub.ibmi.cpu.configured` | `{cpu}` | Number of configured virtual CPUs |
| `senhub.ibmi.cpu.capacity` | `{cpu}` | Current processing capacity |

## Memory & Pools

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.memory.main_storage` | `By` | - | Main storage size |
| `senhub.ibmi.memory_pool.size` | `By` | `ibmi.pool.name` | Defined pool size |
| `senhub.ibmi.memory_pool.threads` | `{thread}` | `ibmi.pool.name` | Current threads in the pool |
| `senhub.ibmi.memory_pool.ineligible_threads` | `{thread}` | `ibmi.pool.name` | Ineligible threads waiting for activity level |

## Storage — ASP, disk & user storage

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.asp.system.utilization` | `1` | - | System ASP used (ratio) |
| `senhub.ibmi.asp.utilization` | `1` | `ibmi.asp.number` | Per-ASP used (ratio) |
| `senhub.ibmi.asp.capacity` | `By` | `ibmi.asp.number` | Per-ASP total capacity |
| `senhub.ibmi.asp.threshold` | `1` | `ibmi.asp.number` | Per-ASP storage threshold (ratio) |
| `senhub.ibmi.disk.utilization` | `1` | `ibmi.disk.unit` | Disk unit busy (ratio) |
| `senhub.ibmi.disk.space.utilization` | `1` | `ibmi.disk.unit` | Disk unit space used (ratio) |
| `senhub.ibmi.disk.capacity` | `By` | `ibmi.disk.unit` | Disk unit capacity |
| `senhub.ibmi.disk.available` | `By` | `ibmi.disk.unit` | Disk unit available space |
| `senhub.ibmi.disk.read` / `.write` | `By` | `ibmi.disk.unit` | Bytes read / written (counter) |
| `senhub.ibmi.disk.operations` | `{operation}` | `ibmi.disk.unit` | Read/write requests, split by `disk.io.direction` (counter) |
| `senhub.ibmi.disk.units` | `{unit}` | - | Total disk units |
| `senhub.ibmi.user_storage.used` | `By` | `ibmi.user.name` | Storage used per user profile |
| `senhub.ibmi.user_storage.quota` | `By` | `ibmi.user.name` | Storage quota per user profile |
| `senhub.ibmi.user_storage.utilization` | `1` | `ibmi.user.name` | Quota usage ratio per user profile |
| `senhub.ibmi.user_storage.over_threshold` | `{user}` | - | Users over 80% of their storage quota |

## Jobs — aggregate & per-job

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.jobs.total` | `{job}` | - | Total jobs on the partition |
| `senhub.ibmi.jobs.active` | `{job}` | - | Active jobs |
| `senhub.ibmi.jobs.by_status` | `{job}` | `ibmi.job.type`, `ibmi.job.status` | Job count split by type and status |
| `senhub.ibmi.jobs.by_subsystem` | `{job}` | `ibmi.subsystem` | Job count per subsystem |
| `senhub.ibmi.jobs.topn_cap_hit` | `1` | - | `1` when the active-job SQL hit the row cap (counts may undercount) |
| `senhub.ibmi.job.cpu.utilization` | `1` | `ibmi.job.name` | Per-job CPU (ratio), top-N by elapsed CPU |
| `senhub.ibmi.job.cpu.elapsed_time` | `s` | `ibmi.job.name` | Per-job elapsed CPU time |
| `senhub.ibmi.job.cpu.cumulative_time` | `s` | `ibmi.job.name` | Per-job cumulative CPU time (counter) |
| `senhub.ibmi.job.cpu.delta_time` / `.rate` | `s` / `1` | `ibmi.job.name` | Per-job CPU delta and per-second rate |
| `senhub.ibmi.job.temp_storage` | `By` | `ibmi.job.name` | Per-job temporary storage |
| `senhub.ibmi.job.disk.io` / `.elapsed_io` | `{operation}` | `ibmi.job.name` | Per-job total / elapsed disk I/O |
| `senhub.ibmi.job.page_faults` | `{fault}` | `ibmi.job.name` | Per-job page faults |
| `senhub.ibmi.job.threads` | `{thread}` | `ibmi.job.name` | Per-job thread count |
| `senhub.ibmi.job.priority` | `1` | `ibmi.job.name` | Per-job run priority |

## Job queues & scheduled jobs

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.job_queue.active` | `{job}` | `ibmi.queue.name` | Active jobs in the queue |
| `senhub.ibmi.job_queue.held` | `{job}` | `ibmi.queue.name` | Held jobs |
| `senhub.ibmi.job_queue.released` | `{job}` | `ibmi.queue.name` | Released jobs |
| `senhub.ibmi.job_queue.scheduled` | `{job}` | `ibmi.queue.name` | Scheduled jobs |
| `senhub.ibmi.job_queue.depth` | `{job}` | `ibmi.queue.name` | Total jobs in the queue |
| `senhub.ibmi.job_queue.nonempty` | `{queue}` | - | Number of non-empty job queues |
| `senhub.ibmi.scheduled_job.count` | `{job}` | - | Scheduled jobs total |
| `senhub.ibmi.scheduled_job.last_run_age` | `s` | `ibmi.job.name` | Age since a scheduled job last ran |

## Subsystems, output queues & spool

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.subsystem.active_jobs` | `{job}` | `ibmi.subsystem` | Active jobs per subsystem |
| `senhub.ibmi.output_queue.files` | `{file}` | `ibmi.queue.name` | Spool files per output queue |
| `senhub.ibmi.output_queue.spooled_files` | `{file}` | - | Total spooled files across output queues |
| `senhub.ibmi.spooled_file.count` | `{file}` | - | Spooled files total |
| `senhub.ibmi.spooled_file.oldest_age` | `s` | - | Age of the oldest spooled file |

## Database — tables, index advisor, journals

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.table.rows` | `{row}` | `ibmi.table.schema`, `ibmi.table.name` | Row count |
| `senhub.ibmi.table.logical_reads` | `{read}` | `ibmi.table.schema`, `ibmi.table.name` | Logical reads (counter) |
| `senhub.ibmi.table.updates` | `{update}` | `ibmi.table.schema`, `ibmi.table.name` | Updates (counter) |
| `senhub.ibmi.table.deleted_rows` | `{row}` | `ibmi.table.schema`, `ibmi.table.name` | Deleted (not-yet-reorganized) rows |
| `senhub.ibmi.index_advisor.times_advised` | `{advisory}` | `ibmi.table.name` | Missing-index recommendation hit count (counter) |
| `senhub.ibmi.index_advisor.mti_used` | `{use}` | `ibmi.table.name` | Maintained temporary index uses (counter) |
| `senhub.ibmi.index_advisor.avg_query_estimate` | `s` | `ibmi.table.name` | Average estimated query time |
| `senhub.ibmi.index_advisor.advised_indexes` | `{index}` | - | Total advised indexes |
| `senhub.ibmi.index_advisor.recent_advisories` | `{advisory}` | - | Advisories seen in the last hour |
| `senhub.ibmi.journal.active` | `1` | `ibmi.journal.name` | Journal active state |
| `senhub.ibmi.journal.receivers_size` | `By` | `ibmi.journal.name` | Total attached-receiver size |
| `senhub.ibmi.journal.remote_lag` | `s` | `ibmi.journal.name` | Estimated remote-journal lag |
| `senhub.ibmi.journal_receiver.size` | `By` | `ibmi.receiver.name` | Journal receiver size |
| `senhub.ibmi.journal_receiver.attached` | `{receiver}` | - | Attached receivers total |

## Network — TCP, listeners, interfaces, HTTP

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.tcp.connections.established` | `{connection}` | - | Established TCP connections |
| `senhub.ibmi.netstat.connections` | `{connection}` | - | Total TCP connections |
| `senhub.ibmi.netstat.connections_by_state` | `{connection}` | `ibmi.tcp.state` | Connections split by TCP state |
| `senhub.ibmi.netstat.listener.up` | `1` | `ibmi.net.local_port` | Listener up (per port, `network.transport`) |
| `senhub.ibmi.netstat.listener.jobs` | `{job}` | `ibmi.net.local_port` | Jobs bound to a listener |
| `senhub.ibmi.netstat.listeners` | `{listener}` | - | Listeners total |
| `senhub.ibmi.netstat.interface.up` | `1` | `ibmi.net.address` | Interface up |
| `senhub.ibmi.netstat.interface.mtu` | `By` | `ibmi.net.address` | Interface MTU |
| `senhub.ibmi.http_server.threads.active` / `.idle` | `{thread}` | `ibmi.http.server_name` | HTTP server active / idle threads |
| `senhub.ibmi.http_server.responses` | `{response}` | `ibmi.http.server_name` | HTTP responses served (counter) |

## Hardware

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.hardware.count` | `{resource}` | `ibmi.hardware.category`, `ibmi.hardware.status` | Hardware resource count by category and status |
| `senhub.ibmi.hardware.total` | `{resource}` | - | Total hardware resources |
| `senhub.ibmi.hardware.non_operational` | `{resource}` | - | Resources in any non-OPERATIONAL status |

## Security & compliance

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.user_profile.count` | `{user}` | - | User profiles total |
| `senhub.ibmi.user_profile.by_status` | `{user}` | `ibmi.user.status` | User profiles by status (enabled / disabled) |
| `senhub.ibmi.user_profile.by_class` | `{user}` | `ibmi.user.class` | User profiles by class (*SECOFR, *USER, …) |
| `senhub.ibmi.user_profile.failed_signons` | `{user}` | - | Profiles with failed sign-on attempts |
| `senhub.ibmi.sysval.security_level` | `1` | - | QSECURITY system value |
| `senhub.ibmi.sysval.audit_level` | `1` | - | QAUDLVL system value |
| `senhub.ibmi.library_list.position` | `1` | `ibmi.library.name` | Library position in the list, by type |
| `senhub.ibmi.license.licensed_users` | `{user}` | `ibmi.license.product_id` | Licensed users per product / feature |
| `senhub.ibmi.license.usage_limit` | `1` | `ibmi.license.product_id` | License usage limit per product / feature |
| `senhub.ibmi.ptf_group.installed` | `1` | `ibmi.ptf.group` | PTF group installed state |
| `senhub.ibmi.ptf_group.level` | `1` | `ibmi.ptf.group` | Installed PTF group level |
| `senhub.ibmi.watch.session_active` | `1` | `ibmi.watch.session_id` | Watch session active state |

## Probe self-observability

| Metric | Unit | Resource attribute | Description |
|--------|------|--------------------|-------------|
| `senhub.ibmi.collector.success` | `{collection}` | `ibmi.collector` | Successful collections per collector (counter) |
| `senhub.ibmi.collector.failure` | `{collection}` | `ibmi.collector` | Failed collections per collector (counter) |
| `senhub.ibmi.collector.last_duration` | `s` | `ibmi.collector` | Duration of the last collection |
| `senhub.ibmi.collector.last_success_timestamp` | `s` | `ibmi.collector` | Unix timestamp of the last successful collection |

!!! note "Event conduits"
    The `message_queue` (QSYSOPR), `history_log` (QHST), `audit_journal` (QAUDJRN)
    and `msgw_job` (message-wait jobs) collectors relay operational events rather
    than numeric metrics. They are not exported to OTLP/Prometheus; a future
    release will export them as OTLP logs.

# Requirements

- **Db2 for i SQL services** reachable from the agent host over the JT400 (JTOpen) toolbox — the database host server must be started (`STRHOSTSVR SERVER(*DATABASE)`).
- An **IBM i user profile** for the probe with read access to the SQL services used (system, jobs, storage, database, network and message catalogs under `QSYS2` / `SYSTOOLS`). A profile with `*USE` authority to those services is sufficient; `*ALLOBJ` is not required. Give it a non-expiring password and no interactive display sessions.
- The **JT400 bridge**: either the bundled `Jt400Runner.class` + `jt400.jar` under `bridge_runner_dir` with a JRE on the agent host, or a GraalVM native-image `jt400runner` binary referenced via `native_runner`.
- Network path from the agent host to the partition (default database host-server port `8471`, plus the port-mapper on `449`).

# Outputs

IBM i metrics are available through every configured output — OTLP, Prometheus, and the pull formats (PRTG, Nagios, Web UI). For PRTG and Nagios, query the probe by its configured `name`:

```bash
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/ibmi-prod"
curl "http://localhost:8080/api/{agentkey}/nagios/metrics/ibmi-prod"
```
