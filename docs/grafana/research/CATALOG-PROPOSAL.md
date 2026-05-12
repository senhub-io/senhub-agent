# SenHub Grafana catalog — v1 proposal

**Companion to**: `REFERENCE-DASHBOARDS.md` (the research that
underpins this proposal).

**Audience**: SenHub product owner — validate before any dashboard
JSON is written.

**Scope**: 18 dashboards across 7 audiences, calqued on the Grafana
Cloud Integrations pattern.

**Naming convention**: `SenHub <audience> — <view>`. UID:
`senhub-<audience>-<view>` (kebab-case). Tags:
`["senhub", "agents", "<audience>"]`.

**Datasources expected**:
- `victoriametrics` (UID `victoriametrics`) — Prometheus-compatible
- `victorialogs` (UID `victorialogs` or
  `victoriametrics-logs-datasource` if available) — VictoriaLogs

**Common variables**:
- `$service` — multi-select of `service_name` matching the
  audience's regex (e.g. `sha\d{3}-prod` for our prod fleet today,
  configurable for tenants)
- `$instance` — multi-select of `service_instance_id`

---

## 1. Linux host (7 dashboards)

### 1.1 SenHub Linux — Overview

**Audience**: operator opens one host, expects to know its state in
3 seconds.

**Variables**: `$service`, `$instance`.

**Layout**:

- Row "Status" (4 stat tiles + 1 logo):
  - Memory used (%), CPU utilization (avg of cores), Load 1m,
    Max FS used %.
- Row "CPU + Memory":
  - CPU utilization per core (timeseries, max=1)
  - Memory usage by state (stacked bytes)
- Row "Filesystem + Network":
  - Filesystem usage bargauge per mount (`system.filesystem.utilization`)
  - Network throughput per interface + direction (rate, Bps)
- Row "Recent errors" (logs panel — severity ERROR/FATAL only, last 100):
  - LogsQL: `service.name:~"$service" AND severity:~"ERROR|FATAL"`

### 1.2 SenHub Linux — Fleet

**Audience**: NOC view, "show me all hosts side by side".

**Variables**: none (intentional — fleet means everyone).

**Layout**:

- Row "Heatmap": table of hosts with stat-column per KPI:
  - service.name | CPU % | Mem % | Max FS % | Load 1m | Uptime
  - Color thresholds inline.
- Row "Distribution":
  - CPU utilization timeseries (one line per host, max 50 hosts).
  - Memory utilization timeseries (one line per host).
- Row "Health summary" (stat tiles):
  - Total hosts reporting
  - Hosts > 80% CPU (count)
  - Hosts > 90% memory (count)
  - Hosts with FS > 95% (count)

### 1.3 SenHub Linux — CPU & System

**Variables**: `$service`, `$instance` (single-select for this drill-
down).

**Layout**:

- Row "Headline":
  - Avg CPU utilization (stat)
  - Load 1m / 5m / 15m (3 stats inline)
  - Number of cores (stat)
- Row "Time series":
  - CPU utilization per core (`system_cpu_utilization_ratio`)
  - CPU time by mode (`rate(system_cpu_time_seconds_total[$__rate_interval])`,
    stacked — idle/user/system/iowait/softirq/...)
- Row "Saturation":
  - Load averages 1m/5m/15m timeseries
  - Context switch rate, interrupt rate (if exposed — currently
    not, gap to file)
- Row "Processes":
  - Process count timeseries (gap — not yet exposed by SenHub,
    `node_exporter` exposes via `node_processes_*`)

### 1.4 SenHub Linux — Memory

**Layout**:

- Row "Headline":
  - Memory used (%, stat), Memory total (bytes), SWAP used (% stat
    when available)
- Row "Breakdown":
  - Stacked memory by state (used/cached/buffers/free/slab/...)
  - Utilization ratio over time
- Row "Pressure" (when exposed):
  - Paging rate (in/out)
  - Major page faults
  - Memory pressure stall info (PSI — gap, not exposed yet)

