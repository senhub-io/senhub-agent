# Reference dashboards survey

**Goal:** capture the canonical patterns used by the dominant agent dashboards on the market so SenHub's catalog launches with the same shape operators already recognize, rather than inventing structure.

**Scope:** the seven SenHub audiences agreed for the v1 catalog —
Linux host, Windows host, agent self-monitoring, logs, Citrix XenApp/Desktop, NetScaler ADC, Veeam Backup, Redfish hardware.

**Style alignment:** Grafana Cloud Integrations (the modern Grafana
Labs reference). Drop the visual conventions of the existing
`senhub-*` and `sensorfactory-*` dashboards on sha901 — they predate
this work and shouldn't constrain the new catalog.

---

## 1. The pattern that everybody copies

Across Grafana Cloud's Linux + Windows integrations, every reference
dashboard follows the same 6-7 dashboard layout per audience:

| # | Dashboard role | Audience |
|---|---|---|
| 1 | **Overview** (single host) | Operator opens one host, sees a one-screen status |
| 2 | **Fleet overview** | NOC view: many hosts, find the bad one in 5s |
| 3 | **CPU and system** | Drill-down: load, processes, contention |
| 4 | **Memory** | Drill-down: used/cached/buffers, swap, pressure |
| 5 | **Filesystem and disks** | Drill-down: usage, IO, inodes |
| 6 | **Network** | Drill-down: throughput, errors, drops, retransmits |
| 7 | **Logs** | LogsQL view filtered by the same host/job vars |

Windows often merges Memory into "CPU and system" and Network into
"Overview" (5 dashboards instead of 7). That's a stylistic choice;
the layout depth is similar.

**This is the spine we copy.**

---

## 2. Audience-by-audience reference

### 2.1 Linux host — gold standard

**Canonical reference**: Grafana Cloud "Linux Server" integration
(7 dashboards) + Node Exporter Full (id 1860, ~28 M downloads —
single 3-row mega-dashboard, kept as drill-down companion).

**Grafana Cloud Linux integration dashboards:**

1. **Linux node / overview** — single-node summary
2. **Linux node / fleet overview** — multi-instance aggregated view
3. **Linux node / CPU and system**
4. **Linux node / memory**
5. **Linux node / filesystem and disks**
6. **Linux node / network**
7. **Linux node / logs**

**Alerting rules shipped (24 total) — useful as inspiration for
SenHub alert recipes:**

Filesystem (6):
- `NodeFilesystemAlmostOutOfSpace` (warning + critical tiers)
- `NodeFilesystemFilesFillingUp` (warning + critical)
- `NodeFilesystemAlmostOutOfFiles` (warning + critical)

System health (18):
- `NodeCPUHighUsage`, `NodeMemoryHighUtilization`,
- `NodeClockNotSynchronising`, `NodeClockSkewDetected`,
- `NodeDiskIOSaturation`, `NodeFileDescriptorLimit`, `NodeHasRebooted`,
- `NodeHighNumberConntrackEntriesUsed`, `NodeMemoryMajorPagesFaults`,
- `NodeNetworkReceiveErrs`, `NodeNetworkTransmitErrs`,
- `NodeProcessesCountIsHigh`,
- `NodeRAIDDegraded`, `NodeRAIDDiskFailure`,
- `NodeSystemSaturation`,
- `NodeSystemdServiceCrashlooping`, `NodeSystemdServiceFailed`,
- `NodeTextFileCollectorScrapeError`

**Node Exporter Full (id 1860) — layout** *(useful when designing
the single-host "deep dive" companion):*

Row 1 — **Quick CPU / Mem / Disk** (top-row stat tiles):
- Pressure (bargauge — PSI)
- CPU Busy (gauge), Sys Load (gauge), RAM Used (gauge),
  SWAP Used (gauge), Root FS Used (gauge)
- CPU Cores (stat), RAM Total (stat), SWAP Total (stat),
  RootFS Total (stat), Uptime (stat)

Row 2 — **Basic CPU / Mem / Net / Disk** (4 chunky timeseries):
- CPU Basic, Memory Basic, Network Traffic Basic,
  Disk Space Used Basic

Row 3+ — **Full drilldown** (collapsed by default):
- CPU, Memory, Network Traffic, Network Saturation,
- Disk IOps, Disk Throughput,
- Filesystem Space Available, Filesystem Used, Disk I/O Utilization,
- Pressure Stall Information,
- Memory Committed, Memory Dirty/Writeback, …

