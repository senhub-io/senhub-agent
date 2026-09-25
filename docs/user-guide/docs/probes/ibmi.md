<img src="../../assets/probe-logos/ibmi.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

All metrics are emitted under the `senhub.ibmi.*` namespace. Partition-level aggregates are complemented by **per-resource series** (per ASP, disk unit, job, queue, memory pool, table, journal, interface, HTTP server, …), each carrying an `ibmi.*` attribute that also acts as a filter in the Sensor URLs tab of the console.

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
    disabled_collectors: ["index_advisor", "sys_table_stats"]   # skip heavy DB queries in QA
```

# Configuration Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:59189fb7f2342b570259f06f03730237269b6af7cca03d1b6a84655e3aa6e388 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | Yes | - | IBM i hostname or address the bridge connects to. Example: `ibmi01.example.com` |
| `user` | Yes | - | IBM i user profile of the SQL session |
| `password` | Yes | - | Password of the user profile. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `bridge_runner_dir` | In practice | - | Directory holding Jt400Runner.class and jt400.jar; required unless native_runner is set, then only the working directory. Example: `/opt/senhub-agent/jt400` |
| `native_runner` | No | - | GraalVM native-image jt400runner binary run instead of a JVM; bridge_runner_dir and java_home become optional. Example: `/opt/senhub-agent/jt400runner` |
| `java_home` | No | - | JAVA_HOME used to launch the bridge; empty uses the environment |
| `interval` | No | `30` | Seconds between collections |
| `query_timeout_s` | No | `10` | Per-query timeout, in seconds |
| `startup_timeout_s` | No | `15` | Bridge startup timeout, in seconds |
| `enabled_collectors` | No | - | Collectors to run; empty runs the default set, and this is the only way to turn on audit_journal, authority_collection, ptf, ptf_group, query_supervisor, service_agent and watch_info. One of `system_status`, `asp`, `subsystem`, `memory_pool`, `output_queue`, `active_job`, `job_queue`, `scheduled_job`, `user_profile`, `system_value`, `netstat_listener`, `netstat_interface`, `netstat_connection`, `http_server`, `jvm`, `disk_status`, `sys_table_stats`, `journal_info`, `journal_receiver`, `library_list`, `license`, `media_library`, `spooled_file`, `user_storage`, `index_advisor`, `hardware_resource`, `message_queue`, `history_log`, `msgw_job`, `audit_journal`, `authority_collection`, `ptf_group`, `ptf`, `service_agent`, `watch_info`, `query_supervisor` |
| `disabled_collectors` | No | - | Collectors removed from the enabled set; unknown names are ignored. One of `system_status`, `asp`, `subsystem`, `memory_pool`, `output_queue`, `active_job`, `job_queue`, `scheduled_job`, `user_profile`, `system_value`, `netstat_listener`, `netstat_interface`, `netstat_connection`, `http_server`, `jvm`, `disk_status`, `sys_table_stats`, `journal_info`, `journal_receiver`, `library_list`, `license`, `media_library`, `spooled_file`, `user_storage`, `index_advisor`, `hardware_resource`, `message_queue`, `history_log`, `msgw_job`, `audit_journal`, `authority_collection`, `ptf_group`, `ptf`, `service_agent`, `watch_info`, `query_supervisor` |
| `message_queues` | No | - | Message queues to watch, one collector each; empty watches QSYS/QSYSOPR |
| `message_queues[].name` | Yes | - | Queue name. Example: `QSYSOPR` |
| `message_queues[].library` | No | `QSYS` | Library of the queue |
| `message_queues[].min_severity` | No | `0` | Messages below this severity are not relayed |
| `environment` | No | - | Deployment environment name carried by the partition entity. Example: `production` |
| `db_instance_name` | No | - | Identity override of the Db2 for i entity; empty derives it from the relational database name |

<!-- schema:params:end -->

## Collector names

`enabled_collectors` and `disabled_collectors` take the names below, exactly as written. A name that is not in this list is ignored without a warning, so a typo silently leaves the collector in its default state.

Run by default:

`system_status`, `asp`, `subsystem`, `memory_pool`, `output_queue`, `active_job`, `job_queue`, `scheduled_job`, `user_profile`, `system_value`, `netstat_listener`, `netstat_interface`, `netstat_connection`, `http_server`, `jvm`, `disk_status`, `sys_table_stats`, `journal_info`, `journal_receiver`, `library_list`, `license`, `media_library`, `spooled_file`, `user_storage`, `index_advisor`, `hardware_resource`, `message_queue`, `history_log`, `msgw_job`.

Off by default, turned on only by listing them in `enabled_collectors`:

`audit_journal`, `authority_collection`, `ptf_group`, `ptf`, `service_agent`, `watch_info`, `query_supervisor`.

When `enabled_collectors` is set, only the listed collectors run; the default set is not added to it.

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
| `senhub.ibmi.cpu.utilization` | `ibmi.cpu.elapsed_used_percent` | CPU Used | % | Percentage of CPU used over the elapsed interval |
| `senhub.ibmi.cpu.configured` | `ibmi.cpu.configured_count` | CPU Configured Count | # | Number of configured virtual CPUs |
| `senhub.ibmi.cpu.capacity` | `ibmi.cpu.current_capacity` | CPU Current Capacity | # | Current processing capacity |
| `senhub.ibmi.memory.main_storage` | `ibmi.memory.main_storage_kb` | Main Storage | KB | Main storage configured on the partition (MAIN_STORAGE_SIZE) |
| `senhub.ibmi.asp.system.utilization` | `ibmi.asp.system_used_percent` | System ASP Used | % | Percentage of the system ASP in use; the number that fills before the partition stops writing |
| `senhub.ibmi.asp.utilization` | `ibmi.asp.used_percent` | ASP Used ({asp_number}) | % | Percentage of this ASP in use |
| `senhub.ibmi.asp.capacity` | `ibmi.asp.total_capacity_mb` | ASP Total Capacity ({asp_number}) | MB | Total capacity of this ASP |
| `senhub.ibmi.asp.threshold` | `ibmi.asp.storage_threshold_percent` | ASP Storage Threshold ({asp_number}) | % | Threshold at which this ASP raises a storage warning, as configured — not a measurement |
| `senhub.ibmi.jobs.total` | `ibmi.jobs.total_count` | Jobs Total | # | Jobs in the system, active and inactive (TOTAL_JOBS_IN_SYSTEM) |
| `senhub.ibmi.jobs.active` | `ibmi.jobs.active_total` | Jobs Active Total | # | Active jobs returned by the scan; capped by the row limit, see ibmi.jobs.topn_cap_hit |
| `senhub.ibmi.jobs.by_status` | `ibmi.jobs.count_by_status` | Jobs {job_type} / {status} | # | Active jobs of this type in this status; the enum values come from IBM, so cardinality is bounded |
| `senhub.ibmi.jobs.by_subsystem` | `ibmi.jobs.count_by_subsystem` | Jobs in {subsystem} | # | Active jobs running in this subsystem |
| `senhub.ibmi.jobs.topn_cap_hit` | `ibmi.jobs.topn_cap_hit` | Top-N Cap Hit | # | 1 = active_job SQL hit the row cap, counts may be undercounts |
| `senhub.ibmi.job.cpu.utilization` | `ibmi.job.elapsed_cpu_percent` | CPU % — {job_name} | % | Share of CPU attributed to this job since the statistics baseline — set on the first query of the connection, not per collection (senhub-agent-enterprise#90) |
| `senhub.ibmi.job.cpu.elapsed_time` | `ibmi.job.elapsed_cpu_ms` | Elapsed CPU — {job_name} | ms | CPU time this job used since the statistics baseline; same window as elapsed_cpu_percent |
| `senhub.ibmi.job.cpu.cumulative_time` | `ibmi.job.cpu_time_ms` | Cumulative CPU — {job_name} | ms | CPU time this job has used since it started — cumulative, not an interval |
| `senhub.ibmi.job.cpu.delta_time` | `ibmi.job.cpu_time_ms_delta` | CPU Delta — {job_name} | ms | CPU time consumed between two collections, derived from cpu_time_ms |
| `senhub.ibmi.job.cpu.rate` | `ibmi.job.cpu_time_ms_rate_per_sec` | CPU Rate — {job_name} | ms/s | CPU milliseconds consumed per wallclock second — dimensionless ratio when divided by 1000 |
| `senhub.ibmi.job.temp_storage` | `ibmi.job.temp_storage_mb` | Temp Storage — {job_name} | MB | Temporary storage currently allocated to this job; a job that grows here without end is the usual sign of a leak |
| `senhub.ibmi.job.disk.io` | `ibmi.job.total_disk_io` | Total Disk I/O — {job_name} | # | Disk I/O operations this job has performed since it started |
| `senhub.ibmi.job.disk.elapsed_io` | `ibmi.job.elapsed_total_disk_io` | Disk I/O (elapsed) — {job_name} | # | Disk I/O since the statistics baseline; same window as the other elapsed values |
| `senhub.ibmi.job.page_faults` | `ibmi.job.elapsed_page_faults` | Page Faults — {job_name} | # | Page faults since the statistics baseline; sustained faults point at an undersized memory pool |
| `senhub.ibmi.job.threads` | `ibmi.job.thread_count` | Threads — {job_name} | # | Threads this job is running |
| `senhub.ibmi.job.priority` | `ibmi.job.run_priority` | Run Priority — {job_name} | # | Priority at which this job competes for the processor, 1 highest to 99 lowest — lower is more urgent |
| `senhub.ibmi.job_queue.active` | `ibmi.job_queue.active_jobs` | Active Jobs — {queue_name} | # | Jobs of this queue currently running |
| `senhub.ibmi.job_queue.held` | `ibmi.job_queue.held_jobs` | Held Jobs — {queue_name} | # | Jobs held in this queue; held work does not run until released |
| `senhub.ibmi.job_queue.released` | `ibmi.job_queue.released_jobs` | Released Jobs — {queue_name} | # | Jobs released and waiting to be dispatched from this queue |
| `senhub.ibmi.job_queue.scheduled` | `ibmi.job_queue.scheduled_jobs` | Scheduled Jobs — {queue_name} | # | Jobs in this queue waiting for their scheduled time |
| `senhub.ibmi.job_queue.depth` | `ibmi.job_queue.jobs_total` | Total Jobs — {queue_name} | # | Jobs of every state in this queue — its depth |
| `senhub.ibmi.job_queue.nonempty` | `ibmi.job_queue.nonempty_total` | Non-empty Job Queues | # | Job queues holding at least one job |
| `senhub.ibmi.scheduled_job.count` | `ibmi.scheduled_job.total` | Scheduled Jobs | # | Entries in the job scheduler |
| `senhub.ibmi.scheduled_job.last_run_age` | `ibmi.scheduled_job.last_run_age_seconds` | Scheduled Last Run Age — {job_name} | s | Time since this scheduled job last ran; the metric to alert on, an entry that stops firing keeps a healthy last status |
| - | `ibmi.msgw_job.event` | MSGW — {job_name} | s | Job in message wait; value = seconds stuck |
| `senhub.ibmi.subsystem.active_jobs` | `ibmi.subsystem.active_jobs` | Active Jobs in {subsystem} | # | Active jobs in this subsystem |
| `senhub.ibmi.memory_pool.size` | `ibmi.memory_pool.defined_size_mb` | Pool Size — {pool_name} | MB | Size configured for this memory pool |
| `senhub.ibmi.memory_pool.threads` | `ibmi.memory_pool.current_threads` | Pool Threads — {pool_name} | # | Threads currently eligible to run in this pool |
| `senhub.ibmi.memory_pool.ineligible_threads` | `ibmi.memory_pool.ineligible_threads` | Pool Ineligible — {pool_name} | # | Threads waiting for an activation slot in this pool; a rising count means the pool's activity level is too low for the work |
| `senhub.ibmi.disk.utilization` | `ibmi.disk.percent_busy` | Disk Busy — unit {unit_number} | % | Share of time this disk unit was busy since the statistics baseline |
| `senhub.ibmi.disk.read` | `ibmi.disk.data_read_bytes` | Disk Read — unit {unit_number} | B | Bytes read from this disk unit since the statistics baseline |
| `senhub.ibmi.disk.write` | `ibmi.disk.data_written_bytes` | Disk Write — unit {unit_number} | B | Bytes written to this disk unit since the statistics baseline |
| `senhub.ibmi.disk.operations` | `ibmi.disk.read_requests` | Disk Read Requests — unit {unit_number} | # | Read requests served by this disk unit since the statistics baseline |
| `senhub.ibmi.disk.operations` | `ibmi.disk.write_requests` | Disk Write Requests — unit {unit_number} | # | Write requests served by this disk unit since the statistics baseline |
| `senhub.ibmi.disk.capacity` | `ibmi.disk.capacity_bytes` | Disk Capacity — unit {unit_number} | B | Total capacity of this disk unit |
| `senhub.ibmi.disk.available` | `ibmi.disk.available_gb` | Disk Available — unit {unit_number} | GB | Space still available on this disk unit |
| `senhub.ibmi.disk.space.utilization` | `ibmi.disk.percent_used` | Disk Used — unit {unit_number} | % | Share of this disk unit in use |
| `senhub.ibmi.disk.units` | `ibmi.disk.units_total` | Disk Units | # | Disk units the partition reports |
| `senhub.ibmi.output_queue.files` | `ibmi.output_queue.files_count` | Spool Files — {queue_name} | # | Spooled files waiting in this output queue |
| `senhub.ibmi.output_queue.spooled_files` | `ibmi.output_queue.spooled_files_total` | Spooled Files Total | # | Spooled files across every output queue |
| `senhub.ibmi.spooled_file.count` | `ibmi.spooled_file.total` | Spooled Files | # | Spooled files the scan returned |
| `senhub.ibmi.spooled_file.oldest_age` | `ibmi.spooled_file.oldest_age_seconds` | Oldest Spool Age | s | Age of the oldest spooled file; output nobody collects accumulates here and consumes ASP |
| `senhub.ibmi.user_storage.used` | `ibmi.user_storage.used_kb` | User Storage — {user} | KB | Storage owned by this user profile |
| `senhub.ibmi.user_storage.quota` | `ibmi.user_storage.quota_kb` | User Quota — {user} | KB | Storage limit set on this user profile; 0 means no limit |
| `senhub.ibmi.user_storage.utilization` | `ibmi.user_storage.usage_ratio_percent` | User Storage Ratio — {user} | % | Share of this user's quota in use; absent when no quota is set |
| `senhub.ibmi.user_storage.over_threshold` | `ibmi.user_storage.users_over_80pct` | Users Over 80% Quota | # | User profiles past 80% of their storage quota |
| `senhub.ibmi.table.rows` | `ibmi.table.rows_count` | Rows — {table_schema}.{table_name} | # | Rows in this table |
| `senhub.ibmi.table.logical_reads` | `ibmi.table.logical_reads_total` | Logical Reads — {table_schema}.{table_name} | # | Logical reads against this table since the statistics were last reset |
| `senhub.ibmi.table.updates` | `ibmi.table.updates_total` | Updates — {table_schema}.{table_name} | # | Update operations against this table since the statistics were last reset |
| `senhub.ibmi.table.deleted_rows` | `ibmi.table.deleted_rows` | Deleted Rows — {table_schema}.{table_name} | # | Rows deleted but not yet reorganised; space a REORG would reclaim |
| `senhub.ibmi.index_advisor.times_advised` | `ibmi.index_advisor.times_advised` | Times Advised — {table_schema}.{table_name} | # | Missing-index recommendation hit count |
| `senhub.ibmi.index_advisor.mti_used` | `ibmi.index_advisor.mti_used` | MTI Used — {table_schema}.{table_name} | # | Times the system built a temporary index for this table instead of using a permanent one |
| `senhub.ibmi.index_advisor.avg_query_estimate` | `ibmi.index_advisor.avg_query_estimate_seconds` | Avg Query Estimate — {table_schema}.{table_name} | s | Estimated query time the advisor expects to save by creating the advised index |
| `senhub.ibmi.index_advisor.advised_indexes` | `ibmi.index_advisor.total` | Advised Indexes | # | Index recommendations open across the partition |
| `senhub.ibmi.index_advisor.recent_advisories` | `ibmi.index_advisor.recent_advisories_1h` | Index Advisories (1h) | # | Index recommendations raised in the last hour; a burst points at a query pattern that changed |
| `senhub.ibmi.journal.active` | `ibmi.journal.active` | Journal {journal} ({journal_library}) | # | 1 when this journal is active |
| `senhub.ibmi.journal.receivers_size` | `ibmi.journal.receivers_total_size_kb` | Receivers Size — {journal} | KB | Size of every receiver attached to this journal; receivers that are never detached grow without bound |
| `senhub.ibmi.journal.remote_lag` | `ibmi.journal.remote_lag_estimated_seconds` | Remote Lag (est) — {journal} | s | Estimated delay of the remote journal behind the local one — an estimate, not a value the system reports |
| `senhub.ibmi.journal_receiver.size` | `ibmi.journal_receiver.size_kb` | Receiver Size — {receiver} | KB | Size of this journal receiver |
| `senhub.ibmi.journal_receiver.attached` | `ibmi.journal_receiver.attached_total` | Attached Receivers | # | Journal receivers currently attached |
| `senhub.ibmi.tcp.connections.established` | `ibmi.tcp.connections_established` | TCP Connections Established | # | TCP connections currently established |
| `senhub.ibmi.netstat.connections` | `ibmi.netstat.connections_total` | TCP Connections Total | # | TCP connections in every state |
| `senhub.ibmi.netstat.connections_by_state` | `ibmi.netstat.connections_by_state` | TCP {tcp_state} | # | TCP connections in this state; a growing TIME-WAIT or CLOSE-WAIT count is the usual first symptom |
| `senhub.ibmi.netstat.listener.up` | `ibmi.netstat.listener_up` | Listener — port {local_port} | # | 1 when something is listening on this port |
| `senhub.ibmi.netstat.listener.jobs` | `ibmi.netstat.listener_jobs` | Listener Jobs — port {local_port} | # | Jobs listening on this port |
| `senhub.ibmi.netstat.listeners` | `ibmi.netstat.listener_total` | Listeners Total | # | Ports with a listener |
| `senhub.ibmi.netstat.interface.up` | `ibmi.netstat.interface_up` | Interface — {address} | # | 1 when this interface is active |
| `senhub.ibmi.netstat.interface.mtu` | `ibmi.netstat.interface_mtu` | Interface MTU — {address} | B | MTU configured on this interface |
| `senhub.ibmi.http_server.threads.active` | `ibmi.http_server.active_threads` | HTTP Active Threads — {server_name} | # | Threads serving requests on this HTTP server |
| `senhub.ibmi.http_server.threads.idle` | `ibmi.http_server.idle_threads` | HTTP Idle Threads — {server_name} | # | Threads idle on this HTTP server; no idle thread left means requests are queuing |
| `senhub.ibmi.http_server.responses` | `ibmi.http_server.total_responses` | HTTP Responses — {server_name} | # | Responses this HTTP server has sent since it started |
| `senhub.ibmi.hardware.count` | `ibmi.hardware_resource.count` | HW {category} ({status}) | # | Hardware resources of this category in this status |
| `senhub.ibmi.hardware.total` | `ibmi.hardware_resource.total` | Hardware Resources Total | # | Hardware resources the partition reports |
| `senhub.ibmi.hardware.non_operational` | `ibmi.hardware_resource.non_operational_total` | Non-operational HW | # | Resources in any non-OPERATIONAL status |
| `senhub.ibmi.user_profile.count` | `ibmi.user_profile.total` | User Profiles Total | # | User profiles on the partition |
| `senhub.ibmi.user_profile.by_status` | `ibmi.user_profile.count_by_status` | Users — {status} | # | User profiles in this status |
| `senhub.ibmi.user_profile.by_class` | `ibmi.user_profile.count_by_class` | Users — class {user_class} | # | User profiles of this class; the count of *SECOFR is the one worth watching |
| `senhub.ibmi.user_profile.failed_signons` | `ibmi.user_profile.failed_signon_total` | Users with Failed Signons | # | User profiles carrying at least one failed sign-on attempt |
| `senhub.ibmi.sysval.security_level` | `ibmi.sysval.security_level` | QSECURITY | # | QSECURITY system value: 20 no resource security, 30 resource security, 40 and 50 add integrity protection |
| `senhub.ibmi.sysval.audit_level` | `ibmi.sysval.audit_level` | QAUDLVL | # | QAUDLVL as a numeric code; 0 means auditing is off |
| `senhub.ibmi.library_list.position` | `ibmi.library_list.position` | Library {library} ({type}) | # | Position of this library in the list, in order of search |
| `senhub.ibmi.license.licensed_users` | `ibmi.license.licensed_user_count` | Licensed Users — {product_id} | # | Users currently licensed for this product |
| `senhub.ibmi.license.usage_limit` | `ibmi.license.usage_limit` | License Usage Limit — {product_id} | # | Users this product's licence allows |
| `senhub.ibmi.ptf_group.installed` | `ibmi.ptf_group.installed` | PTF Group — {group} | # | 1 when this PTF group is installed at the level the system expects |
| `senhub.ibmi.ptf_group.level` | `ibmi.ptf_group.level` | PTF Group Level — {group} | # | Level of this PTF group as installed; compare against the level IBM publishes |
| `senhub.ibmi.watch.session_active` | `ibmi.watch.session_active` | Watch Session — {session_id} | # | 1 when this watch session is running |
| - | `ibmi.message_queue.event` | QSYSOPR Message | # | A message reached QSYSOPR; carries the message identifier and text as attributes |
| - | `ibmi.history_log.event` | QHST Event | # | An entry was written to the history log QHST |
| - | `ibmi.audit_journal.event` | Audit Event | # | An entry was written to the audit journal |
| `senhub.ibmi.collector.success` | `ibmi.collector.success_total` | Collector Success — {collector} | # | Successful runs of this collector since the agent started |
| `senhub.ibmi.collector.failure` | `ibmi.collector.failure_total` | Collector Failure — {collector} | # | Failed runs of this collector; a collector failing alone leaves its metrics absent while the rest keep reporting |
| `senhub.ibmi.collector.last_duration` | `ibmi.collector.last_duration_ms` | Collector Duration — {collector} | ms | How long this collector's last run took |
| `senhub.ibmi.collector.last_success_timestamp` | `ibmi.collector.last_success_timestamp` | Collector Last Success — {collector} | s | Unix time of this collector's last successful run |

<!-- schema:metrics:end -->