### 1.5 SenHub Linux — Filesystem

**Variables**: `$service`, `$instance`, `$mount` (multi-select).

**Layout**:

- Row "Capacity":
  - Bargauge per mountpoint (used %)
  - Total bytes used vs total available (stacked)
- Row "Inode":
  - Inode utilization per mount (timeseries)
- Row "I/O" — *if disk I/O metrics later added (gap today)*:
  - Read/write throughput per device
  - I/O utilization %, queue depth

### 1.6 SenHub Linux — Network

**Variables**: `$service`, `$instance`, `$iface` (multi-select).

**Layout**:

- Row "Throughput":
  - Per-interface bytes/sec (in + out, stacked)
  - Per-interface packets/sec
- Row "Errors":
  - Errors + drops rate per interface
- Row "Sockets" — *gap if not exposed today*:
  - TCP connections by state, conntrack table fill

### 1.7 SenHub Linux — Logs

**Datasource**: `victorialogs`.

**Variables**: `$service`, `$severity` (multi), `$unit` (systemd unit,
multi), free-text `$search`.

**Layout**:

- Row "Volume":
  - Logs per second by severity (stacked area)
  - Logs per second by `systemd.unit` (top-10)
- Row "Distribution":
  - Total logs (stat), Error logs (stat with sparkline),
    Warn logs (stat), Top-10 sources by volume (bar),
    Top-10 sources by error count (bar)
- Row "Stream" (logs panel):
  - Query:
    `service.name:~"$service" AND severity:~"$severity" AND systemd.unit:~"$unit" AND _msg:~".*$search.*"`
  - Limit 500, descending

---

## 2. Windows host (5 dashboards)

Same template as Linux but adapted to Windows specifics. Re-uses
**most** queries identically (the OTel mapper produces the same names
on Linux and Windows for cpu/memory/network/filesystem). The Windows
delta:

- **Filesystem** dashboard names drives by letter (`C:\\`, `D:\\`)
  via the `system.filesystem.mountpoint` label which on Windows
  carries the drive letter.
- **CPU & system** dashboard includes Windows-only panels for
  - DPC time, IRQ rate (`senhub.system.cpu.queue_length` per OTel
    mapping doc)
  - Top-N services by health (when the SenHub Windows event probe
    eventually surfaces service status)

**Dashboards:**

- `SenHub Windows — Overview` (mirror of Linux Overview)
- `SenHub Windows — Fleet` (mirror)
- `SenHub Windows — CPU & System` (with the Windows-specific extras)
- `SenHub Windows — Disks & Filesystems`
- `SenHub Windows — Logs` (filtered to Windows event log entries
  via the syslog/event probe — once Windows event log probe ships)

---

## 3. Agent self-monitoring (1 dashboard)

### 3.1 SenHub Agent — Self-monitoring

**Audience**: anyone responsible for "is the fleet healthy?" —
distinct from the workload metrics.

**Datasource**: `victoriametrics` (the Prometheus scrape path exposes
the `senhub_agent_*` metrics).

**Variables**: `$service`, `$instance`.

**Layout**:

- Row "Fleet status":
  - Agents reporting (stat = count of distinct `service_name`)
  - Probes configured total (sum of `senhub_agent_probes_total`)
  - Probes healthy (sum of `senhub_agent_probes_healthy`)
  - Build version split (table by `senhub_agent_build_info`)
- Row "Activity":
  - Agent uptime (timeseries, `senhub_agent_uptime_seconds / 3600`)
  - Cache entries per agent (timeseries)
  - Collect errors rate (`rate(senhub_agent_collect_errors_total[$__rate_interval])`)
- Row "OTLP export health" (when push counters land in agent self
  metrics — currently a gap, easy to add):
  - Records pushed per second per agent
  - Export errors rate
  - Log buffer fill % (when wired)
- Row "Process resources" (calqued on Grafana Alloy `resources`
  mixin):
  - CPU usage of the agent process (when exposed — gap)
  - RSS memory (gap)
  - Goroutines, GC rate (gap)