Templating: `$node` (instance), `$job`, `$__rate_interval`,
`${ds_prometheus}`. Variable `$node` is multi-select, regex
filterable, default current.

### 2.2 Windows host — slightly simpler

**Canonical reference**: Grafana Cloud "Windows" integration
(5 dashboards):

1. **Windows overview** — single host summary
2. **Windows fleet overview** — multi-host
3. **Windows CPU and system** (memory bundled in)
4. **Windows disks and filesystems**
5. **Windows logs** — Application + System event logs

**Alerting rules (8):**
- `WindowsCPUHighUsage`, `WindowsMemoryHighUtilization`,
- `WindowsDiskAlmostOutOfSpace`, `WindowsDiskDriveNotHealthy`,
- `WindowsNTPClientDelay`, `WindowsNTPTimeOffset`,
- `WindowsNodeHasRebooted`, `WindowsServiceNotHealthy`

**Notable Windows-specific panels worth keeping in SenHub:**
- Logical disk free space (per-drive bargauge)
- Service status table (Win32_Service health: running/stopped/failed)
- Top-N processes by CPU & memory (table)
- NTP offset stat + timeseries
- Event log volume by source (Application / System / Security)
  — bar chart per source

### 2.3 Agent self-monitoring — Grafana Alloy mixin

**Canonical reference**: Grafana Alloy's
`operations/alloy-mixin/dashboards/`. **9 thematic dashboards** —
self-monitor every aspect of the running agent:

1. **alloy-logs** — logs the agent itself emits
2. **cluster-node** — single node in an Alloy cluster
3. **cluster-overview** — multi-node cluster view
4. **controller** — Alloy components controller activity
5. **loki** — Loki sink pipeline self-stats
6. **opentelemetry** — OTel pipeline stats
7. **otel-engine-overview** — OTel engine internals
8. **prometheus** — Prom RW pipeline self-stats
9. **resources** — CPU/memory/GC/goroutines/network of the agent process

**`resources.libsonnet` is the most useful template** (panels):
- CPU usage (% of 1 core)
- Memory (RSS)
- Garbage collections (rate)
- Goroutines (count + slope — leak indicator)
- Memory (heap in use)
- Network receive bandwidth
- Network send bandwidth

**For SenHub**: we don't need 9 dashboards. The minimum useful set is
**1** ("SenHub agent — self-monitoring") with sections for: process
resources (the Alloy "resources" template), probe health, OTLP push
counters (success / drops / retries / queue fill), Prometheus exposition
counters (requests, scrape latency).

The metrics already exist as `senhub_agent_*` (uptime, probes
total/active/healthy, collect errors, HTTP requests, build info).

### 2.4 Logs — Grafana Cloud pattern

**Canonical reference**: "Linux node / logs" + "Windows logs"
dashboards in the respective Grafana Cloud integrations.

Both follow the same layout:

Row 1 — **Volume** (timeseries):
- Logs per second per severity (stacked area: TRACE/DEBUG/INFO/WARN/
  ERROR/FATAL, color-coded standard scheme)
- Logs per second per source (per syslog.appname or systemd.unit,
  top-10)

Row 2 — **Distribution** (stat + bar):
- Total logs in range (stat)
- Error rate (stat with sparkline)
- Top-10 sources by volume (bar)
- Top-10 sources by error count (bar)

Row 3 — **Stream** (logs panel):
- Filtered LogsQL query, severity dropdown, free-text search box
- 200-500 line buffer, descending

Templating: `service` (multi-select), `severity` (multi-select,
default INFO+), `appname/unit` (multi-select), free-text `search`.

### 2.5 Citrix XenApp / XenDesktop — no canonical open reference

**What exists in the ecosystem:**
- **Citrix Director** UI (built-in to Citrix CVAD): not Grafana
- **ControlUp** (commercial): proprietary, not portable
- **eG Innovations** (commercial): proprietary
- A few one-off community dashboards on grafana.com (most based on
  prtg-citrix-sensor-style data, not a Prometheus exporter standard)

**Implication for SenHub**: there's **no de-facto pattern to copy** —
we design from first principles based on what the SenHub Citrix
probe exposes and what Citrix admins consistently look at in Director.