> Several panels here highlight gaps in the agent's self-observability.
> Fill those first (cheap, ~½ day of code in the Prometheus exposition
> bridge), THEN ship the dashboard. Without process resource metrics
> we'd ship a half-empty dashboard that looks worse than no dashboard.

---

## 4. Citrix XenApp / Desktop (2 dashboards)

### 4.1 SenHub Citrix VDI — Sessions & Logons

**Audience**: Citrix admin watching daily VDI experience.

**Variables**: `$service` (typically a single Citrix Director URL
identifier), `$site` (multi, when multi-site).

**Layout**:

- Row "Headline":
  - Active sessions (stat with sparkline)
  - Failed sessions last 1h (stat)
  - Avg logon duration p50 (stat)
  - p95 logon duration (stat)
- Row "Trends":
  - Active sessions timeseries (per `senhub.citrix.session_type`:
    desktop / app)
  - Logon duration histogram (p50 / p95 / p99 — via `_bucket` if
    SenHub probe emits histogram, or three stat lines if quantiles
    pre-computed)
- Row "Failed sessions":
  - Failed sessions per hour, broken down by `senhub.citrix.failure_reason`
    (stacked)
  - Top-10 users with failed logons (table)
  - Top-10 machines refusing connections (table)
- Row "Machine states":
  - Donut: machines by state (registered / unregistered / failed /
    in maintenance) — uses the strict-OTel expand on `hw.status` /
    Citrix state enum

### 4.2 SenHub Citrix VDI — Capacity & Health

**Layout**:

- Row "License usage" (bargauge):
  - License consumed vs limit, per license type
- Row "Delivery groups":
  - Table: per-delivery-group available capacity, used capacity,
    sessions, failed registrations
- Row "Hypervisor health":
  - Per-hypervisor connection state (up / down / error)
  - Top-N hypervisors by VM count

---

## 5. NetScaler ADC (2 dashboards)

### 5.1 SenHub NetScaler — HA & VServers

**Audience**: NetScaler admin daily watch.

**Variables**: `$service` (NetScaler appliance), `$vserver_type`
(lb / cs / aaa / gslb — multi).

**Layout**:

- Row "HA":
  - HA role per node (stat — primary/secondary)
  - HA sync state
  - Last failover time
- Row "vServers status":
  - Table: vserver | type | state (up/down/oos/trofs/busy/unknown)
    | requests/sec | response time | hits | errors
  - Color the state column with the standard scheme
- Row "Throughput":
  - NS-wide throughput (bit/s) by direction
  - Per-vserver throughput (timeseries top-10)
- Row "Connections":
  - Active/established TCP connections (server side + client side)
  - SSL transactions/sec
  - Cache hit ratio

### 5.2 SenHub NetScaler — Appliance & SSL

**Layout**:

- Row "System":
  - CPU (data plane vs management plane — uses
    `senhub.netscaler.cpu.plane` attribute), Memory %, Disk %
    (uses `system.filesystem.*` for the appliance disks)
- Row "SSL certs":
  - Top-10 certs by closest expiry (table sorted ascending by
    `expires_in_days`)
  - Cert validity timeseries
- Row "AppFW" (when probe exposes them):
  - Blocked requests/responses rate (by type: sqli/xss/buffer_overflow)

---

## 6. Veeam Backup (2 dashboards)

### 6.1 SenHub Veeam — Jobs

**Layout**:

- Row "Headline":
  - Jobs run last 24 h (stat), Failed last 24 h (stat),
    Warnings last 24 h (stat), Currently running (stat)
- Row "Job timeline":
  - Heatmap: jobs (rows) × time (columns), color = last result
    (green/yellow/red/grey) — Grafana state-timeline panel
- Row "Trends":
  - Job duration p95 per job (top-10 longest)
  - Job throughput (data processed/sec) per job (top-10)
- Row "Failed jobs":
  - Table: job name | last result | last error message | duration | size

### 6.2 SenHub Veeam — Repositories

**Layout**:

- Row "Capacity":
  - Bargauge: per-repository free / used / total
  - Repositories nearing capacity (table sorted asc by free)
- Row "Activity":
  - Per-repo read/write throughput (timeseries)
  - Per-repo restore points count + storage growth slope

---

## 7. Redfish hardware (2 dashboards)

### 7.1 SenHub Redfish — Hardware Health

**Audience**: anyone who needs to know "did a fan die in the data
center last night?"

**Layout**:

- Row "Headline":
  - Chassis count (stat)
  - Chassis at fault (stat — count where `hw.state != ok`)
  - Predicted failures (stat — count where `hw.state =
    predicted_failure`)
  - Hosts in degraded state (stat)
- Row "Status matrix":
  - State-timeline / heatmap panel: rows = `hw.type` (cpu / memory /
    disk / power_supply / fan / network_card), cells = state, color
    per state mapping
- Row "Drives":
  - Table: drive | type | size | state | smart status | health
  - Color the state column
- Row "Environmental":
  - Inlet temperature (timeseries per chassis)
  - Fan RPM (sparkline grid per fan)
  - Power consumption (W) per PSU

### 7.2 SenHub Redfish — Storage & RAID

**Layout**:

- Row "RAID arrays":
  - Table: array | RAID level | status | size | rebuild progress %
- Row "Physical disks":
  - State table (already in 7.1, repeated here for storage-focused
    operator)
  - Smart predicted_failure timeseries
- Row "Logical disks":
  - Capacity per logical volume, fragmentation when exposed

---

## 8. Roll-up: 18 dashboards total

| Audience | Dashboards | Effort estimate |
|---|---|---|
| Linux host | 7 | 1 day (Grafana Cloud templates as starting point) |
| Windows host | 5 | 0.5 day (mirror Linux + Windows specifics) |
| Agent self-monitoring | 1 | 0.5 day (depends on filling self-metric gaps first, ~½ day Go) |
| Citrix VDI | 2 | 1 day (design from scratch + verify metric availability) |
| NetScaler ADC | 2 | 1 day (rich probe, decide what to surface) |
| Veeam | 2 | 0.5 day (well-scoped) |
| Redfish | 2 | 1 day (strict OTel expand state-timelines are non-trivial) |

**Total** ≈ 5-6 days work to ship the full catalog, aligned with the
earlier estimate.

## 9. Gaps to fix first

These three things should ship in code BEFORE the dashboards because
they unblock high-value panels and avoid empty visuals:

1. **Process self-metrics** in `senhub_agent_*`:
   process CPU %, RSS, goroutines, GC rate.
   (~½ day in `internal/agent/services/agentstate/` + Prometheus
   bridge.)
2. **OTLP push self-metrics**:
   `senhub_agent_otlp_metrics_pushed_total`,
   `senhub_agent_otlp_logs_pushed_total`,
   `senhub_agent_otlp_export_errors_total`,
   `senhub_agent_otlp_buffer_fill_ratio` — already listed in the
   OTLP implementation plan §7 but not yet wired.
   (~½ day in the OTLP strategy.)
3. **Process count** in Linux/Windows host probes (gap for the
   "CPU & System" dashboard):
   add `system_processes_count` exposure. (~½ day.)

These three gaps are addressed in a follow-up branch
`feat/agent-self-observability` before the dashboard work lands.

## 10. Decision point

Validate this proposal before any dashboard JSON is written. Two
sub-questions:

1. **Catalog scope**: ship all 18 in v1, or phase as 7 host + 1
   self-monitoring first, then 8 vendor in v2? Ranking by ROI per
   day suggests host + self-monitoring first.

2. **Gap-fill ordering**: do we ship dashboards with explicit
   "TODO: metric not yet exposed" placeholders, or wait for the
   three self-obs gaps to be filled? Cleaner to fill the gaps first;
   adds ~1.5 day before any dashboard work starts.

My recommendation: **fill gaps → build host + self-mon dashboards →
validate live on sha901 → build vendor dashboards in a second wave**.