**Panel inventory Director admins watch (research from Citrix docs +
ControlUp screencaps):**
- Connected sessions count + trend (24 h)
- Failed logons count + reason breakdown
- Logon duration p50 / p95 / p99 (the "logon duration" experience)
- Machine power state (active / unregistered / failed / in maintenance)
- License usage % vs limit
- VDI vs Server OS desktop split
- Top-N users with longest logon duration
- Top-N failed sessions by user/machine
- Hypervisor connection health (XenServer/vSphere/Hyper-V)

The SenHub Citrix probe already exposes most of these — design space
is wide open.

### 2.6 Citrix NetScaler ADC — thin references

**Canonical reference**: `netscaler/netscaler-adc-metrics-exporter`
ships 3 sample dashboards:

1. **sample_system_stats** — 3-panel dashboard:
   licensed throughput rate, memory %, CPU %.  
   *Single row, very basic. Useful as a starting point only.*
2. **sample_lb_stats** — LB vservers counters (multiple per-vserver
   metric graphs).
3. **k8s_cic_ingress_services_stats** — K8s Ingress Controller
   specific (not relevant for SenHub's typical use case).

**Citrix ADM** (NetScaler's official cloud admin UI) is the real
reference but its dashboards aren't portable.

**Implication for SenHub**: same as Citrix XenApp — opportunity to be
the reference. The SenHub NetScaler probe exposes ~100 metrics; the
existing OTel mapping already collapses sensibly. Design dashboard
to surface the operator's daily-watch list:
- HA cluster state (primary/secondary, sync state, last failover)
- VServer status table (up/down/oos/trofs/busy/unknown) with traffic
- Service / servicegroup health
- SSL cert expiry (top-N nearest)
- Throughput by direction, by interface
- Connection rate, active/established counts
- Cache hit ratio, compression efficiency
- CPU/memory of the appliance itself (`sample_system_stats` content)

### 2.7 Veeam Backup — no canonical open reference

**What exists:**
- **Veeam ONE** (commercial product): the reference UI, not portable
- A handful of community grafana.com dashboards based on
  `veeam-prometheus-exporter` from 2019-2022 — none widely adopted
- Most ops teams cobble dashboards manually from the Veeam REST API

**Implication for SenHub**: design space wide open.

**Panel inventory backup admins watch:**
- Last backup success/failure per job (table sorted by last-run time)
- Backup completion time per job (timeseries, p95)
- Storage repository capacity (bargauge per repo, free/used/total)
- Failed jobs in last 24 h (stat + table)
- Long-running jobs (over their SLA threshold)
- Restore points count per VM (top/bottom outliers)
- Storage repository read/write throughput

The SenHub Veeam probe exposes the `senhub.veeam.*` namespace
(~33 raw metrics → 20 OTel names via the YAML transformer's
collapses), enough to cover all of the above.

### 2.8 Redfish hardware — no canonical reference

**What exists:**
- `jenningsloy318/redfish_exporter` and `mauve-software/iDRAC-exporter`
  but neither ships dashboards
- iDRAC/iLO/IPMI web UIs are vendor-specific, not portable
- Some homelab community dashboards exist but no standard

**Implication for SenHub**: again, opportunity.

**Panel inventory hardware admins watch:**
- Power state per chassis (on/off/standby/error — heatmap or table)
- Inlet temperature, exhaust temperature (timeseries, per chassis)
- Fan RPM per fan (sparkline grid)
- PSU input/output power, status
- Physical disk SMART (predicted_failure / failed states)
- RAID array health (per array: degraded/optimal/failed)
- BMC firmware versions + EOL alerts

The SenHub Redfish probe maps to `hw.status` with strict OTel
expansion (one datapoint per state — see semconv doc §4.x), so a
"states heatmap" is the natural visualization.

---

## 3. Patterns retained for the SenHub catalog

### 3.1 Layout invariants

Every SenHub v1 dashboard adopts:

- **Time range default**: `now-1h`
- **Refresh default**: `30s` (matches our default 30s metrics push)
- **Tags**: every JSON carries `["senhub", "agents", "<audience>"]`
  for filtering in the dashboards list
- **Top row**: stat tiles (3-5) showing the headline KPIs of the
  audience — first thing the operator sees, no scroll
- **Second row**: 2-4 chunky timeseries for the same KPIs over time
- **Subsequent rows** (collapsed-by-default for depth): per-resource
  drilldown
- **Last row** when applicable: a logs panel filtered by the same
  variable

### 3.2 Templating variables (standard names)

| Variable | Meaning | Datasource | Always multi-select |
|---|---|---|---|
| `$service` | `service.name` from OTLP resource | victoriametrics | yes |
| `$instance` | `service.instance.id` | victoriametrics | yes |
| `$mount` (fs dashboards) | `system.filesystem.mountpoint` | victoriametrics | yes |
| `$iface` (network) | `network.interface.name` | victoriametrics | yes |
| `$severity` (logs) | `severity` | victorialogs | yes, default `INFO+ERROR+WARN+FATAL` |

### 3.3 Color scheme for severity / state

| Domain | Standard mapping |
|---|---|
| Log severity | TRACE/DEBUG = grey, INFO = blue, NOTICE = light blue, WARN = yellow, ERROR = orange, FATAL = red |
| OTel `hw.state` | `ok` = green, `degraded` = yellow, `failed` = red, `unknown` = grey, `predicted_failure` = orange |
| NetScaler `*.state` | `up` = green, `down` = red, `trofs/busy` = orange, `unknown` = grey |
| Veeam `job.last_result` | `success` = green, `warning` = yellow, `failed` = red, `none` = grey |

### 3.4 Naming convention

`SenHub <audience> — <view>` :
- `SenHub Linux — Overview`
- `SenHub Linux — Fleet`
- `SenHub Linux — CPU & System`
- `SenHub Linux — Memory`
- `SenHub Linux — Filesystem`
- `SenHub Linux — Network`
- `SenHub Linux — Logs`
- `SenHub Windows — Overview` ... etc.
- `SenHub Agent — Self-monitoring`
- `SenHub Citrix VDI — Sessions & Logons`
- `SenHub NetScaler — HA & VServers`
- `SenHub Veeam — Jobs & Repositories`
- `SenHub Redfish — Hardware Health`

Dashboard UID convention: `senhub-<audience>-<view>` kebab-case.

### 3.5 Anti-patterns we won't copy

- **Mega-dashboards** (40+ panels in one screen, à la Node Exporter
  Full row 3): keep them only as optional "deep dive" companions,
  never the default landing page.
- **Cryptic legends**: every panel must answer "what should I do if
  this is red?" — implicit threshold visible via color, legend
  format named not raw label.
- **Mixed datasources without indication**: panels using
  victorialogs vs victoriametrics labelled in the title when
  ambiguity is possible.
- **Hard-coded `[1m]` rate intervals**: always use
  `$__rate_interval` so the panel adapts to the user's zoom.

---

## 4. Catalogue sizing summary

| Audience | Dashboards | Reference quality |
|---|---|---|
| Linux host | 7 | Strong (Grafana Cloud + node_exporter) |
| Windows host | 5 | Strong (Grafana Cloud) |
| Agent self-monitoring | 1 | Strong (Grafana Alloy resources mixin) |
| Citrix VDI | 1-2 | **Design from scratch** (no open standard) |
| NetScaler ADC | 1-2 | Thin (NetScaler exporter samples) |
| Veeam Backup | 1-2 | **Design from scratch** |
| Redfish hardware | 1-2 | **Design from scratch** |
| **Total v1 target** | **17-19 dashboards** | |

That's the canonical Grafana Cloud-style coverage, with v1 going from
the current 1 dashboard ("SenHub Agents — OTLP push") to a full
audience-aligned catalog.

Implementation effort sticks to the earlier ~5 day estimate: the
4 "design from scratch" audiences are the cost — Linux/Windows are
~half-day each if we follow the Grafana Cloud Linux/Windows JSONs as
templates with relabeled queries.

---

## 5. Next steps

This research feeds:

1. **`CATALOG-PROPOSAL.md`** — concrete dashboard-by-dashboard
   structure with the metric queries we'll use (one section per
   dashboard, panels listed with the SenHub PromQL).
2. **Implementation** — JSON files in `docs/grafana/` per audience,
   deployed to sha901 via the existing provisioning path.
3. **Future: alert rules pack** — the Grafana Cloud integrations
   ship 24+8 = 32 standard alerts. We replicate the spirit in
   `docs/grafana/alerts/` once the dashboards are stable.
