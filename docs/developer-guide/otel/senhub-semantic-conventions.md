# SenHub OpenTelemetry Semantic Conventions

**Status:** WIP — a living document, updated with each batch of probes
**Last updated:** 2026-05-14 (batch 5: databases)
**Audience:** probe developers, mapper maintainers

## 0. Purpose

This document lists the **OTel naming conventions** SenHub Agent adopts for every metric it exposes. It covers:

1. Metrics that adopt the official OTel conventions **as they are** (`system.*`, `http.*` namespaces, and so on)
2. Proprietary extensions under the **`senhub.*`** namespace for domains OTel does not cover (netscaler, citrix, veeam…) or for platform-specific metrics (Windows Perfmon, Linux-specific…), each with its justification and the references consulted
3. Harmonisations (e.g. `cpu.mode=system` shared between Linux `system` and Windows `privileged`)

**Guiding principles:**
- **OTel first**: adopt an existing convention rather than invent one. Check the official semconv, the OTel Collector contrib receivers, and de-facto vendor conventions (Grafana Labs, VictoriaMetrics, prometheus-community) before defining anything.
- **Stability**: once published, a convention does not move — dashboards depend on it. Changes mean a major version.
- **Traceability**: every `senhub.*` extension is documented here with its justification and links.

## 1. Reference sources

Consulted for each decision:

- [OTel Semantic Conventions](https://github.com/open-telemetry/semantic-conventions) (official)
- [OTEP 0119 - Standard System Metrics](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md) (for OS-specific metrics)
- [OTel Collector contrib receivers](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver)
- [prometheus-community exporters](https://github.com/prometheus-community) (node_exporter, windows_exporter, redfish_exporter…)
- Vendor documentation (Grafana Labs integrations, VictoriaMetrics, DataDog)

## 2. OTel → Prometheus conversion rules

Applied by the Prometheus mapper per the [OTel compatibility spec](https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/):

1. `senhub_` prefix added to the name (all namespaces)
2. dots → underscores, in names and attributes
3. characters outside `[a-zA-Z_:][a-zA-Z0-9_:]*` replaced by `_`, consecutive underscores collapsed
4. unit suffix: `s` → `_seconds`, `By` → `_bytes`, `Hz` → `_hertz`, `1` → `_ratio`, annotation units in braces `{...}` → dropped, `foo/bar` → `_foo_per_bar`
5. a counter gets `_total` if it does not already end in it
6. utilisation (`unit: 1`): the mapper **converts automatically**, turning the cache's 0-100 values into a 0-1 ratio

Examples:
| OTel | Prometheus |
|---|---|
| `system.cpu.time` / counter / `s` / `cpu.mode=user` | `senhub_system_cpu_time_seconds_total{cpu_mode="user"}` |
| `system.cpu.utilization` / gauge / `1` / `cpu.mode=user` | `senhub_system_cpu_utilization_ratio{cpu_mode="user"}` (value ÷ 100) |
| `senhub.system.cpu.queue_length` / gauge / `{thread}` | `senhub_system_cpu_queue_length` |
| `system.linux.cpu.load_1m` / gauge / `{thread}` | `senhub_system_linux_cpu_load_1m` |

## 2bis. Strict OTel compliance — the "mapper-side" principle

**OTel compliance lives in the mapper, not in the cache.** When a probe emits a data point whose strict OTel semantics require a **different shape** on the way out — an enum encoded as a numeric value has to become N per-state data points, for instance — the mapper performs that transformation **at serialization time, towards the target format** (Prometheus today, native OTLP tomorrow).

**What this buys future exports**: when a native OTLP mapper is added (Phase 3) it will emit strict OTel **with nothing to correct** — the deviations are already handled upstream by mapper logic shared across the OTel-aware formats (Prometheus, OTLP, Zabbix-OTel, and so on).

**The documented mechanism**: the `otel.expand` block in the YAML transformers declares an enum → per-state expansion. The mapper reads that directive and produces the appropriate N data points on each scrape. See `IMPLEMENTATION-PLAN.md §4` for the exact schema.

The typical case: every `hw.status` metric (hardware health) follows this pattern — 1 data point in the cache (an enum code from a lookup) → N data points at serialization, one per `hw.state` value.

## 3. Labels present on everything

On **every** metric a probe emits, the Prometheus mapper adds:

| Label | Source | Exemple |
|---|---|---|
| `probe_name` | instance name (config) | `cpu-linux-primary` |
| `probe_type` | registry type | `cpu` |
| *custom_tags labels* | when `include_probe_tags: true` | `env=prod, site=paris` |

The `instance` label (reserved by the Prometheus scrape) is **never** emitted by the agent.

## 4. Conventions adopted, per probe

### 4.1 Probe `cpu` (système)

**Primary source:** [OTel system metrics — CPU](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)
**Secondary source:** [windows_exporter collector.cpu](https://github.com/prometheus-community/windows_exporter/blob/master/docs/collector.cpu.md), [OTEP 0119](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md)

#### 4.1.1 Native OTel metrics used

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `system.cpu.time` | `s` | Counter | Cumulative CPU time per mode (Linux: `/proc/stat`) |
| `system.cpu.utilization` | `1` | Gauge | Instantaneous utilisation (%), normalised to a ratio by the mapper |

**Attributes used:**

- `cpu.mode` (well-known OTel) — values:
  - `user` — user-space time (Linux cpu_user, Windows user_time)
  - `system` — kernel time (Linux cpu_system, Windows privileged_time) **[harmonised]**
  - `idle` — idle time (Linux cpu_idle)
  - `nice` — low-priority user (Linux cpu_nice)
  - `iowait` — I/O wait (Linux cpu_iowait)
  - `interrupt` — hardware interrupt time (Linux cpu_irq, Windows interrupt_time)
  - `softirq` — software interrupts (Linux cpu_softirq) — well-known extension
  - `steal` — stolen by the hypervisor (Linux cpu_steal)
  - `dpc` — Deferred Procedure Calls (Windows dpc_time) — **extension, aligned with windows_exporter**
- `cpu.logical_number` (well-known OTel) — the logical core number as a string (`"0"`, `"1"`, …)

**Harmonising `system` ↔ `privileged`**: OTel accepts either `kernel` or `system`. We settle on `system` so a cross-OS dashboard queries one mode and gets both Linux kernel time AND Windows privileged time.

#### 4.1.2 OTEP 0119 metrics (load average)

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `system.linux.cpu.load_1m` | `{thread}` | Gauge | cpu_load1 |
| `system.linux.cpu.load_5m` | `{thread}` | Gauge | cpu_load5 |
| `system.linux.cpu.load_15m` | `{thread}` | Gauge | cpu_load15 |

The `linux` prefix states the OS specificity explicitly, per OTEP 0119. Not emitted on Windows.

#### 4.1.3 `senhub.*` extensions (Windows-specific)

**Rationale:** windows_exporter exposes these as counters (totals since boot). Our probe captures them as **instantaneous rates** from Perfmon (DPCs/sec, Interrupts/sec). OTel defines no convention for those rates, so an extension was created.

| Senhub metric | Unit | Type | Probe source | windows_exporter equivalent |
|---|---|---|---|---|
| `senhub.system.cpu.dpcs` | `1/s` | Gauge | cpu_dpc_rate, dpc_rate | `windows_cpu_dpcs_total` (counter) — rate = `rate(...)` |
| `senhub.system.cpu.dpcs_queued` | `1/s` | Gauge | cpu_dpc_queued, dpc_queued | *(none, Perfmon-specific)* |
| `senhub.system.cpu.interrupts` | `1/s` | Gauge | cpu_interrupts, interrupt_sec | `windows_cpu_interrupts_total` (counter) — rate = `rate(...)` |
| `senhub.system.cpu.queue_length` | `{thread}` | Gauge | cpu_queue_length, processor_queue_length | *(none)* |

Attributes: `cpu.logical_number` (optional, present when measured per core).

> **Possible V2 evolution**: refactor the probe to emit cumulative counters and align fully with windows_exporter (`senhub_system_cpu_dpcs_total` and so on). To be discussed later.

### 4.2 `memory` probe (system)

**Primary source:** [OTel system metrics — Memory](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)
**Secondary source:** [OTEP 0119 §Paging](https://github.com/open-telemetry/oteps/blob/main/text/0119-standard-system-metrics.md) *(draft — adopted knowing a rename of the OTEP would force a migration)*

#### 4.2.1 Native OTel metrics used

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `system.memory.limit` | `By` | UpDownCounter | Total RAM installed (Win `memory_total`) |
| `system.memory.usage` | `By` | UpDownCounter | RAM in use, per state (`system.memory.state` attribute) |
| `system.memory.utilization` | `1` | Gauge | % RAM utilisée (cross-platform, `memory_used_percent`) |
| `system.paging.usage` | `By` | UpDownCounter | Swap in use, per state (`system.paging.state` attribute) — Linux `swap_used`/`swap_free` |
| `system.paging.utilization` | `1` | Gauge | % pagefile (`pagefile_usage`) + % swap (`swap_used_percent`) — attribut `system.paging.state`, OTEP 0119 draft |

**Attribut `system.memory.state`**

Valeurs officielles OTel : `buffers, cached, free, used`

**Harmonising Windows `available` → `free`**: both mean memory immediately available for allocation. It keeps cross-OS dashboards simple.

**Extensions `system.memory.state`** (Windows-specific, non OTel-standard) :

| Value | Source | Description |
|---|---|---|
| `committed` | `memory_committed` | Virtual memory committed by the memory manager |
| `modified` | `memory_modified_page_list` | Memory modified but not yet written to disk |
| `nonpaged_pool` | `memory_nonpaged_pool` | Kernel memory that cannot be paged out |
| `paged_pool` | `memory_paged_pool` | Kernel memory that can be paged out |

**Attribut `system.paging.state`**

Values: `used, free`. **Linux swap** (`swap_used`/`swap_free`) is the counterpart of the **Windows pagefile**: OTel models both under `system.paging.*`. They do not get confused — the host OS (a resource attribute) separates the series — and the harmonisation makes paging dashboards cross-OS, on the same logic as `available → free` for RAM.

#### 4.2.2 `senhub.*` extensions (paging)

**Rationale:** our probe exposes Windows paging as **instantaneous rates** from Perfmon. OTEP 0119 proposes `system.paging.faults` and `system.paging.operations` as counters. We create `_per_second` gauge variants for the duration of the migration, to be aligned with the OTel standard when the probe is reworked to cumulative counters. `senhub.system.paging.limit` covers total swap (`swap_total`), for which OTel has no equivalent — it mirrors `system.memory.limit` for RAM.

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.system.paging.faults` | `1/s` | Gauge | – |
| `senhub.system.paging.operations` | `1/s` | Gauge | `direction: in` ou `out` |
| `senhub.system.paging.utilization_peak` | `1` | Gauge | – *(no OTEP 0119 equivalent)* |
| `senhub.system.paging.limit` | `By` | UpDownCounter | – Total configured swap (`swap_total`); *(no OTEP 0119 equivalent)* |

### 4.3 Probe `network` (système)

**Primary source:** [OTel system metrics — Network](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/)

**Alignement 100 % OTel natif** — aucune extension `senhub.*` introduite.

#### 4.3.1 Métriques OTel utilisées

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `system.network.io` | `By` | Counter | Bytes transmis/reçus (total cumulatif) |
| `system.network.packet.count` | `{packet}` | Counter | Paquets transmis/reçus |
| `system.network.errors` | `{error}` | Counter | Erreurs de transmission/réception |
| `system.network.packet.dropped` | `{packet}` | Counter | Paquets rejetés volontairement (discards) |

**Attributes used:**

- `network.io.direction` — valeurs officielles : `receive`, `transmit`
- `network.interface.name` — nom de l'interface (`eth0`, `ens1`, `Ethernet 2`, …)

### 4.4 Probe `logicaldisk` (filesystem + disk I/O)

**Primary source:** [OTel system-metrics §Filesystem](https://opentelemetry.io/docs/specs/semconv/system/system-metrics/) and `§Disk`
**Secondary source:** [node_exporter filesystem_*](https://github.com/prometheus/node_exporter) (inode conventions)

**A note on terminology:** the probe type in the config stays `logicaldisk` — a historical name, kept for JWT licence compatibility and to match Windows Perfmon `\LogicalDisk\`. The metrics it exposes follow the OTel `system.filesystem.*` namespace (capacity) and `senhub.system.disk.*` (Windows I/O rates). It is the OTel namespace that dashboards see.

#### 4.4.1 Native OTel metrics used

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `system.filesystem.limit` | `By` | UpDownCounter | Total capacity (`fs_total_bytes`) |
| `system.filesystem.usage` | `By` | UpDownCounter | In use, per state (attribute `system.filesystem.state`) |
| `system.filesystem.utilization` | `1` | Gauge | Occupancy ratio (`system.filesystem.state` attribute) |

**The `system.filesystem.state` attribute**

Official OTel values: `free, reserved, used`

**Extension `system.filesystem.state=available`** — Linux `statfs` exposes `f_bavail`, the space available to non-root processes, which is distinct from `f_bfree`. Mapped to `available` so the distinction is not lost.

**Unit conversions applied by the mapper:**
- Windows `disk_free_mb` (MB) → OTel unit `By` (bytes): **mapper ×1048576** (MiB).
- Percentages (0-100) → OTel ratio (0-1): **mapper ÷100**.

#### 4.4.2 `senhub.*` extensions (inodes — Linux)

**Rationale:** OTel `system.filesystem.*` is byte-centric. node_exporter exposes `node_filesystem_files` (total inodes) and `node_filesystem_files_free`. We mirror `system.filesystem.*` with an inode sub-namespace, for consistency.

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.system.filesystem.inode.limit` | `{inode}` | UpDownCounter | – |
| `senhub.system.filesystem.inode.usage` | `{inode}` | UpDownCounter | `system.filesystem.state: free` or `used` |
| `senhub.system.filesystem.inode.utilization` | `1` | Gauge | `system.filesystem.state: used` |

#### 4.4.3 `senhub.*` extensions (disk I/O rates — Windows)

**Rationale:** OTel `system.disk.*` defines cumulative counters (`system.disk.operations`, `system.disk.io`). Our Windows probe captures **instantaneous rates** from Perfmon (`\LogicalDisk\Disk Reads/sec` and so on). Hence `_per_second` gauge extensions — full OTel alignment becomes possible once the probe is reworked (V2).

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.system.disk.operations` | `1/s` | Gauge | `disk.io.direction: read` or `write` |
| `senhub.system.disk.io` | `By/s` | Gauge | `disk.io.direction: read` or `write` |
| `senhub.system.disk.queue_length` | `{operation}` | Gauge | – |

#### 4.4.4 Attributes (tag → attribute mapping)

| Internal tag | OTel attribute |
|---|---|
| `device` | `system.device` (e.g. `/dev/sda1`) |
| `mount_point` | `system.filesystem.mountpoint` (ex: `/`, `/var`) |
| `drive` (Windows) | `system.filesystem.mountpoint` (ex: `C:`, `D:`) — harmonisé Linux/Windows |
| `fs_type` | `system.filesystem.type` (ex: `ext4`, `ntfs`) |

### 4.5 `ping_gateway` and `ping_webapp` probes (ICMP connectivity)

**Primary source:** none in OTel (no ICMP semconv)
**Secondary source:** [Prometheus blackbox_exporter](https://github.com/prometheus/blackbox_exporter) (`probe_icmp_*` convention)

**Note:** our ICMP probes take **continuous aggregated measurements** (windowed averages), not one-shot probes like blackbox_exporter. The names are adapted accordingly, under the `senhub.probe.*` namespace.

#### 4.5.1 `senhub.*` extensions

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.probe.icmp.duration_seconds` | `s` | Gauge | `url.full` *(optional — present for ping_webapp, absent for ping_gateway)* |
| `senhub.probe.icmp.packet_loss_ratio` | `1` | Gauge | `url.full` *(optional)* |

**Unit conversions applied by the mapper:** ms → s (÷1000) for latency; % → ratio (÷100) for packet loss.

ping_gateway vs ping_webapp: same metric name, but ping_gateway does **not** emit the `url.full` label — its target is the default gateway, detected at runtime.

### 4.6 `load_webapp` probe (HTTP phase timing)

**Primary source:** nothing in OTel applies directly (`http.client.*` is histogram-oriented over one-shot requests; our model is continuous, with averages)
**Secondary source:** [blackbox_exporter](https://github.com/prometheus/blackbox_exporter/blob/master/prober/http.go) — `probe_http_duration_seconds{phase=…}` with phases `resolve, connect, tls, processing, transfer`

#### 4.6.1 The `senhub.probe.http.*` extension

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.probe.http.duration_seconds` | `s` | Gauge | `phase`, `url.full` |

**`phase` values** (aligned with blackbox_exporter, plus a `total` extension):

| Value | Meaning |
|---|---|
| `resolve` | DNS resolution |
| `connect` | TCP establishment |
| `tls` | Handshake TLS |
| `processing` | Time To First Byte (TTFB) |
| `total` | Full request → full response duration *(extension — blackbox exposes `probe_duration_seconds` separately)* |

**Unit conversion:** ms → s (÷1000), by the mapper.

### 4.7 `wifi_signal_strength` probe (WiFi connectivity)

**Primary source:** none in OTel (no wifi semconv)
**Secondary source:** no established community convention

A full extension under the `senhub.system.network.wifi.*` namespace.

#### 4.7.1 `senhub.*` extensions

| Senhub metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.system.network.wifi.signal_strength.dbm` | `dBm` | Gauge | `senhub.network.wifi.ssid`, `senhub.network.wifi.bssid` |
| `senhub.system.network.wifi.quality_ratio` | `1` | Gauge | `senhub.network.wifi.ssid`, `senhub.network.wifi.bssid` *(÷100)* |

**Attributes:**

| Attribute | Source | Description |
|---|---|---|
| `senhub.network.wifi.ssid` | `ssid` tag | Network name (ESSID) |
| `senhub.network.wifi.bssid` | `bssid` tag | Access-point MAC address (BSSID) |

> There was no YAML transformer for `wifi_signal_strength` — created in batch 2.

### 4.8 `syslog` and `event` probes (log conduits)

**Nature:** these probes are **log conduits** — they collect and forward — not metric collectors. They receive events and logs and relay them to consumers (the SenHub cloud, OTLP log export, and so on). They are not sources of Prometheus signals.

> **Note, 2026-05-12:** the `otel` probe (OTLP reception) was removed from the registry — a stub implementation that was never finished. A full reimplementation (a real OTLP gRPC/HTTP server) is required before it comes back.

**Decision:** no business metric is exposed on the `/metrics` endpoint. This is declared explicitly with `otel.skip: true` in the YAML, to honour the "no metric without a mapping" contract — the skip IS an explicit mapping, documented and auditable.

#### 4.8.1 Schéma `otel.skip`

```yaml
otel:
  skip: true
  reason: "<mandatory explanation, for review>"
```

The Prometheus mapper ignores these metrics; they do not appear in `/metrics`. The `prtg:` / `nagios:` fields keep working (backwards compatibility).

#### 4.8.2 Future evolution — operational instrumentation

These probes could later be **instrumented** (a dedicated piece of work, outside the scope of the current OTel-first mapping) to expose their own **operational metrics**:

| Future candidate | Unit | Type |
|---|---|---|
| `senhub.probe.syslog.events_received` | `{event}` | Counter |
| `senhub.probe.syslog.events_dropped` | `{event}` | Counter |
| `senhub.probe.syslog.buffer_fill_ratio` | `1` | Gauge |
| `senhub.probe.event.events_received` | `{event}` | Counter |

These would require reworking the probe code to maintain internal counters. Separate work.

#### 4.8.3 Probes concerned

- **syslog**: the `syslog_event` metric is marked `skip: true`.
- **event**: YAML created, with the `event_event` metric marked `skip: true`.

### 4.9 `redfish` probe (server hardware monitoring)

**Primary source:** [OTel hardware namespace](https://opentelemetry.io/docs/specs/semconv/hardware/) — 16 categories (power_supply, physical_disk, logical_disk, disk_controller, enclosure, and others)
**Secondary source:** [jenningsloy318/redfish_exporter](https://github.com/jenningsloy318/redfish_exporter) (reference for the Prometheus pattern)

#### 4.9.1 Native OTel metrics used

| OTel metric | Unit | Type | How we use it |
|---|---|---|---|
| `hw.status` | `1` | UpDownCounter | Health, with `hw.type` ∈ {power_supply, physical_disk, logical_disk, disk_controller, enclosure} — expand pattern over `hw.state` |
| `hw.physical_disk.size` | `By` | UpDownCounter | Total drive capacity |
| `hw.logical_disk.limit` | `By` | UpDownCounter | Total volume capacity |
| `hw.logical_disk.usage` | `By` | UpDownCounter | Volume in use (allocated/free), with `hw.logical_disk.state` |
| `hw.logical_disk.utilization` | `1` | Gauge | Volume occupancy ratio |

**The `hw.state` attribute** — values emitted through the expansion:
- official OTel: `ok`, `degraded`, `failed`, `predicted_failure`
- **`unknown` extension** — for Redfish code 3 (Unknown), which has no standard OTel equivalent. An honest value: "Redfish could not determine the state".

**Mapping of the `sfs.redfish.health` lookup codes:**
- 0 (OK) → `hw.state=ok`
- 1 (Warning) → `hw.state=degraded`
- 2 (Critical) → `hw.state=failed`
- 3 (Unknown) → `hw.state=unknown` *(extension)*

#### 4.9.2 `senhub.*` extensions

Extensions created for concepts the official OTel hardware namespace does not cover:

| Senhub metric | Type | Reason |
|---|---|---|
| `senhub.hardware.physical_disk.has_active_operations` | Gauge bool | No OTel equivalent |
| `senhub.hardware.physical_disk.operation.progress_ratio` | Gauge `1` | No OTel equivalent |
| `senhub.hardware.physical_disk.link_speed` | Gauge `bit/s` | No OTel equivalent (Redfish exposes NegotiatedSpeed in Gbps; mapper ×1e9) |
| `senhub.hardware.physical_disk.location_indicator_active` | Gauge bool | No OTel equivalent |
| `senhub.hardware.physical_disk.block_size` | Gauge `By` | No OTel equivalent |
| `senhub.hardware.logical_disk.encrypted` | Gauge bool | No OTel equivalent |
| `senhub.hardware.logical_disk.io.operations` | Counter `{operation}` | No OTel logical_disk I/O (unlike `system.disk.operations`, which is host-level) |
| `senhub.hardware.logical_disk.io` | Counter `By` | Same — I/O bytes per volume |
| `senhub.hardware.storage.pool.*` | (multiple) | RAID pools — absent from the OTel hw.type taxonomy |
| `senhub.hardware.system.power_state` | UpDownCounter | Enum Redfish (Off/On/Powering On/Powering Off/Unknown) |
| `senhub.hardware.eventservice.status` | UpDownCounter | Redfish-specific |
| `senhub.hardware.redundancy.status` | UpDownCounter | Controller redundancy group |
| `senhub.hardware.redundancy.controllers.count` | UpDownCounter | Count, with `senhub.hardware.redundancy.bound` ∈ {active, min, max} |

#### 4.9.3 Attributes introduced

Aligned with OTel where possible (`hw.id`, `hw.name`, `hw.parent`, `hw.model`, `hw.serial_number`, `hw.physical_disk.type`, `hw.logical_disk.raid_level`, `hw.logical_disk.state`) and extended for the rest:

- `senhub.hardware.physical_disk.interface` — SAS/SATA/NVMe
- `senhub.hardware.physical_disk.slot` — slot number
- `senhub.hardware.enclosure.id` — enclosure identifier
- `senhub.hardware.disk_controller.slot` — controller slot
- `senhub.hardware.storage.pool.name` / `.id` / `.state` / `.raid_level`
- `senhub.hardware.redundancy.set` / `.state` / `.mode` / `.scope` / `.bound`

#### 4.9.4 Skipped metrics

- `hardware.storage.volume.io.total_ops` et `hardware.storage.volume.io.total_bytes` — redundant with reads+writes; skipped with a justification, since they are derivable in PromQL via `sum without(disk_io_direction)`.

### 4.10 `veeam` probe (backup & replication)

**Primary source:** no OTel convention for backup
**Secondary source:** [peekjef72/veeam_exporter](https://github.com/peekjef72/veeam_exporter) and community variants (converging patterns for job states and repo capacity)

**Decision:** every metric lives under `senhub.veeam.*` extensions, with systematic collapsing — totals and counts become state labels rather than separate metric names.

#### 4.10.1 `senhub.veeam.*` extensions

**Jobs (overview + detail):**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.jobs.total` | `{job}` | Gauge |
| `senhub.veeam.jobs.by_last_result` | `{job}` | Gauge (`senhub.veeam.job.last_result` attribute ∈ {success, warning, failed, running}) |
| `senhub.veeam.job.status` | `1` | UpDownCounter (**expand** `senhub.veeam.job.state` ∈ {none, success, warning, failed, running}) |
| `senhub.veeam.job.seconds_since_last_run` | `s` | Gauge |
| `senhub.veeam.job.objects` | `{object}` | Gauge |
| `senhub.veeam.job.bottleneck.status` | `1` | UpDownCounter (**expand** `senhub.veeam.job.bottleneck` ∈ {none, source, proxy, network, target}) |
| `senhub.veeam.job.last_run.bytes` | `By` | Gauge (`senhub.veeam.job.data_phase` attribute ∈ {processed, read, transferred}) |

**Repository:**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.repository.limit` | `By` | UpDownCounter |
| `senhub.veeam.repository.usage` | `By` | UpDownCounter (`senhub.veeam.repository.state` attribute ∈ {used, free}) |
| `senhub.veeam.repository.utilization` | `1` | Gauge (`senhub.veeam.repository.state` attribute ∈ {free}) |

**License:**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.license.status` | `1` | UpDownCounter (**expand** `senhub.veeam.license.state` ∈ {valid, expired, invalid}) |
| `senhub.veeam.license.days_remaining` | `{day}` | Gauge |
| `senhub.veeam.license.instances` | `{instance}` | Gauge (`senhub.veeam.license.instances_state` attribute ∈ {total, used, remaining}) |

**Proxies:**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.proxy.status` | `1` | UpDownCounter (**expand** `senhub.veeam.proxy.state` ∈ {disabled, offline, online}) |
| `senhub.veeam.proxies` | `{proxy}` | Gauge (attribut `senhub.veeam.proxies_state` ∈ {total, enabled, disabled}) |

**Protected objects :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.object.restore_points` | `{restore_point}` | Gauge |
| `senhub.veeam.object.last_run_failed` | `1` | Gauge bool |
| `senhub.veeam.objects` | `{object}` | Gauge (attribut `senhub.veeam.objects_state` ∈ {total, failed}) |

**Infrastructure (managed servers) :**
| Senhub metric | Unit | Type |
|---|---|---|
| `senhub.veeam.server.status` | `1` | UpDownCounter (**expand** `senhub.veeam.server.state` ∈ {unavailable, available}) |
| `senhub.veeam.servers` | `{server}` | Gauge (attribut `senhub.veeam.servers_state` ∈ {total, available, unavailable}) |

#### 4.10.2 Attributs (tag → attribute mapping)

| Tag interne | Attribut OTel |
|---|---|
| `job_name` | `senhub.veeam.job.name` |
| `job_type` | `senhub.veeam.job.type` |
| `repo_name` | `senhub.veeam.repository.name` |
| `proxy_name` | `senhub.veeam.proxy.name` |
| `object_name` | `senhub.veeam.object.name` |
| `object_type` | `senhub.veeam.object.type` |
| `server_name` | `senhub.veeam.server.name` |
| `server_type` | `senhub.veeam.server.type` |

#### 4.10.3 Récap

33 internal metrics → 20 unique OTel names, thanks to collapsing via labels. 5 metrics use the `expand` pattern for status enums (job, bottleneck, license, proxy, server).

### 4.11 Probe `citrix` (Virtual Apps and Desktops)

**Primary source:** no OTel convention for Citrix CVAD
**Secondary source:** no standard Prometheus exporter (Dynatrace, ControlUp and Nexthink are proprietary) — designed from scratch, consistent with our own conventions

Every metric lives under `senhub.citrix.*`, collapsed systematically by functional category.

#### 4.11.1 Extensions `senhub.citrix.*`

**Sessions :**
- `senhub.citrix.sessions.count` (gauge, `{session}`) + `senhub.citrix.session.state` ∈ {connected, disconnected}

**Machines (infrastructure) :**
- `senhub.citrix.machines.total` (gauge, `{machine}`) — total in the delivery group
- `senhub.citrix.machines.by_registration_state` (gauge, `{machine}`) + `senhub.citrix.machine.registration_state` ∈ {registered, unregistered, faulty, maintenance}

**Logon performance :**
- `senhub.citrix.logon.duration_1h_average` (gauge, `s`)
- `senhub.citrix.logon.last_session_duration` (gauge, `s`)
- `senhub.citrix.logon.sessions_opened` (gauge, `{session}`)
- `senhub.citrix.logon.phase_duration` (gauge, `s`) + `senhub.citrix.logon.phase` ∈ {brokering, vm_start, hdx, authentication, gpo, scripts, profile, interactive} — **8 phases collapsées**

**Connection failures :**
- `senhub.citrix.connection_failures.total` (gauge, `{failure}`)
- `senhub.citrix.connection_failures.by_category` (gauge, `{failure}`) + `senhub.citrix.connection_failure.category` ∈ {client_connection, configuration, machine, capacity_unavailable, licenses_unavailable, other}

**Load index (VDA utilisation) :**
- `senhub.citrix.load_index.ratio` (gauge, `1`) + `senhub.citrix.load_index.dimension` ∈ {effective, cpu, memory, disk, network, sessions} — **mapper ÷100**
- `senhub.citrix.machines.overloaded` (gauge, `{machine}`)

**License:**
- `senhub.citrix.license.sessions_active` (gauge, `{session}`)
- `senhub.citrix.license.peak_concurrent_users` (gauge, `{user}`)
- `senhub.citrix.license.unique_users` (gauge, `{user}`)
- `senhub.citrix.license.grace.sessions_remaining` (gauge, `{session}`)
- `senhub.citrix.license.grace.active` (gauge, `1`) bool
- `senhub.citrix.license.grace.time_remaining` (gauge, `s`) — **mapper ×3600** (heures → secondes)

**Machine fault states (Director) :**
- `senhub.citrix.machines.multi_session_fault_total` (gauge, `{machine}`) — distinct de `by_registration_state{faulty}` (source DDC vs Director)
- `senhub.citrix.machines.by_fault_state` (gauge, `{machine}`) + `senhub.citrix.machine.fault_state` ∈ {boot_failure, stuck_at_boot, unregistered, max_capacity, vm_not_found, unknown}

#### 4.11.2 Récap

**45 internal metrics → 19 OTel names**, collapsed by state, category and phase. No `expand` needed — there is no lookup-backed enum here, since each state is already its own data point.

Mapper-side conversions: `%` → ratio (÷100) for load_index; hours → seconds (×3600) for grace time remaining.

### 4.12 Probe `netscaler` (Citrix ADC)

**Primary source:** no OTel convention for NITRO/NetScaler
**Secondary source:** [citrix-adc-metrics-exporter officiel](https://github.com/netscaler/netscaler-adc-metrics-exporter) (`citrixadc_*` pattern) — transposé sous `senhub.netscaler.*`

A large scope (100 metrics) organised around **16 NITRO entities**:
system, ns, ssl (global), lbvserver, service, servicegroup, ssl.certificate, ha, disk, interface, cs (vserver+policy), gslb (vserver+site+service), cache, compression, aaa, vpn, appfw.

#### 4.12.1 OTel native utilisé

- `system.filesystem.usage` + `system.filesystem.utilization` for the **disk** metrics (the appliance's local partition). The `probe_type=netscaler` label tells them apart from the host OS filesystem metrics.

Nothing else is native OTel — NITRO has no semconv equivalent.

#### 4.12.2 Extensions `senhub.netscaler.*` — vue d'ensemble

Namespace structure:
- `senhub.netscaler.system.*` — CPU/mémoire/réseau/TCP/HTTP (global appliance)
- `senhub.netscaler.ns.*` — throughput global
- `senhub.netscaler.ssl.*` — SSL global et certificats
- `senhub.netscaler.lbvserver.*` / `.csvserver.*` / `.gslb.*` — load balancing
- `senhub.netscaler.service.*` / `.servicegroup.*` — backends
- `senhub.netscaler.interface.*` — interfaces réseau
- `senhub.netscaler.cache.*` / `.compression.*` — accélération
- `senhub.netscaler.aaa.*` / `.vpn.*` — auth et gateway
- `senhub.netscaler.appfw.*` — Web Application Firewall
- `senhub.netscaler.ha.*` — High Availability

#### 4.12.3 Metrics using `otel.expand` (11 states)

Every `state` enum (lbvserver, service, servicegroup, csvserver, gslbvserver, gslbsite, gslbservice, interface, aaa.vserver, vpn.vserver, ssl.certificate, ha.role, ha.node, ha.sync) — the common NITRO values:

**Vserver/service/servicegroup/cs/gslb** (`lbvserver.state` enum) :
1=down, 2=unknown, 3=busy, 4=out_of_service, 5=trofs, 7=up, 8=trofs_down

**Interface** : 0=disabled, 1=enabled
**SSL certificate** : 0=invalid, 1=valid
**HA role** : 0=unknown, 1=secondary, 2=primary
**HA node/sync** : 0=down/failed, 1=up/success

#### 4.12.4 Collapses majeurs

- **rx/tx** partout → `network.io.direction` ∈ {receive, transmit}
  - System network throughput (Mbps), packets.rate, packets (total counter)
  - Interface io (bytes total), throughput (Mbps), errors, packets.dropped
  - LB vserver throughput
  - HA heartbeat packets + rate
- **Cache hits/misses** → `senhub.netscaler.cache.lookups` + `senhub.netscaler.cache.lookup_result` ∈ {hit, miss}
- **Compression compressed/original bytes** → `senhub.netscaler.compression.bytes` + `senhub.netscaler.compression.bytes_type`
- **AAA auth successes/failures** → `senhub.netscaler.aaa.vserver.auth_attempts` + `senhub.netscaler.aaa.auth_result` ∈ {success, failure}
- **ServiceGroup members active/inactive** → `senhub.netscaler.servicegroup.members` + `senhub.netscaler.servicegroup.member_state`
- **CS policy hits/undefine_hits** → `senhub.netscaler.cspolicy.evaluations` + `senhub.netscaler.cspolicy.result` ∈ {hit, undefined}
- **AppFW requests/responses blocked** → `senhub.netscaler.appfw.blocked` + `senhub.netscaler.http.message_type`
- **AppFW violations by type** (sqli, xss, buffer_overflow) → `senhub.netscaler.appfw.violations.by_type`
- **CPU data/management plane** → `senhub.netscaler.system.cpu.utilization` + `senhub.netscaler.cpu.plane` ∈ {data, management}
- **HTTP requests/responses rates** → `senhub.netscaler.system.http.messages.rate` + `senhub.netscaler.http.message_type`
- **TCP client/server connections** → `senhub.netscaler.system.tcp.connections.active` + `senhub.netscaler.tcp.side`
- **NS throughput total/http** → `senhub.netscaler.ns.throughput` + `senhub.netscaler.traffic_type`

#### 4.12.5 Unit conversions by the mapper

- `%` → ratio (÷100) — CPU, memory, cache hit ratio, disk %, compression ratio
- `Mbits/s` / `Mbps` → `bit/s` (×1e6) — system/interface/ns throughput, link speed
- `KB` → `By` (×1024) — disk, cache memory
- `μs` → `s` (÷1e6) — gslb site RTT

#### 4.12.6 Récap

**100 métriques internes → ~65 noms OTel uniques** grâce aux collapses.
**11 metrics** use `otel.expand` for the NITRO enums.
**3 métriques disk** mappées à OTel native `system.filesystem.*`.
**~62 extensions** under `senhub.netscaler.*`, for the NITRO-specific domains.

### 4.13 Probes `mysql` / `postgresql` (databases)

**Sources principales :**
- [OTel Collector contrib — mysqlreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/mysqlreceiver) (convention de facto `mysql.*`)
- [OTel Collector contrib — postgresqlreceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/postgresqlreceiver) (convention de facto `postgresql.*`)
- [OTel Semantic Conventions — Database](https://opentelemetry.io/docs/specs/semconv/database/) (resource attrs `db.system.name`, `db.namespace`, `server.address`, `server.port`)

**Strategy:** treat the contrib receivers as canon, for drop-in interoperability with public Grafana dashboards and third-party tools. Extend under `senhub.db.<engine>.*` only when contrib lacks the metric, or under `senhub.db.*` (no engine prefix) when the semantics are cross-engine and identical.

OTel has no official semconv for server-side DB monitoring — the contrib receivers' `mysql.*` / `postgresql.*` are widely adopted de-facto conventions (Grafana Cloud, New Relic, and others).

#### 4.13.1 Resource attributes

Chaque export OTLP de probe DB ajoute (au-delà de `service.*` et `host.*` déjà émis) :

| Attribute | Value | Source |
|---|---|---|
| `db.system.name` | `"mysql"` ou `"postgresql"` | OTel semconv canonique |
| `server.address` | host du serveur DB | OTel semconv |
| `server.port` | port (3306 / 5432) | OTel semconv |
| `db.namespace` | default database (config) | OTel semconv |

The agent tag `probe_type=mysql\|postgresql` is still emitted as a metric attribute — it is universal to every SenHub probe.

#### 4.13.2 MySQL — métriques (32)

**Contrib mysql receiver utilisé tel quel (10) :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Threads running | `mysql.threads` | `{thread}` | gauge | `kind=running` |
| Threads connected | `mysql.threads` | `{thread}` | gauge | `kind=connected` |
| Aborted connections (clients + connects) | `mysql.connection.errors` | `{error}` | counter | `error=aborted_clients` ou `aborted_connects` (2 datapoints distincts) |
| Refused connections | `mysql.connection.errors` | `{error}` | counter | `error=max_connections` |
| Queries (Questions) | `mysql.query.count` | `{query}` | counter | (none) |
| Slow queries | `mysql.query.slow.count` | `{query}` | counter | (none) |
| Commands per verb | `mysql.commands` | `{command}` | counter | `command=select\|insert\|update\|delete\|replace` |
| Buffer pool dirty pages | `mysql.buffer_pool.data_pages` | `{page}` | gauge | `status=dirty` |
| Uptime | `mysql.uptime` | `s` | counter | (none) |
| Replica lag | `mysql.replica.time_behind_source` | `s` | gauge | (none) |

**Extensions `senhub.db.*` (cross-engine, 5) :**

| Metric | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Probe heartbeat (DB reachable) | `senhub.db.up` | `1` | gauge | (none) |
| Version banner | `senhub.db.version.info` | `1` | gauge | `db.system.version`=<str> |
| Connection idle (computed: connected−running) | `senhub.db.connection.idle` | `{connection}` | gauge | (none) |
| Connection utilization | `senhub.db.connection.utilization` | `1` | gauge | (none) — ratio threads_connected/max_connections |
| Database total size | `senhub.db.database.size` | `By` | gauge | (none) |

**Extensions `senhub.db.mysql.*` (12) :**

| Metric | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Max connections (gauge, distinct du counter contrib `mysql.connection.count`) | `senhub.db.mysql.connection.max` | `{connection}` | gauge | (none) |
| Transaction count | `senhub.db.mysql.transaction.count` | `{transaction}` | counter | `state=committed\|rolled_back` |
| Buffer pool hit ratio (dérivé reads/requests) | `senhub.db.mysql.buffer_pool.hit_ratio` | `1` | gauge | (none) |
| Buffer pool utilization (dérivé pages_data/pages_total) | `senhub.db.mysql.buffer_pool.utilization` | `1` | gauge | (none) |
| Cumulative deadlocks | `senhub.db.mysql.lock.deadlocks` | `{lock}` | counter | (none) — silently absent on MariaDB |
| Row locks waiting (gauge instantané) | `senhub.db.mysql.lock.waiting` | `{lock}` | gauge | (none) |
| Row lock wait time avg | `senhub.db.mysql.row_lock.time.avg` | `s` | gauge | (none) — **conversion ms→s** |
| IO bytes (read/write) | `senhub.db.mysql.io` | `By` | counter | `io.direction=read\|write` |
| Tmp tables disk ratio | `senhub.db.mysql.tmp_tables.disk_ratio` | `1` | gauge | (none) |
| Tables count | `senhub.db.mysql.table.count` | `{table}` | gauge | (none) |
| Replica IO thread running | `senhub.db.mysql.replica.io_thread.running` | `1` | gauge | (none) |
| Replica SQL thread running | `senhub.db.mysql.replica.sql_thread.running` | `1` | gauge | (none) |

**`senhub.db.*` replication extensions (3, shared with postgres):**

| Metric | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Role | `senhub.db.replication.role` | `1` | gauge | `role=primary\|replica\|standalone` (via `otel.expand`) |
| Composite health | `senhub.db.replication.health` | `1` | gauge | (none) |
| Replicas connected | `senhub.db.replication.replicas.connected` | `{replica}` | gauge | (none) |

#### 4.13.3 PostgreSQL — métriques (21)

**Contrib postgresql receiver utilisé tel quel (8) :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Backends by connection state | `postgresql.backends` | `{backend}` | gauge | `db.client.connection.state=active\|idle\|idle_in_transaction` |
| Max connections | `postgresql.connection.max` | `{connection}` | gauge | (none) |
| Commits cumulatif | `postgresql.commits` | `{transaction}` | counter | (none) |
| Rollbacks cumulatif | `postgresql.rollbacks` | `{transaction}` | counter | (none) |
| Deadlocks cumulatif | `postgresql.deadlocks` | `{deadlock}` | counter | (none) |
| Database size | `postgresql.db_size` | `By` | gauge | (none) |
| Tables count | `postgresql.table.count` | `{table}` | gauge | (none) |
| WAL replication lag (replay) | `postgresql.wal.lag` | `s` | gauge | `operation=replay` |

**Extensions `senhub.db.*` (6, cross-engine partagées) :** `senhub.db.up`, `senhub.db.version.info`, `senhub.db.connection.utilization`, `senhub.db.replication.role` + `.health` + `.replicas.connected` (idem mysql ci-dessus).

**Extensions `senhub.db.postgresql.*` (7) :**

| Metric | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Uptime | `senhub.db.postgresql.uptime` | `s` | gauge | (none) — `pg_postmaster_start_time` (contrib does not expose uptime) |
| Buffer hit ratio (dérivé blocks_hit/blocks_read) | `senhub.db.postgresql.buffer.hit_ratio` | `1` | gauge | (none) |
| Locks waiting (gauge instantané) | `senhub.db.postgresql.lock.waiting` | `{lock}` | gauge | (none) |
| Long-running transaction age (oldest active xact) | `senhub.db.postgresql.long_running_xact` | `s` | gauge | (none) |
| Archiver failures cumulatif | `senhub.db.postgresql.archiver.failed` | `{failure}` | counter | (none) |
| Archive freshness (age last_archived_wal) | `senhub.db.postgresql.archiver.last_archived.age` | `s` | gauge | (none) |
| Replica IO running (composite) | `senhub.db.postgresql.replica.io.running` | `1` | gauge | (none) |

#### 4.13.4 Collapses majeurs

Design decisions that reduce the number of distinct metrics by using attributes:

- **`mysql.threads{kind=running|connected}`** instead of two separate metrics. Idle (`connected − running`) is still emitted as the derived metric `senhub.db.connection.idle`, for sinks that cannot do arithmetic (PRTG, Nagios).
- **`mysql.connection.errors{error=aborted_clients|aborted_connects|max_connections}`** instead of three separate metrics. **More faithful than the previous implementation**, which summed `aborted_clients + aborted_connects` into a single `connections_aborted`.
- **`mysql.commands{command=…}`**: 5 series (select/insert/update/delete/replace) under one name. Bounded cardinality, no explosion.
- **`senhub.db.mysql.io{io.direction=read|write}`** instead of separate `_read_bytes` / `_write_bytes`. Aligned with the OTel semconv `io.direction` attribute, also used by `system.disk.io` and `system.network.io`.
- **`senhub.db.mysql.transaction.count{state=committed|rolled_back}`** instead of two metrics. **A deliberate asymmetry with postgres**: postgres exposes separate `postgresql.commits` + `postgresql.rollbacks` (the contrib canon) while mysql contrib has none, so we extend under `senhub.db.mysql.*` with an attribute.
- **`postgresql.backends{state=…}`** : 3 séries (active/idle/idle_in_transaction) sous un seul nom — pattern contrib.
- **`senhub.db.replication.role`** with `otel.expand`: 3 datapoints `role=primary|replica|standalone`, each 1 on a match and 0 otherwise. The strict OTel pattern for enums (see §2bis).

#### 4.13.5 Asymétries assumées entre mysql et postgres

| Concept | MySQL | PostgreSQL | Pourquoi |
|---|---|---|---|
| Commits/Rollbacks | `senhub.db.mysql.transaction.count{state}` | `postgresql.commits` + `postgresql.rollbacks` | Contrib postgres has two distinct metrics; contrib mysql has no tx metric — we follow each canon |
| Threads/Backends | `mysql.threads{kind}` | `postgresql.backends{state}` | Deux conventions différentes du contrib — attribut nommé différemment (kind vs state) |
| Lag de réplication | `mysql.replica.time_behind_source` | `postgresql.wal.lag{operation=replay}` | Sémantiques natives engine-specific |
| Uptime | `mysql.uptime` (counter) | `senhub.db.postgresql.uptime` (gauge) | Contrib postgres does not expose uptime; we derive it from `pg_postmaster_start_time`, which is logically a gauge |

Cross-engine queries go through the `db.system.name` resource attribute or the `probe_type` tag — not through a shared metric name.

#### 4.13.6 Récap

- **MySQL**: 27 active metrics (deadlocks absent on MariaDB), split into 10 contrib + 5 senhub-cross-db + 12 senhub-mysql.
- **PostgreSQL**: 21 active metrics, split into 8 contrib + 6 senhub-cross-db + 7 senhub-pg. `postgresql.backends` emits 3 series discriminated by `db.client.connection.state`.
- **3 métriques** utilisent `otel.expand` (`senhub.db.replication.role`).
- **No metric carries a unit suffix in its name** (ms/seconds/bytes/count) — the OTel rule, strictly observed.

## 5. Conventions — lot 4 complet

Every probe is mapped. Phase 0.5 is complete.

### 4.14 Probe `ibmi` (IBM i / Power Systems)

**Sources principales :**
- [IBM i Services — DB2 for i](https://www.ibm.com/docs/en/i/7.5?topic=services-system-supplied-routines-views) (the SYSIBM/QSYS2 tables and views the probe uses)
- [Lot 4 conventions internes](#412-probe-citrix) — `senhub.citrix.*`, `senhub.netscaler.*`, `senhub.veeam.*` comme modèle de namespace vendor-specific

**Strategy:** no canonical OTel convention exists for IBM i — a proprietary OS, not covered by the `opentelemetry-collector-contrib` receivers. The probe therefore namespaces all of its metrics under `senhub.ibmi.*`, on the same model as batch 4 (veeam/citrix/netscaler).

**Naming policy:** `senhub.ibmi.<family>.<measure>`. Families covered: `cpu`, `memory`, `asp`, `disk`, `job`, `jobs`, `job_queue`, `scheduled_job`, `subsystem`, `memory_pool`, `output_queue`, `spooled_file`, `user_storage`, `table`, `index_advisor`, `journal`, `journal_receiver`, `tcp`, `netstat`, `http_server`, `hardware`, `user_profile`, `sysval`, `library_list`, `license`, `ptf_group`, `watch`, `collector`. No unit suffix in the name (`.bytes`, `.seconds`, `.kb`, `.ms`, `.percent`) — the canonical OTel unit lives in `otel.unit`. No `.count` / `.total` suffix either — the `type` (counter vs gauge) carries that.

#### 4.14.1 Couverture (94 métriques, 90 OTel-mappées + 4 event-conduit skip)

**Système (CPU, mémoire, ASP, disque) — 9 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| CPU utilisation | `senhub.ibmi.cpu.utilization` | `1` | gauge | — |
| CPU configured count | `senhub.ibmi.cpu.configured` | `{cpu}` | gauge | — |
| CPU current capacity | `senhub.ibmi.cpu.capacity` | `{cpu}` | gauge | — |
| Main storage | `senhub.ibmi.memory.main_storage` | `By` | gauge | — |
| System ASP utilisation | `senhub.ibmi.asp.system.utilization` | `1` | gauge | — |
| ASP utilisation (per-ASP) | `senhub.ibmi.asp.utilization` | `1` | gauge | `ibmi.asp.number`, `ibmi.asp.type` |
| ASP capacity | `senhub.ibmi.asp.capacity` | `By` | gauge | `ibmi.asp.number` |
| ASP threshold | `senhub.ibmi.asp.threshold` | `1` | gauge | `ibmi.asp.number` |
| Disk utilisation | `senhub.ibmi.disk.utilization` | `1` | gauge | `ibmi.disk.unit`, `ibmi.disk.device` |
| Disk bytes read | `senhub.ibmi.disk.read` | `By` | counter | `ibmi.disk.unit` |

**Jobs (aggregate + per-job top-N) — 17 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Total jobs | `senhub.ibmi.jobs.total` | `{job}` | gauge | — |
| Active jobs | `senhub.ibmi.jobs.active` | `{job}` | gauge | — |
| Jobs by status | `senhub.ibmi.jobs.by_status` | `{job}` | gauge | `ibmi.job.type`, `ibmi.job.status` |
| Jobs by subsystem | `senhub.ibmi.jobs.by_subsystem` | `{job}` | gauge | `ibmi.subsystem` |
| Top-N cap hit flag | `senhub.ibmi.jobs.topn_cap_hit` | `1` | gauge | — |
| Per-job CPU utilisation | `senhub.ibmi.job.cpu.utilization` | `1` | gauge | `ibmi.job.name`, `ibmi.job.user`, `ibmi.subsystem` |
| Per-job elapsed CPU | `senhub.ibmi.job.cpu.elapsed_time` | `s` | gauge | id. |
| Per-job cumulative CPU | `senhub.ibmi.job.cpu.cumulative_time` | `s` | counter | id. |
| Per-job CPU delta | `senhub.ibmi.job.cpu.delta_time` | `s` | gauge | `ibmi.job.name` |
| Per-job CPU rate | `senhub.ibmi.job.cpu.rate` | `1` | gauge | `ibmi.job.name` (value_scale 0.001 : ms/s → ratio) |
| Per-job temp storage | `senhub.ibmi.job.temp_storage` | `By` | gauge | `ibmi.job.name` |
| Per-job disk I/O | `senhub.ibmi.job.disk.io` | `{operation}` | counter | `ibmi.job.name` |
| Per-job disk I/O (elapsed) | `senhub.ibmi.job.disk.elapsed_io` | `{operation}` | gauge | `ibmi.job.name` |
| Per-job page faults | `senhub.ibmi.job.page_faults` | `{fault}` | gauge | `ibmi.job.name` |
| Per-job threads | `senhub.ibmi.job.threads` | `{thread}` | gauge | `ibmi.job.name` |
| Per-job priority | `senhub.ibmi.job.priority` | `1` | gauge | `ibmi.job.name` |
| Subsystem active jobs | `senhub.ibmi.subsystem.active_jobs` | `{job}` | gauge | `ibmi.subsystem` |

**Job queues & scheduled — 8 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Job queue — active | `senhub.ibmi.job_queue.active` | `{job}` | gauge | `ibmi.queue.library`, `ibmi.queue.name` |
| Job queue — held | `senhub.ibmi.job_queue.held` | `{job}` | gauge | id. |
| Job queue — released | `senhub.ibmi.job_queue.released` | `{job}` | gauge | id. |
| Job queue — scheduled | `senhub.ibmi.job_queue.scheduled` | `{job}` | gauge | id. |
| Job queue — depth | `senhub.ibmi.job_queue.depth` | `{job}` | gauge | id. |
| Non-empty queues | `senhub.ibmi.job_queue.nonempty` | `{queue}` | gauge | — |
| Scheduled jobs count | `senhub.ibmi.scheduled_job.count` | `{job}` | gauge | — |
| Scheduled last-run age | `senhub.ibmi.scheduled_job.last_run_age` | `s` | gauge | `ibmi.job.name` |

**Memory pools — 3 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Pool size | `senhub.ibmi.memory_pool.size` | `By` | gauge | `ibmi.pool.id`, `ibmi.pool.name` |
| Pool current threads | `senhub.ibmi.memory_pool.threads` | `{thread}` | gauge | id. |
| Pool ineligible threads | `senhub.ibmi.memory_pool.ineligible_threads` | `{thread}` | gauge | id. |

**Spool & user storage — 8 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Output queue files | `senhub.ibmi.output_queue.files` | `{file}` | gauge | `ibmi.queue.library`, `ibmi.queue.name` |
| Output queue spooled total | `senhub.ibmi.output_queue.spooled_files` | `{file}` | gauge | — |
| Spooled file count | `senhub.ibmi.spooled_file.count` | `{file}` | gauge | — |
| Spooled file oldest age | `senhub.ibmi.spooled_file.oldest_age` | `s` | gauge | — |
| User storage used | `senhub.ibmi.user_storage.used` | `By` | gauge | `ibmi.user.name`, `ibmi.asp.number` |
| User storage quota | `senhub.ibmi.user_storage.quota` | `By` | gauge | id. |
| User storage utilisation | `senhub.ibmi.user_storage.utilization` | `1` | gauge | id. |
| Users over 80% quota | `senhub.ibmi.user_storage.over_threshold` | `{user}` | gauge | — |

**Database — tables & index advisor — 9 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Rows | `senhub.ibmi.table.rows` | `{row}` | gauge | `ibmi.table.schema`, `ibmi.table.name` |
| Logical reads | `senhub.ibmi.table.logical_reads` | `{read}` | counter | id. |
| Updates | `senhub.ibmi.table.updates` | `{update}` | counter | id. |
| Deleted rows | `senhub.ibmi.table.deleted_rows` | `{row}` | gauge | id. |
| Index times-advised | `senhub.ibmi.index_advisor.times_advised` | `{advisory}` | counter | + `ibmi.table.key_columns` |
| Index MTI used | `senhub.ibmi.index_advisor.mti_used` | `{use}` | counter | id. |
| Index avg query estimate | `senhub.ibmi.index_advisor.avg_query_estimate` | `s` | gauge | id. |
| Index advised total | `senhub.ibmi.index_advisor.advised_indexes` | `{index}` | gauge | — |
| Index recent advisories (1h) | `senhub.ibmi.index_advisor.recent_advisories` | `{advisory}` | gauge | — |

**Journals — 5 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Journal active flag | `senhub.ibmi.journal.active` | `1` | gauge | `ibmi.journal.name`, `ibmi.journal.library` |
| Receivers total size | `senhub.ibmi.journal.receivers_size` | `By` | gauge | id. |
| Remote lag (estimé) | `senhub.ibmi.journal.remote_lag` | `s` | gauge | id. |
| Receiver size | `senhub.ibmi.journal_receiver.size` | `By` | gauge | `ibmi.receiver.name`, `ibmi.receiver.library` |
| Attached receivers count | `senhub.ibmi.journal_receiver.attached` | `{receiver}` | gauge | — |

**Réseau (TCP, netstat, HTTP server) — 11 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| TCP connections established | `senhub.ibmi.tcp.connections.established` | `{connection}` | gauge | — |
| Netstat connections total | `senhub.ibmi.netstat.connections` | `{connection}` | gauge | — |
| Connections by state | `senhub.ibmi.netstat.connections_by_state` | `{connection}` | gauge | `ibmi.tcp.state` |
| Listener up | `senhub.ibmi.netstat.listener.up` | `1` | gauge | `ibmi.net.local_port`, `ibmi.net.port_name`, `network.transport` |
| Listener jobs | `senhub.ibmi.netstat.listener.jobs` | `{job}` | gauge | `ibmi.net.local_port`, `ibmi.net.port_name` |
| Listeners total | `senhub.ibmi.netstat.listeners` | `{listener}` | gauge | — |
| Interface up | `senhub.ibmi.netstat.interface.up` | `1` | gauge | `ibmi.net.address`, `ibmi.net.line_description` |
| Interface MTU | `senhub.ibmi.netstat.interface.mtu` | `By` | gauge | `ibmi.net.address` |
| HTTP active threads | `senhub.ibmi.http_server.threads.active` | `{thread}` | gauge | `ibmi.http.server_name` |
| HTTP idle threads | `senhub.ibmi.http_server.threads.idle` | `{thread}` | gauge | id. |
| HTTP responses | `senhub.ibmi.http_server.responses` | `{response}` | counter | id. |

**Hardware — 3 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Hardware count (by category & status) | `senhub.ibmi.hardware.count` | `{resource}` | gauge | `ibmi.hardware.category`, `ibmi.hardware.status` |
| Hardware total | `senhub.ibmi.hardware.total` | `{resource}` | gauge | — |
| Non-operational hardware | `senhub.ibmi.hardware.non_operational` | `{resource}` | gauge | — |

**Sécurité (users, sysval) — 6 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Users total | `senhub.ibmi.user_profile.count` | `{user}` | gauge | — |
| Users by status | `senhub.ibmi.user_profile.by_status` | `{user}` | gauge | `ibmi.user.status` |
| Users by class | `senhub.ibmi.user_profile.by_class` | `{user}` | gauge | `ibmi.user.class` |
| Users with failed signons | `senhub.ibmi.user_profile.failed_signons` | `{user}` | gauge | — |
| QSECURITY level | `senhub.ibmi.sysval.security_level` | `1` | gauge | — |
| QAUDLVL level | `senhub.ibmi.sysval.audit_level` | `1` | gauge | — |

**Configuration & compliance (library, license, PTF, watch) — 7 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Library list position | `senhub.ibmi.library_list.position` | `1` | gauge | `ibmi.library.name`, `ibmi.library.type` |
| Licensed users | `senhub.ibmi.license.licensed_users` | `{user}` | gauge | `ibmi.license.product_id`, `ibmi.license.feature_id` |
| License usage limit | `senhub.ibmi.license.usage_limit` | `1` | gauge | id. |
| PTF group installed | `senhub.ibmi.ptf_group.installed` | `1` | gauge | `ibmi.ptf.group` |
| PTF group level | `senhub.ibmi.ptf_group.level` | `1` | gauge | `ibmi.ptf.group` |
| Watch session active | `senhub.ibmi.watch.session_active` | `1` | gauge | `ibmi.watch.session_id`, `ibmi.watch.program` |

**Self-observability (collector health) — 4 métriques :**

| Notre métrique | OTel name | Unit | Type | Attributes |
|---|---|---|---|---|
| Collector success total | `senhub.ibmi.collector.success` | `{collection}` | counter | `ibmi.collector` |
| Collector failure total | `senhub.ibmi.collector.failure` | `{collection}` | counter | `ibmi.collector` |
| Collector last duration | `senhub.ibmi.collector.last_duration` | `s` | gauge | `ibmi.collector` |
| Collector last success ts | `senhub.ibmi.collector.last_success_timestamp` | `s` | gauge | `ibmi.collector` |

**Event-conduit (skip OTel, log export V2) — 4 métriques :**

`ibmi.message_queue.event` (QSYSOPR), `ibmi.history_log.event` (QHST), `ibmi.audit_journal.event` (QAUDJRN), `ibmi.msgw_job.event` (job in message wait) all carry `otel.skip: true` with an explicit reason. Same policy as `syslog`/`event` (§4.8): these are relayed-event markers, not metrics that aggregate meaningfully on the Prom/OTLP channel. V2 target: OTLP log export.

#### 4.14.2 Conventions d'attributs

The probe cache's tags are renamed to clean OTel keys via `tag_to_attribute`. Every key is prefixed `ibmi.*` except `network.transport`, which is the canonical OTel attribute for `tcp`/`udp`. The table above lists the resulting attributes. Cache discrimination (`DiscriminantTagsRegistry["ibmi"]` in `http_cache.go`) keeps the original tag names — only the OTel/Prometheus output sees the renamed version.

#### 4.14.3 Conversions d'unités

All converted automatically by `otelmapper/convert.go`:

- `%` → `1` (÷100)
- `KB` → `By` (×1024)
- `MB` → `By` (×1048576)
- `B` → `By` (no scale)
- `ms` → `s` (÷1000)
- `s` → `s` (no conversion)

One exception: `ibmi.job.cpu_time_ms_rate_per_sec` carries `unit: "ms/s"` on the probe side, which is not canonical; the mapping uses an explicit `value_scale: 0.001` to produce a dimensionless ratio on the OTel side.

### 4.15 Probe `linux_logs` (systemd journal → OTLP logs)

**Sources principales :**
- [OTel Semantic Conventions — General Logs](https://opentelemetry.io/docs/specs/semconv/general/logs/) (resource & log record attrs)
- [OTel Semantic Conventions — Process](https://opentelemetry.io/docs/specs/semconv/attributes-registry/process/) (`process.pid`, `process.executable.name`, `process.owner.uid`)
- [OTel Logs Data Model §4.2](https://opentelemetry.io/docs/specs/otel/logs/data-model/) (SeverityNumber + SeverityText)
- [RFC 5424 §6.2.1 PRI](https://datatracker.ietf.org/doc/html/rfc5424#section-6.2.1) (syslog severity 0..7)

**Strategy:** `linux_logs` is **exclusively a producer on the logs signal**. It emits no metric DataPoint (`Collect()` returns `nil, nil`), so there is no YAML transformer — the log record's shape is already OTel by construction, and the mapping lives in `internal/agent/probes/linuxlogs/journal_reader.go::parseEntry`. Records flow `journalctl JSON → LogRecord → agentstate.LogChannel → OTLP logsPump → OTel SDK Logger → BatchProcessor → OTLP gRPC export` (typiquement vers VictoriaLogs, Loki, ou un OpenTelemetry Collector).

#### 4.15.1 Attributs OTel-canoniques produits

Each record carries the attributes below, read from the JSON of `journalctl --output=json --follow`:

| Attribute OTel | Source journalctl | Notes |
|---|---|---|
| `host.name` | `_HOSTNAME` | resource attr canonique |
| `systemd.unit` | `_SYSTEMD_UNIT` | Canonical OTel attribute for the systemd service |
| `syslog.appname` | `SYSLOG_IDENTIFIER` | OTel attr canonique (équivalent du `appname` RFC 5424) |
| `process.pid` | `_PID` | OTel attr canonique |
| `process.executable.name` | `_COMM` | OTel attr canonique |
| `process.owner.uid` | `_UID` | `process.owner.*` extension — not canonical yet, but consistent with the OTel `process.*` namespace |
| `systemd.transport` | `_TRANSPORT` | extension `systemd.*` (journalctl-spécifique : `kernel`, `stdout`, `syslog`, `journal`, …) |
| `senhub.probe.name` | (poseur framework) | nom de l'instance probe configurée |
| `senhub.probe.type` | `"linux_logs"` (constant) | Universal to every SenHub probe on OTLP |

Every attribute emitted follows the OTel `<namespace>.<key>` nomenclature — no `senhub.linux_logs.*` on the record; see [§5 Logs signal](#logs-signal--convention-otel-respectée)).

#### 4.15.2 Body & timestamp

- **Body** = `MESSAGE` du journal (string).
- **Timestamp** = `__REALTIME_TIMESTAMP` parsé en µs → `time.Time` (UTC). Fallback `time.Now()` si parsing échoue (préférable à un drop).
- **ObservedTimestamp** = identical to Timestamp (the probe consumes `--follow` in real time).

#### 4.15.3 Severity mapping (RFC 5424 → OTel)

The `agentstate.SyslogPriorityToSeverity` helper is shared with `syslog` and `event` — the event probe accepts textual severities, but the numeric result is identical:

| PRI | RFC 5424 | OTel SeverityNumber | OTel SeverityText |
|---:|---|---:|---|
| 0 | Emergency | 24 (FATAL4) | `FATAL4` |
| 1 | Alert     | 23 (FATAL3) | `FATAL3` |
| 2 | Critical  | 22 (FATAL2) | `FATAL2` |
| 3 | Error     | 17 (ERROR)  | `ERROR`  |
| 4 | Warning   | 13 (WARN)   | `WARN`   |
| 5 | Notice    | 10 (INFO2)  | `INFO2`  |
| 6 | Info      |  9 (INFO)   | `INFO`   |
| 7 | Debug     |  5 (DEBUG)  | `DEBUG`  |

Out-of-range → `SeverityUnspecified` (0), `SeverityText` vide. Résilient aux records malformés.

#### 4.15.4 Filtering on the probe side (not the OTel side)

`linux_logs` accepte côté config :
- `units: ["nginx.service", "ssh.service"]` → flag `journalctl --unit=…`
- `identifiers: ["sshd", "kernel"]` → flag `journalctl --identifier=…`
- `priority: 4` → the `journalctl --priority=…` flag (filtered by the journal itself, never crossing the pipe)
- `include_boot: false` (default) → only records arriving after `OnStart` are emitted

Filtering therefore happens upstream: a record outside the probe's scope never touches the OTLP channel. Any further filtering downstream is the OTLP consumer's job (collector, or a VictoriaLogs ingest filter).

#### 4.15.5 No metric signal, by design

`linux_logs` has no `definitions/linux_logs.yaml` and emits no DataPoint. This **differs from `syslog` and `event`** (§4.8), which emit one synthetic DataPoint per relayed event for PRTG / Nagios backwards compatibility (`syslog_event`, `event_event`, both marked `otel.skip: true`). `linux_logs` arrived after that policy, purely as a logs producer — there is no synthetic PRTG channel to maintain.

Consequence: a typical `linux_logs` deployment needs the OTLP logs export enabled on the storage (`storage[otlp].signals.logs: true`); otherwise the records are published and consumed by nobody.

### 4.16 Probe `windows_eventlog` (Windows Event Log → OTLP logs)

**Sources principales :**
- [OTel Semantic Conventions — General Logs](https://opentelemetry.io/docs/specs/semconv/general/logs/) (resource & log record attrs)
- [OTel Logs Data Model §4.2](https://opentelemetry.io/docs/specs/otel/logs/data-model/) (SeverityNumber + SeverityText)
- [Windows Event Schema](https://learn.microsoft.com/windows/win32/wes/eventschema-schema) (the XML shape rendered by `EvtRender`)
- [wevtapi `EvtSubscribe`](https://learn.microsoft.com/windows/win32/api/winevt/nf-winevt-evtsubscribe) (modèle pull + bookmark)

**Strategy:** the Windows counterpart of `linux_logs`. **Exclusively a producer on the logs signal**: no metric DataPoint (`Collect()` returns `nil, nil`), so no YAML transformer. The mapping lives in `internal/agent/probes/windowseventlog/event_xml.go::toLogRecord`. Flow: `wevtapi EvtSubscribe → EvtRender(EventXml) → parseEventXML → LogRecord → agentstate.LogChannel → OTLP logsPump → OTel gRPC export`. Windows-only; on other operating systems the probe registers but `OnStart` fails explicitly (the `subscription_other.go` stub), exactly as `linux_logs` does off Linux.

#### 4.16.1 Attributs produits

The record carries the keys mandated by issue #154, plus canonical OTel attributes wherever an equivalent exists:

| Attribute | Source (Event XML) | Notes |
|---|---|---|
| `event_id` | `System/EventID` | clé mandatée #154 |
| `event_level` | `System/Level` → label | clé mandatée #154 (Critical/Error/Warning/Information/Verbose) |
| `event_channel` | `System/Channel` | clé mandatée #154 |
| `event_provider` | `System/Provider/@Name` | clé mandatée #154 |
| `event_source` | `System/Provider/@Name` | clé mandatée #154 (alias de provider, parité PRTG) |
| `record_id` | `System/EventRecordID` | clé mandatée #154 |
| `host.name` | `System/Computer` | resource attr OTel canonique |
| `process.pid` | `System/Execution/@ProcessID` | OTel attr canonique |
| `user.id` | `System/Security/@UserID` | SID ; omis si `redact_pii: true` |
| `eventdata.<Name>` | `EventData/Data` | payload structuré ; champs sensibles masqués en mode PII |
| `senhub.probe.name` / `senhub.probe.type` | (framework) | `senhub.probe.type = "windows_eventlog"` |

#### 4.16.2 Body & timestamp

- **Body** = `RenderingInfo/Message`, the message rendered by the provider. Fallback when absent (the message DLL is not installed): `"<Provider> event <EventID>: k=v, …"`, built from the sorted `EventData`.
- **Timestamp** = `System/TimeCreated/@SystemTime` (RFC 3339 nano) → `time.Time`. Fallback `time.Now()` si parsing échoue.

#### 4.16.3 Severity mapping (Windows Level → OTel)

| Level | Windows | OTel SeverityNumber | SeverityText |
|---:|---|---:|---|
| 1 | Critical | 21 (FATAL) | `Critical` |
| 2 | Error | 17 (ERROR) | `Error` |
| 3 | Warning | 13 (WARN) | `Warning` |
| 4 | Information | 9 (INFO) | `Information` |
| 5 | Verbose | 5 (DEBUG) | `Verbose` |
| 0 | LogAlways | 9 (INFO) | `Information` |

#### 4.16.4 Filtrage côté probe

`levels:` is pre-filtered at the source through a wevtapi XPath query (`*[System[(Level=1 or Level=2)]]`); `include_event_ids` / `exclude_event_ids` (exclude wins) and `sources` (a case-insensitive provider glob) are applied in a second pass in Go. An event outside the scope never touches the OTLP channel.

#### 4.16.5 Bookmark & RGPD

- **Bookmark**: one wevtapi bookmark per channel, persisted as JSON (`bookmark_path`) through an atomic write. On restart the subscription resumes with `StartAfterBookmark` — no duplication, no loss. Without `bookmark_path`, it tails from now on every start.
- **GDPR**: `redact_pii: true` masks sensitive `EventData` fields — Security-channel logons such as `TargetUserName`, `IpAddress`, SIDs — and replaces the Security body with a marker. Enable it when collecting the `Security` channel.

#### 4.16.6 No metric signal, by design

As with `linux_logs`: no `definitions/windows_eventlog.yaml`, no DataPoint. It needs the OTLP logs export enabled (`storage[otlp].signals.logs: true`) for the records to be consumed.

### 4.17 Probe `filetail` (tail de fichiers plats → OTLP logs)

**Sources principales :**
- [OTel Semantic Conventions — General Logs](https://opentelemetry.io/docs/specs/semconv/general/logs/)
- [OTel Logs Data Model §4.2](https://opentelemetry.io/docs/specs/otel/logs/data-model/) (SeverityNumber + SeverityText)
- [OTel `log.file.*` attributes](https://opentelemetry.io/docs/specs/semconv/attributes-registry/log/) (`log.file.path`)

**Strategy:** generic and cross-platform, the flat-file counterpart of `linux_logs`/`windows_eventlog`. **Exclusively a producer on the logs signal** (`Collect()` → `nil, nil`, no YAML transformer). Mapping in `internal/agent/probes/filetail/parser.go::parseLine`. Flow: `github.com/nxadm/tail (rotation/reopen) → assemblage multiline → parser (regex/json/logfmt/raw) → LogRecord → agentstate.LogChannel → OTLP logs`.

#### 4.17.1 Attributs produits

| Attribute | Source | Notes |
|---|---|---|
| `log.file.path` | chemin du fichier tailé | attr OTel canonique `log.file.*` |
| `<champ parsé>` | groupe nommé regex / clé JSON / clé logfmt | chaque champ extrait devient un attribut |
| `senhub.probe.name` / `senhub.probe.type` | framework | `senhub.probe.type = "filetail"` |

#### 4.17.2 Body, severity, timestamp

- **Body** = the `message`/`msg`/`body` field when a structured parser extracted one, otherwise the raw line.
- **Severity** = champ `level`/`severity`/`lvl` mappé (TRACE/DEBUG/INFO/WARN/ERROR/FATAL, insensible casse) via `severityFromText`.
- **Timestamp** = `parser.timestamp_field` parsed with `timestamp_format` (falling back to common layouts and unix epoch); otherwise the instant the line was read.

#### 4.17.3 Parsers

`regex` (named groups, at least one required), `json` (jsonl; a non-object line is skipped and logged), `logfmt` (key=value), `raw` (the whole line as the body — the default). Multiline folds stack traces (`match: after`/`before`).

#### 4.17.4 Rotation, bookmark, identité fichier

Rotation is handled by nxadm/tail (reopen). `bookmark_path` persists the per-file offset (atomically, every ~2 s and on shutdown), so a restart resumes without loss or duplication. Identity uses a fingerprint (CRC32 of the first 1000 bytes) that is **only stable from 1000 bytes onwards**; below that the fingerprint is "" — unstable, because the head changes as the file grows — and identity falls back to an offset/size comparison. Otherwise a small file that grows would be re-read from 0 on restart, duplicating its content.

#### 4.17.5 No metric signal, by design

As with `linux_logs`/`windows_eventlog`: no `definitions/filetail.yaml`, no DataPoint. Requires `storage[otlp].signals.logs: true`.

### 4.18 Probe `otlp_receiver` (collecteur edge OTLP entrant → sinks)

**Sources principales :**
- [OTLP MetricsService](https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/collector/metrics/v1/metrics_service.proto)
- [OTLP Metrics Data Model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/)

**Strategy:** the agent as an **edge collector**. An event-driven probe (the `ProbeWithCallback` contract, like `syslog`) that opens an OTLP gRPC or HTTP server, decodes incoming metrics into internal DataPoints, and pushes them to the data_store and on to every sink. Code: `internal/agent/probes/otlpreceiver/` (`grpc_server.go`, `http_server.go`, `decode.go`).

#### 4.18.1 Décodage (decode.go)

- **Gauge** + **Sum** number datapoints → un DataPoint scalaire chacun, **nom OTel conservé tel quel** (ex. `system.cpu.utilization`).
- Resource attributes and datapoint attributes are folded into tags (the datapoint wins on a collision).
- **Histogram / ExponentialHistogram / Summary**: no scalar value, so they are not ingested — they are counted and reported back to the sender through `PartialSuccess.rejected_data_points`.

#### 4.18.2 Pass-through mapper (clé de l'intégration)

Every ingested DataPoint carries the tag **`metric_type=otlp_ingest`** (the `otelmapper.MetricTypeOTLPIngest` constant) plus `probe_name`/`probe_type=otlp_receiver`. Because incoming metrics are **already OTel-shaped** — arbitrary external names, no transformer definition possible — `otelmapper.Resolve` detects them by that marker and **passes them straight through** as an `OtelRecord` (name, value and unit as they are, type `gauge`) **without** a definition lookup. Without that pass-through the OTLP and Prometheus exporters would drop these metrics (def==nil) and they would reach only the http cache. The marker is neutral, with no coupling to the probe package, per the "otelmapper stays neutral" rule.

#### 4.18.3 Limites

Re-exported as `gauge` — the incoming gauge/sum distinction is not preserved on the flat DataPoint bus. Histograms and summaries are not ingested. Free tier.

### 4.19 Probe `snmp_trap` (récepteur de traps SNMP → OTLP logs)

**Sources principales :**
- [SNMPv2-MIB (RFC 3418)](https://datatracker.ietf.org/doc/html/rfc3418) — traps génériques + snmpTrapOID.0 / sysUpTime.0
- [OTel Logs Data Model §4.2](https://opentelemetry.io/docs/specs/otel/logs/data-model/)
- gosnmp `TrapListener` (réutilisé de snmp_poll #156)

**Strategy:** the push counterpart of `snmp_poll`. An event-driven probe that listens for v2c/v3 traps over UDP, decodes them with gosnmp, and publishes each trap as an **OTel log** on `agentstate.PublishLog` (logs-only, like `linux_logs`/`syslog` — `Collect()` → `nil`, no YAML transformer). Code: `internal/agent/probes/snmptrap/` (`snmptrap_probe.go` listener, `traps.go` décodage).

#### 4.19.1 Attributs produits

| Attribute | Source | Notes |
|---|---|---|
| `trap_oid` | valeur de `snmpTrapOID.0` (1.3.6.1.6.3.1.1.4.1.0) | clé mandatée #161 |
| `trap_name` | compiled table of the 6 generic traps, otherwise `unknown` | key mandated by #161 |
| `source_ip` | `*net.UDPAddr` de l'émetteur | clé mandatée #161 |
| `snmp_version` | v1/v2c/v3 | |
| `sysuptime` | `sysUpTime.0` | |
| `varbind.<oid>` | one per binding (excluding snmpTrapOID/sysUpTime) | formatted value |
| `senhub.probe.name` / `senhub.probe.type` | framework | `senhub.probe.type = "snmp_trap"` |

#### 4.19.2 Severity & body

- **Severity**: a fixed heuristic, since a trap carries no severity field. `linkDown`/`authenticationFailure`/`egpNeighborLoss` → WARN; everything else → INFO.
- **Body** : `SNMP trap <name> (<oid>) from <ip> with N varbind(s)`.

#### 4.19.3 Résolution de noms (MIBs LOCALES, jamais fetchées)

Two layers: (1) a compiled table of the 6 generic SNMPv2-MIB traps; (2) **local MIBs supplied by the operator** through `mib_paths`, parsed at startup by the shared `internal/agent/services/snmpmib/` package (built on `gosmi`), which resolves both `trap_oid` AND the varbind OIDs (`varbind.ifOperStatus.3` rather than the numeric form). The key distinction: **never a network fetch** — only the local files the operator placed there, since fetching at runtime from a URL is the documented anti-pattern. An OID with no MIB loaded stays numeric (`trap_name=unknown`). `snmpmib` is reusable by the other SNMP probes (snmp_poll, batch 2).

#### 4.19.4 Limites

v3 USM is best-effort — the gosnmp listener holds a single USM identity, and v3 traps are flagged unreliable upstream; v2c is solid. Port 162 is privileged → root or CAP_NET_BIND_SERVICE (#223). Free tier.

### 4.20 icmp_check (free, #299)

No otelcol-contrib receiver covers active ICMP, hence the `senhub.icmp.*` namespace. One series per target (`icmp.target` + `icmp.target.ip` attributes).

| OTel metric | Unit | Type | Source wire |
|---|---|---|---|
| `senhub.icmp.up` | `1` | gauge | reachability du cycle (≥1 réponse) |
| `senhub.icmp.packet_loss` | `1` | gauge | wire en % → ratio côté mapper |
| `senhub.icmp.packets.sent` / `.received` | `{packet}` | gauge | comptes du cycle |
| `senhub.icmp.rtt.min/.avg/.max/.stddev` | `s` | gauge | wire en ms, `value_scale: 0.001` ; émis seulement si ≥1 réponse |

Privileged mode (raw ICMP) and unprivileged mode (datagram, via the `ping_group_range` sysctl on Linux); privileged is the default on Windows only. The multi-target chassis is reusable by tcp_dial (#159) and dns_latency (#158).


### 4.21 http_check (free, #300)

Aligned with the otelcol-contrib httpcheck receiver wherever the metric exists (`httpcheck.duration`); `senhub.httpcheck.*` extensions otherwise. One series per target (`httpcheck.target` attribute).

| OTel metric | Unit | Type | Source wire |
|---|---|---|---|
| `senhub.httpcheck.up` | `1` | gauge | statut attendu (+ content_match) |
| `senhub.httpcheck.status.code` | `{code}` | gauge | HTTP code (annotation unit: no `_ratio` suffix on the Prometheus side) |
| `httpcheck.duration` | `s` | gauge | total, wire ms `value_scale: 0.001` (nom contrib) |
| `senhub.httpcheck.duration.{dns,connect,tls,ttfb}` | `s` | gauge | phases httptrace, wire ms |
| `senhub.httpcheck.response.size` | `By` | gauge | corps lu (cap 1 MiB) |
| `senhub.httpcheck.tls.expiry` | `d` | gauge | jours restants du certificat leaf (négatif si expiré) |
| `senhub.httpcheck.content.match` | `1` | gauge | émis seulement si content_match configuré |

Redirections rapportées non suivies ; keep-alive désactivé (chaque cycle mesure un handshake complet).

### 4.22 tcp_dial + dns_latency (free, #159/#158)

Same principles as §4.20/4.21: the active chassis, wire milliseconds → `value_scale: 0.001`, and a failure is a measurement (up=0).

| OTel metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.tcpdial.up` / `.duration` | `1` / `s` | gauge | `tcpdial.target` |
| `senhub.dns.up` / `.lookup.duration` / `.answers` | `1` / `s` / `{answer}` | gauge | `dns.question.name` (semconv DNS), `dns.resolver` (`system` = resolver OS) |

### 4.23 prometheus_scrape (free, #304)

Pull ingestion: each scraped sample is a **typed pass-through** — name
and labels kept as they are, with the `otel_type` tag carrying the
counter/gauge semantics to the mapper (the same mechanism as the OIDs
dynamiques de snmp_poll, #207). Untyped → gauge. Histogram et summary
droppés et comptés (contrat scalaires-seulement, identique à
otlp_receiver). No YAML enumeration is possible — only the
self-metrics are defined:

| OTel metric | Unit | Type | Attributes |
|---|---|---|---|
| `senhub.promscrape.up` | `1` | gauge | `promscrape.target` |
| `senhub.promscrape.scrape.duration` | `s` | gauge | wire ms, `value_scale: 0.001` |
| `senhub.promscrape.samples` / `.dropped` | `{sample}` | gauge | `promscrape.target` |

### 4.24 exec (free, #305)

Probe de checks custom : code de sortie Nagios → `senhub.exec.status`
(0 ok / 1 warning / 2 critical / 3 unknown), perfdata et contrat JSON
en **pass-through typé** sous `senhub.exec.<label>` (tag `otel_type`,
the same mechanism as prometheus_scrape, §4.23). Perfdata normalisation:
temps → secondes, octets → bytes, UOM `c` → counter. Self-metrics
définies en YAML :

| OTel metric | Unit | Type | Notes |
|---|---|---|---|
| `senhub.exec.status` | `{status}` | gauge | annotation unit, no `_ratio` suffix |
| `senhub.exec.duration` | `s` | gauge | wire ms, `value_scale: 0.001` |
| `senhub.exec.timeout` / `.skipped` | `1` | gauge | booléens |

### 4.25 snmp_poll (free, #156) — backfill

Section added after the fact (#345): the probe shipped batches 1a/1b
without a semconv table, leaving the YAML transformer as the only source.

Built-in modules (MIB-2, IF-MIB) — one series per device (`snmp.target`),
with interface metrics adding `network.interface.index`:

| OTel metric | Unit | Type | Source MIB |
|---|---|---|---|
| `senhub.snmp.up` | `1` | gauge | joignabilité du cycle |
| `senhub.snmp.poll.duration` | `s` | gauge | wall-clock du poll |
| `snmp.sys.uptime` | `cs` | gauge | sysUpTime (centisecondes, unité SNMP native) |
| `snmp.interface.in_octets` / `out_octets` | `By` | counter | ifInOctets / ifOutOctets |
| `snmp.interface.in_errors` / `out_errors` | `{error}` | counter | ifInErrors / ifOutErrors |
| `snmp.interface.in_discards` / `out_discards` | `{packet}` | counter | ifInDiscards / ifOutDiscards |
| `snmp.interface.speed` | `bit/s` | gauge | ifSpeed |
| `snmp.interface.admin_status` / `oper_status` | `{status}` | gauge | ifAdminStatus / ifOperStatus (IF-MIB enums 1..7; annotation unit since #344, so no `_ratio` suffix) |

`custom_mappings` and dynamic OIDs go through the typed pass-through
(the `otel_type` tag) — by construction there is nothing to enumerate here.

### 4.26 apache (free, #465)

Aligned with the otelcol-contrib `apachereceiver`. `senhub.apache.up` is a SenHub extension, with no equivalent in the contrib receiver. Source: the mod_status `?auto` endpoint. Common attributes: `instance` (host:port), `server.address`, `server.port`.

| OTel metric | Unit | Type | Source mod_status |
|---|---|---|---|
| `senhub.apache.up` | `1` | gauge | joignabilité du cycle (1 = succès, 0 = échec) |
| `apache.uptime` | `s` | counter | Uptime |
| `apache.current_connections` | `{connection}` | gauge | ConnsTotal |
| `apache.workers` | `{worker}` | gauge | BusyWorkers / IdleWorkers ; attribut `apache.workers.state` (busy/idle) |
| `apache.requests` | `{request}` | counter | Total Accesses |
| `apache.traffic` | `By` | counter | Total kBytes × 1024 |

Référence receiver contrib : [opentelemetry-collector-contrib/receiver/apachereceiver](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/apachereceiver).
### 4.27 haproxy (free, #464)

Aligned with the otelcol-contrib haproxy receiver wherever the name
exists; one series per `(proxy, component)` pair (attributes
`haproxy.proxy.name` +
`haproxy.component`).

| OTel metric | Unit | Type | Source CSV |
|---|---|---|---|
| `senhub.haproxy.up` | `1` | gauge | joignabilité de l'endpoint stats |
| `haproxy.sessions.count` | `{session}` | gauge | scur — sessions actives courantes |
| `haproxy.sessions.total` | `{session}` | counter | stot — total cumulatif depuis reset |
| `haproxy.bytes.input` | `By` | counter | bin — octets reçus cumulatifs |
| `haproxy.bytes.output` | `By` | counter | bout — octets envoyés cumulatifs |
| `haproxy.connections.errors` | `{error}` | counter | econ — erreurs de connexion cumulatives |
| `haproxy.requests.errors` | `{error}` | counter | ereq — erreurs de requête cumulatives (frontends) |
| `haproxy.responses.errors` | `{error}` | counter | eresp — erreurs de réponse cumulatives |
| `haproxy.requests.rate` | `{request}/s` | gauge | req_rate — taux courant (frontends) |

Les métriques cumulatives (`haproxy.sessions.total`, `haproxy.bytes.*`,
`haproxy.*.errors`) are of type `counter` (monotonically increasing),
which produces the `_total` suffix on the Prometheus side and the correct
monotonic behaviour in OTLP. Use `rate()` / `increase()` directly on those
séries.
### 4.28 Probe `kafka` (broker / topic / consumer-group monitoring)

**Sources principales :**
- [OTel Collector contrib — `kafkametricsreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/kafkametricsreceiver) — the canonical reference for names and units.
- [Apache Kafka documentation — Replication](https://kafka.apache.org/documentation/#replication) — ISR semantics.

**Strategy:** the names adopted are those of `kafkametricsreceiver` (`kafka.brokers`, `kafka.topic.partitions`, `kafka.partition.*`, `kafka.consumer_group.*`). The one exception, `senhub.kafka.up` — a per-cycle reachability indicator — sits under our own namespace.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `senhub.kafka.up` | `1` | gauge | — | 1 = cluster joignable ce cycle |
| `kafka.brokers` | `{broker}` | gauge | — | |
| `kafka.topic.partitions` | `{partition}` | gauge | `messaging.kafka.topic` | |
| `kafka.partition.current_offset` | `{item}` | gauge | `messaging.kafka.topic`, `messaging.kafka.partition` | |
| `kafka.partition.oldest_offset` | `{item}` | gauge | id. | |
| `kafka.partition.replicas` | `{replica}` | gauge | id. | Répliques assignées |
| `kafka.partition.replicas_in_sync` | `{replica}` | gauge | id. | ISR — sous-réplication si ISR < replicas (#468) |
| `kafka.consumer_group.members` | `{member}` | gauge | `messaging.kafka.consumer.group` | |
| `kafka.consumer_group.offset` | `{item}` | gauge | group + topic + partition | |
| `kafka.consumer_group.lag` | `{item}` | gauge | id. | plancher à 0 (never negative) |
| `kafka.consumer_group.lag_sum` | `{item}` | gauge | group + topic | somme lag toutes partitions |

`kafka.partition.replicas_in_sync` comes from `client.InSyncReplicas(topic, partition)` (sarama). A per-partition error is logged at `Warn` and the metric omitted for that cycle; `kafka.partition.replicas` is always emitted. A typical alert condition: `replicas_in_sync < replicas`.
### 4.29 Probe `clickhouse` (free, #465)

Scrapes the ClickHouse `/metrics` Prometheus-text endpoint (available since ClickHouse 20.1)
and maps three ClickHouse metric families:

- `ClickHouseMetrics_*` — instantaneous gauges
- `ClickHouseAsyncMetrics_*` — background/async gauges
- `ClickHouseProfileEvents_*` — cumulative counters

No upstream OTel semantic convention exists for ClickHouse at the time of writing.
Names follow the same pattern as first-party OTel receiver names
(e.g. `clickhouse.queries.active` mirrors `mysql.queries`).
Unit embedded in the name is forbidden per the OTel-first rule; the unit lives in `otel.unit`.

| OTel name | Unit | Type | Source ClickHouse metric |
|---|---|---|---|
| `senhub.clickhouse.up` | `1` | gauge | probe health signal |
| `clickhouse.queries.active` | `{query}` | gauge | `ClickHouseMetrics_Query` |
| `clickhouse.connections` | `{connection}` | gauge | `ClickHouseMetrics_Connection` |
| `clickhouse.memory.used` | `By` | gauge | `ClickHouseMetrics_MemoryTracking` |
| `clickhouse.parts.active` | `{part}` | gauge | `ClickHouseMetrics_Parts` |
| `clickhouse.merges.active` | `{merge}` | gauge | `ClickHouseMetrics_Merge` |
| `clickhouse.uptime` | `s` | counter | `ClickHouseAsyncMetrics_Uptime` |
| `clickhouse.queries.total` | `{query}` | counter | `ClickHouseProfileEvents_Query` |
| `clickhouse.queries.select` | `{query}` | counter | `ClickHouseProfileEvents_SelectQuery` |
| `clickhouse.queries.insert` | `{query}` | counter | `ClickHouseProfileEvents_InsertQuery` |
| `clickhouse.inserted.rows` | `{row}` | counter | `ClickHouseProfileEvents_InsertedRows` |
| `clickhouse.inserted.data` | `By` | counter | `ClickHouseProfileEvents_InsertedBytes` |
| `clickhouse.read.data` | `By` | counter | `ClickHouseProfileEvents_ReadCompressedBytes` |
| `clickhouse.written.data` | `By` | counter | `ClickHouseProfileEvents_WriteCompressedBytes` |

**Discriminant tag:** `instance` (= `server.address`) — registered in `DiscriminantTagsRegistry["clickhouse"]` (#459).
### 4.30 Probe redis (Redis / Valkey)

Probe payante (Pro). Connexion TCP brute (optionallement TLS) au port RESP
(défaut 6379) — aucune dépendance Go externe. Séquence : `AUTH` si mot de
the configured password, then `INFO all`. The RESP bulk-string response is
parsed section by section into a flat `key→value` map.

**Source de référence** : OTel Collector contrib
[`redisreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/redisreceiver).
Where the contrib receiver exposes the metric, we follow its name and attributes
(direct interoperability with Grafana dashboards and standard OTel alerts).
Extensions sous `senhub.db.*` quand aucun équivalent contrib n'existe.

**Compatibilité Redis 7** : `slave_repl_offset` a été renommé en
`replica_repl_offset` in Redis 7. The probe reads both fields, with a fallback.

**No external dependency**: the RESP from `INFO all` is parsed with the
standard library alone (`bufio`, `net`, `crypto/tls`). No Go redis client.
The rule is invariant — any PR adding a third-party import to this package
is rejected.

**TLS**: `tls: true` in the config wraps the `net.Conn` with
`crypto/tls.Client` after the dial; no client-certificate configuration is
exposed yet (tracked in #394).

**Metrics emitted** (one series per `host:port` instance):

| OTel metric | Unit | Type | Source INFO |
|---|---|---|---|
| `senhub.db.up` | `1` | gauge | joignabilité du cycle |
| `redis.uptime` | `s` | counter | `uptime_in_seconds` |
| `senhub.db.version.info` | `1` | gauge | `redis_version` (attr `db.system.version`) |
| `redis.clients.connected` | `{client}` | gauge | `connected_clients` |
| `redis.clients.blocked` | `{client}` | gauge | `blocked_clients` |
| `redis.connections.received` | `{connection}` | counter | `total_connections_received` |
| `redis.connections.rejected` | `{connection}` | counter | `rejected_connections` |
| `redis.memory.used` | `By` | gauge | `used_memory` |
| `redis.memory.used.rss` | `By` | gauge | `used_memory_rss` |
| `redis.memory.peak` | `By` | gauge | `used_memory_peak` |
| `redis.memory.fragmentation.ratio` | `1` | gauge | `mem_fragmentation_ratio` |
| `redis.commands.processed` | `{command}` | counter | `total_commands_processed` |
| `redis.net.input` | `By` | counter | `total_net_input_bytes` |
| `redis.net.output` | `By` | counter | `total_net_output_bytes` |
| `redis.ops.per_sec` | `{op}/s` | gauge | `instantaneous_ops_per_sec` |
| `redis.keyspace.hits` | `{hit}` | counter | `keyspace_hits` |
| `redis.keyspace.misses` | `{miss}` | counter | `keyspace_misses` |
| `redis.keyspace.hit.ratio` | `1` | gauge | dérivé : hits/(hits+misses), 0 si aucun trafic |
| `redis.db.keys` | `{key}` | gauge | keyspace `dbN:keys=K` — tag `db`=N, attr `db.redis.database_index` |
| `redis.db.expires` | `{key}` | gauge | keyspace `dbN:expires=M` — tag `db`=N |
| `redis.replication.role` | `1` | gauge | `role` — master=1, slave/replica=0, sentinel=-1 |
| `redis.replication.offset` | `By` | counter | `master_repl_offset` (master) / `slave_repl_offset` ou `replica_repl_offset` (replica) |
| `redis.replication.slaves.connected` | `{replica}` | gauge | `connected_slaves` (master uniquement) |
| `redis.replication.lag` | `s` | gauge | `master_last_io_seconds_ago` (replica uniquement) |
| `redis.rdb.changes` | `{change}` | gauge | `rdb_changes_since_last_save` |
| `redis.aof.enabled` | `1` | gauge | `aof_enabled` |

**Entité émise** (`entity rail`, source enregistrée au démarrage) :

```
type: db
id:   {db.instance.id: "<db.system.name>:<port>@<host.id>" en loopback,
       "adresse:port" sinon}
attrs: {db.system.name: "redis", server.address: host, server.port: port,
        db.version: redis_version}
```

Correspondence with the OTel contrib `redisreceiver`: the `redis.*` names
correspondent aux noms contrib 1:1. Les métriques `senhub.db.*` (up, version)
are extensions with no contrib equivalent.

**float32 precision on large counters**: `used_memory`,
`total_net_input_bytes` and others can exceed 16 MiB on a busy server, past
which the float32 mantissa loses precision. A defect shared with the other DB
probes (#258). The value is emitted as it is.
### 4.31 Probe `memcached` (Memcached cache server)

**Sources principales :**
- [otelcol-contrib `memcachedreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/memcachedreceiver) — the canonical reference for names and attributes
- Protocole texte Memcached `stats\r\n` (RFC informelle — [Memcached protocol.txt](https://github.com/memcached/memcached/blob/master/doc/protocol.txt))

**Strategy:** follow the `memcachedreceiver` names wherever contrib has the metric (`memcached.network`, `memcached.operations`, `memcached.commands`, `memcached.cpu.usage`, `memcached.uptime`, `memcached.evictions`); use local `memcached.*` extensions for metrics with no contrib equivalent (`memcached.current.connections`, `memcached.connections.total`, `memcached.current.items`, `memcached.items.total`, `memcached.bytes`, `memcached.limit_maxbytes`). No unit suffix in the name — the canonical OTel unit lives in `otel.unit`.

#### 4.31.1 Métriques

| OTel metric | Unit | Type | Attributes | Source stats |
|---|---|---|---|---|
| `senhub.memcached.up` | `1` | gauge | `server.address` | (synthétique) |
| `memcached.uptime` | `s` | counter | `server.address` | `uptime` |
| `memcached.current.connections` | `{connection}` | gauge | `server.address` | `curr_connections` |
| `memcached.connections.total` | `{connection}` | counter | `server.address` | `total_connections` |
| `memcached.current.items` | `{item}` | gauge | `server.address` | `curr_items` |
| `memcached.items.total` | `{item}` | counter | `server.address` | `total_items` |
| `memcached.bytes` | `By` | gauge | `server.address` | `bytes` |
| `memcached.limit_maxbytes` | `By` | gauge | `server.address` | `limit_maxbytes` |
| `memcached.network` | `By` | counter | `server.address`, `network.io.direction` | `bytes_written` / `bytes_read` |
| `memcached.operations` | `{operation}` | counter | `server.address`, `memcached.operation.result` | `get_hits` / `get_misses` |
| `memcached.commands` | `{command}` | counter | `server.address`, `memcached.command` | `cmd_get` / `cmd_set` / `cmd_flush` |
| `memcached.evictions` | `{eviction}` | counter | `server.address` | `evictions` |
| `memcached.cpu.usage` | `s` | counter | `server.address`, `process.cpu.state` | `rusage_user` / `rusage_system` |

#### 4.31.2 Collapses

| OTel metric | Valeurs de l'attribut discriminant |
|---|---|
| `memcached.network` | `network.io.direction` = `transmit` (bytes_written) / `receive` (bytes_read) |
| `memcached.operations` | `memcached.operation.result` = `hit` / `miss` |
| `memcached.commands` | `memcached.command` = `get` / `set` / `flush` |
| `memcached.cpu.usage` | `process.cpu.state` = `user` / `system` |

`network.io.direction` follows the OTel convention (`transmit`/`receive`) — the same values contrib's `memcachedreceiver` produces on `memcached.network{direction}`.

#### 4.31.3 DiscriminantTagsRegistry

Discriminant tags declared in `http_cache.go`: `result`, `command`, `state`, `direction`, `metric_type`.
The probe's `direction` tag is renamed to the OTel attribute `network.io.direction` via `tag_to_attribute` — cache discrimination uses the original tag name (`direction`).
### 4.32 proxmox (free)

Probe REST API Proxmox VE : nodes, VMs QEMU, conteneurs LXC, pools de
stockage. Authentification via PVE API token (header `Authorization:
PVEAPIToken`). The `proxmox.*` namespace (vendor-specific) +
`senhub.proxmox.*` for the SenHub extensions.

| OTel metric | Unit | Type | Notes |
|---|---|---|---|
| `senhub.proxmox.up` | `1` | gauge | 1 = API répond ; 0 = toute erreur de connexion ou d'authentification. Toujours émis, y compris en cas d'échec. |
| `proxmox.node.cpu.utilization` | `1` | gauge | ratio CPU du nœud (0–1) |
| `proxmox.node.memory.used` | `By` | gauge | Memory used on the node |
| `proxmox.node.memory.total` | `By` | gauge | Total memory installed on the node |
| `proxmox.node.status` | `1` | gauge | 1 = online, 0 = offline |
| `proxmox.vm.cpu.utilization` | `1` | gauge | ratio CPU VM/LXC (0–1) |
| `proxmox.vm.memory.used` | `By` | gauge | Memory used by the VM/container |
| `proxmox.vm.memory.total` | `By` | gauge | Memory allocated to the VM/container |
| `proxmox.vm.disk.read` | `By` | counter | Bytes read since boot |
| `proxmox.vm.disk.write` | `By` | counter | Bytes written since boot |
| `proxmox.vm.network.in` | `By` | counter | Bytes received on all vNICs |
| `proxmox.vm.network.out` | `By` | counter | Bytes sent on all vNICs |
| `proxmox.vm.status` | `1` | gauge | 1 = running, 0 = arrêtée |
| `proxmox.storage.used` | `By` | gauge | Bytes used in the pool |
| `proxmox.storage.total` | `By` | gauge | capacité totale du pool |

Attributs discriminants (via `tag_to_attribute`) : `proxmox.node`,
`proxmox.vmid`, `proxmox.vm.name`, `proxmox.vm.type`, `proxmox.storage`.
### 4.33 Probe `unifi` (free, #465)

Probe Ubiquiti UniFi Controller — REST API stdlib HTTP, auth cookie. Une
one instance = one controller. Metrics: availability, inventory by type,
clients, WAN throughput, and per-AP CPU/RAM/satisfaction.

#### 4.33.1 Métriques

| OTel metric | Unit | Type | Attributs / Notes |
|---|---|---|---|
| `senhub.unifi.up` | `1` | gauge | `unifi.endpoint`, `unifi.site` |
| `unifi.devices.total` | `{device}` | gauge | `unifi.device.type` (`uap`/`usw`/`ugw`) |
| `unifi.devices.adopted` | `{device}` | gauge | `unifi.device.type` |
| `unifi.devices.disconnected` | `{device}` | gauge | `unifi.device.type` |
| `unifi.clients.total` | `{client}` | gauge | `unifi.site` |
| `unifi.clients.wifi` | `{client}` | gauge | `unifi.site` |
| `unifi.network.io` | `By` | counter | `network.io.direction` ∈ {`transmit`, `receive`} ; `unifi.site` |
| `unifi.device.cpu` | `1` | gauge | `unifi.device.name`, `unifi.device.type`, `unifi.site` |
| `unifi.device.memory` | `1` | gauge | `unifi.device.name`, `unifi.device.type`, `unifi.site` |
| `unifi.ap.clients` | `{client}` | gauge | `unifi.device.name`, `unifi.site` |
| `unifi.ap.satisfaction` | `1` | gauge | ratio 0..1 ; `unifi.device.name`, `unifi.site` |

#### 4.33.2 Collapse `unifi.network.io` (#465)

`unifi.network.tx_bytes` et `unifi.network.rx_bytes` (deux noms) ont été
merged into **`unifi.network.io`**, discriminated by
`network.io.direction` (`transmit` / `receive`), aligned with the convention
OTel `system.network.io` (§4.3) et `senhub.db.mysql.io{io.direction}`.
The value is the byte-rate the controller reports (the `tx_bytes-r` field
/ `rx_bytes-r` du endpoint `stat/health`). La tag probe `direction` est
mappée vers l'attribut OTel `network.io.direction` dans le YAML
transformer (`tag_to_attribute`).

### Docker Swarm (`swarm`)

The `swarm.*` namespace is **SenHub-defined**: OpenTelemetry has no Swarm
receiver, so there are no upstream names to align with — unlike `k8s.*`. Every
series carries `swarm.cluster.name` in addition to the attributes listed.

State is read from a **manager** node only. `senhub.swarm.up` is 0 on a worker,
outside a swarm, or when the socket does not answer; `senhub.swarm.node_role_state`
says which of the three.

| Metric | Unit | Type | Attributes | Description |
|---|---|---|---|---|
| `senhub.swarm.up` | `1` | gauge | — | 1 when this node is a swarm manager and answered; 0 for worker, non-swarm or unreachable — the state series says which |
| `senhub.swarm.node_role_state` | `{state}` | gauge | `state` | one-hot over manager / worker / not_in_swarm / unreachable: why the probe sees what it sees |
| `swarm.cluster.nodes` | `{node}` | gauge | — | nodes known to the cluster |
| `swarm.cluster.managers` | `{node}` | gauge | — | manager nodes |
| `swarm.cluster.managers.reachable` | `{node}` | gauge | — | managers currently reachable by the Raft leader |
| `swarm.cluster.workers` | `{node}` | gauge | — | worker nodes |
| `swarm.cluster.quorum` | `{state}` | gauge | — | 1 when a strict majority of managers is reachable; 0 means the cluster accepts no change at all — no deploy, no rescheduling |
| `swarm.cluster.tasks.orphaned` | `{task}` | gauge | — | tasks whose service no longer exists; invisible from every per-service view |
| `swarm.cluster.networks` | `{network}` | gauge | — | swarm-scoped overlay segments |
| `swarm.node.ready` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role` | 1 when the node status is ready |
| `swarm.node.state` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role`, `state` | one-hot over ready / down / unknown / disconnected |
| `swarm.node.availability` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role`, `availability` | one-hot over active / pause / drain — the operator's intent, as opposed to the node's actual state |
| `swarm.node.cpu.allocatable` | `{cpu}` | gauge | `swarm.node.name`, `swarm.node.role` | CPU cores the node advertises to the scheduler |
| `swarm.node.memory.allocatable` | `By` | gauge | `swarm.node.name`, `swarm.node.role` | memory the node advertises to the scheduler |
| `swarm.node.manager.leader` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role` | 1 on the Raft leader |
| `swarm.node.manager.reachable` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role` | 1 when this manager is reachable by the leader |
| `swarm.node.manager.reachability` | `{state}` | gauge | `swarm.node.name`, `swarm.node.role`, `reachability` | one-hot over reachable / unreachable / unknown |
| `swarm.node.tasks.running` | `{task}` | gauge | `swarm.node.id` | running tasks placed on this node |
| `swarm.service.replicas.desired` | `{task}` | gauge | `swarm.service.name`, `swarm.service.mode` | replicas asked for; for a global service, the tasks Swarm intends to run |
| `swarm.service.replicas.running` | `{task}` | gauge | `swarm.service.name`, `swarm.service.mode` | replicas actually running, counted from tasks |
| `swarm.service.converged` | `{state}` | gauge | `swarm.service.name`, `swarm.service.mode` | 1 when running replicas have caught up with the declared count |
| `swarm.service.update.state` | `{state}` | gauge | `swarm.service.name`, `swarm.service.mode`, `state` | one-hot over the rolling-update lifecycle; a service stuck in paused is a deploy waiting for a human |
| `swarm.service.tasks.failed` | `{task}` | gauge | `swarm.service.name` | tasks in a terminal failure state (failed, rejected, orphaned) |
| `swarm.service.port.published` | `{port}` | gauge | `swarm.service.name`, `swarm.port.published`, `swarm.port.target`, `network.transport`, `swarm.port.mode` | one series per published port; in ingress mode the port answers on every node, not only where the service runs |
| `swarm.task.state` | `{task}` | gauge | `swarm.service.name`, `state` | tasks per service per lifecycle state; separates 'not there yet' from 'will never get there' |
| `swarm.service.network.attached` | `{state}` | gauge | `swarm.service.name`, `swarm.network.name`, `swarm.service.vip` | 1 per (service, overlay) pair — the reachability map: two services share a segment or they cannot talk |
| `swarm.network.services` | `{service}` | gauge | `swarm.network.name`, `swarm.network.subnet` | services attached to this overlay |
| `swarm.network.tasks` | `{task}` | gauge | `swarm.network.name`, `swarm.network.subnet` | task attachments on this overlay |
| `swarm.network.ingress` | `{state}` | gauge | `swarm.network.name`, `swarm.network.subnet` | 1 on the routing-mesh segment that carries every published port |
| `swarm.network.attachable` | `{state}` | gauge | `swarm.network.name`, `swarm.network.subnet` | 1 when standalone containers may join this overlay |
| `swarm.network.internal` | `{state}` | gauge | `swarm.network.name`, `swarm.network.subnet` | 1 when the overlay has no external route |
| `swarm.network.address.capacity` | `{address}` | gauge | `swarm.network.name`, `swarm.network.subnet` | assignable addresses in the overlay subnet; an overlay running out refuses new tasks with an error naming neither |

**What is NOT measured**: traffic volume between two services on the same
overlay. The Docker API exposes no per-peer counters, and per-container counters
are keyed by interface name (`eth0`), which the API never maps back to a named
network. A real flow matrix needs conntrack or eBPF on every node. The probe
maps reachability, not throughput.

**Entities**: the cluster as a `service.instance` (`swarm://<cluster-id>`).
Neither the nodes (Swarm reports no `machine-id` — a host minted from a hostname
would be a permanent duplicate) nor the overlays (no registered type for a
network segment) emit an entity.

### 4.34 kubernetes (free, #469)

Aligned with the OTel Kubernetes semantic conventions (`k8s.*` namespace,
semconv 1.30+). One availability series per cluster (`k8s.cluster.name`), then
series per node, pod, container or deployment depending on the configuration.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `senhub.kubernetes.up` | `1` | gauge | `k8s.cluster.name` | 0 when the API server is unreachable; emitted even on a total failure (#469) |
| `k8s.node.ready` | `{state}` | gauge | `k8s.node.name`, `k8s.cluster.name` | NodeReady condition |
| `k8s.node.cpu.allocatable` | `{core}` | gauge | `k8s.node.name`, `k8s.cluster.name` | allocatable CPU cores |
| `k8s.node.memory.allocatable` | `By` | gauge | `k8s.node.name`, `k8s.cluster.name` | mémoire allouable en octets |
| `k8s.node.pods.capacity` | `{pod}` | gauge | `k8s.node.name`, `k8s.cluster.name` | capacité max en pods |
| `k8s.node.pods.allocatable` | `{pod}` | gauge | `k8s.node.name`, `k8s.cluster.name` | ceiling of pods the scheduler may place. Renamed from `.allocated` (#756): the value comes from `Status.Allocatable.Pods()`, which is a ceiling and not a count of what is running |
| `k8s.pod.phase` | `{state}` | gauge | `k8s.pod.name`, `k8s.namespace.name`, `k8s.node.name` | 1 when phase=Running |
| `k8s.pod.ready` | `{state}` | gauge | `k8s.pod.name`, `k8s.namespace.name`, `k8s.node.name` | condition PodReady |
| `k8s.pod.restarts` | `{restart}` | counter | `k8s.pod.name`, `k8s.namespace.name`, `k8s.node.name` | total redémarrages conteneurs |
| `k8s.container.ready` | `{state}` | gauge | `k8s.container.name`, `k8s.pod.name`, `k8s.namespace.name` | état ready du conteneur |
| `k8s.container.restarts` | `{restart}` | counter | `k8s.container.name`, `k8s.pod.name`, `k8s.namespace.name` | redémarrages conteneur |
| `k8s.deployment.available` | `{pod}` | gauge | `k8s.deployment.name`, `k8s.namespace.name` | réplicas disponibles |
| `k8s.deployment.desired` | `{pod}` | gauge | `k8s.deployment.name`, `k8s.namespace.name` | réplicas désirés (spec.replicas) |
| `k8s.deployment.ready` | `{state}` | gauge | `k8s.deployment.name`, `k8s.namespace.name` | 1 when available >= desired |

**Conditions de nœud (#756).** Polarité inverse de `k8s.node.ready` : ici **1 = la pression EST présente**. Les noms disent l'état compté plutôt qu'un « status » neutre, parce que mélanger les deux conventions sur un même tableau de bord est un piège réel. Une condition non rapportée sort à 0 pour qu'un lecteur ne confonde pas « pas de pression » avec « pas d'information » — sauf `network_unavailable`, que beaucoup de CNI ne renseignent jamais et où un 0 constant inventerait un fait.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `k8s.node.condition.memory_pressure` | `{state}` | gauge | `k8s.node.name`, `k8s.cluster.name` | 1 = sous pression mémoire |
| `k8s.node.condition.disk_pressure` | `{state}` | gauge | `k8s.node.name`, `k8s.cluster.name` | 1 = under disk pressure; the kubelet is already evicting while `ready` is still 1 |
| `k8s.node.condition.pid_pressure` | `{state}` | gauge | `k8s.node.name`, `k8s.cluster.name` | 1 = sous pression PID |
| `k8s.node.condition.network_unavailable` | `{state}` | gauge | `k8s.node.name`, `k8s.cluster.name` | emitted only when the CNI reports the condition |

**Réservations de ressources (#756).** Sans elles, impossible de dire si un cluster est sur-réservé. Les conteneurs d'init sont exclus des sommes de pod : ils ne conservent pas leur réservation pour la durée de vie du pod. Un pod **sans limite** est illimité — fait distinct d'une limite à zéro — donc aucune série de limite n'est émise plutôt qu'un 0 trompeur.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `k8s.pod.cpu.request` / `k8s.pod.cpu.limit` | `{cpu}` | gauge | `k8s.pod.name`, `k8s.namespace.name`, `k8s.node.name` | summed over the pod's containers |
| `k8s.pod.memory.request` / `k8s.pod.memory.limit` | `By` | gauge | idem | summed over the pod's containers |
| `k8s.container.cpu.request` / `.limit` | `{cpu}` | gauge | `k8s.container.name`, `k8s.pod.name`, `k8s.namespace.name` | per container |
| `k8s.container.memory.request` / `.limit` | `By` | gauge | idem | per container |
| `k8s.container.waiting` | `{state}` | gauge | + `k8s.container.waiting.reason` | 1 while the container is waiting; the reason separates CrashLoopBackOff from an image still downloading, two situations calling for opposite reactions |

**Workloads au-delà de Deployment (#756).** Le type porte le tag `k8s.workload.kind` plutôt que d'être dans le nom, pour qu'un tableau de bord puisse regrouper sans connaître la liste. Chaque type émet le désiré face au réel, parce que l'écart est le chiffre qu'on lit pendant un déploiement.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `k8s.statefulset.desired` / `.ready` / `.current` / `.updated` | `{pod}` | gauge | `k8s.workload.name`, `k8s.workload.kind`, `k8s.namespace.name` | |
| `k8s.daemonset.desired_scheduled` / `.current_scheduled` / `.ready` / `.misscheduled` | `{node}` | gauge | idem | `misscheduled` = placés là où ils ne devraient pas être |
| `k8s.replicaset.desired` / `.ready` / `.available` | `{pod}` | gauge | idem | disabled by default: a Deployment owns one per revision |
| `k8s.job.active` / `.succeeded` / `.failed` / `.desired_completions` | `{pod}` | gauge | idem | `failed` is the signal: a Job whose pods fail stays present and looks scheduled |
| `k8s.cronjob.active_jobs` / `.suspended` | `{job}` / `{state}` | gauge | idem | a suspended CronJob produces nothing and looks like a schedule that has not fired yet |

**Stockage, quotas, autoscaling (#756).** Les phases sortent en **one series per phase** avec 0/1 plutôt qu'un entier d'énumération : une chaîne ne peut pas être une valeur, et numéroter les états fait qu'une phase ajoutée en amont devient silencieusement une phase existante. Ici une phase inconnue n'allume rien.

| OTel metric | Unit | Type | Attributes | Notes |
|---|---|---|---|---|
| `k8s.persistentvolume.capacity` | `By` | gauge | `k8s.persistentvolume.name`, `k8s.storageclass.name` | volumes are cluster-scoped: the namespace filter does not apply to them |
| `k8s.persistentvolume.phase` | `{state}` | gauge | + `phase` | one series per phase |
| `k8s.persistentvolumeclaim.requested` / `.capacity` | `By` | gauge | `k8s.persistentvolumeclaim.name`, `k8s.namespace.name` | `capacity` may exceed `requested` when the storage class rounds up; absent while the claim is Pending |
| `k8s.persistentvolumeclaim.phase` | `{state}` | gauge | + `phase` | a claim stuck Pending is why the pod waiting on it never starts |
| `k8s.resourcequota.hard` / `.used` | `{resource}` | gauge | `k8s.resourcequota.name`, `k8s.resourcequota.resource`, `k8s.namespace.name` | unit depends on the resource: cores for cpu, bytes for memory, integers for counts |
| `k8s.hpa.current_replicas` / `.desired_replicas` / `.min_replicas` / `.max_replicas` | `{pod}` | gauge | `k8s.hpa.name`, `k8s.hpa.target`, `k8s.namespace.name` | an autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own counters |

**Events (#756) — rail logs, pas métriques.** Les Events Kubernetes voyagent en enregistrements de log : ce sont des phrases datées, et les compter garderait le nombre en jetant le diagnostic. Un « Warning » Kubernetes est classé **Error** : Kubernetes n'a pas de niveau erreur, et un échec de téléchargement d'image y arrive au même niveau qu'un avertissement de routine. Attributs : `k8s.event.reason`, `.type`, `.object.kind`, `.object.name`, `.source`, `.count`, plus l'étiquette d'identité du sujet (`k8s.pod.name`, `k8s.node.name`, `k8s.workload.name`…) pour joindre l'événement aux séries qu'il explique.
### 4.35 Probe `mssql` (Microsoft SQL Server)

Source canonique : [OTel Collector contrib `sqlserverreceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/sqlserverreceiver).
Quand le receiver contrib expose la métrique, l'agent adopte son nom et ses attributs pour une interopérabilité directe. `senhub.db.up` est l'exception cross-engine partagée avec mysql/postgresql.

| OTel metric | Unit | Type | Source SQL |
|---|---|---|---|
| `senhub.db.up` | `1` | gauge | heartbeat de connectivité |
| `sqlserver.batch_request.rate` | `{request}/s` | gauge | `Batch Requests/sec` (dm_os_performance_counters) |
| `sqlserver.transaction_rate` | `{transaction}/s` | gauge | `Transactions/sec` (dm_os_performance_counters) |
| `sqlserver.page_buffer_cache.hit_ratio` | `1` | gauge | `Buffer cache hit ratio` / 100 — ratio 0-1, **pas un pourcentage** |
| `sqlserver.page_life_expectancy` | `s` | gauge | `Page life expectancy` (dm_os_performance_counters) |
| `sqlserver.lock_wait_rate` | `{wait}/s` | gauge | `Lock Waits/sec` (dm_os_performance_counters) |
| `sqlserver.processes.blocked` | `{process}` | gauge | `Processes blocked` (dm_os_performance_counters) |
| `sqlserver.user.connection.count` | `{connection}` | gauge | `User Connections` (dm_os_performance_counters) |
| `sqlserver.database.io` | `By` | counter | bytes read/write par database (dm_io_virtual_file_stats) |
| `sqlserver.database.status` | `1` | gauge | état de la base (sys.databases.state — 0=ONLINE) |

**Note unité** : le compteur natif `Buffer cache hit ratio` est émis par SQL Server en entier 0-100.
L'`otel.unit: "1"` dans le transformer YAML déclenche la division ÷100 dans `convertValue`
(même pattern que `oracle.buffer.cache.hit_ratio`). L'unité opérateur (`unit: "%"`) reste inchangée
pour l'affichage PRTG.

**Attributs multi-instance** :
- `sqlserver.database.io` : `db.namespace` (nom de la base), `direction` (`read`/`write`).
- `sqlserver.database.status` : `db.namespace`.

### 4.36 Probe `powerstore` (baie de stockage Dell PowerStore)

Aucune convention OTel semconv pour les baies de stockage — tout sous extensions
`senhub.powerstore.*` (même statut que `senhub.veeam.*`). `hw.*` reste réservé aux
composants matériels d'un hôte, pas aux agrégats niveau baie. La baie est une
`service.instance` (identité = `powerstore:<cluster.global_id>`, immuable ;
`server.address` reste descriptif) — précédent redfish, elle est monitorée
out-of-band via l'API REST donc pas de machine-id, ce n'est pas un `host`.

#### 4.36.1 Extensions `senhub.powerstore.*`

| OTel metric | Type / unité | Attributes | Source REST |
|---|---|---|---|
| `senhub.powerstore.up` | Gauge `1` | — | reachability du `/cluster` |
| `senhub.powerstore.cluster.state` | Gauge `1` | `senhub.powerstore.cluster.config_state` | `/cluster.state` (Configured=2, Unconfigured=1, autre=0) |
| `senhub.powerstore.hardware.components` | Gauge `{component}` | `senhub.powerstore.hardware.state` (`healthy`/`faulted`) | `/hardware.lifecycle_state` |
| `senhub.powerstore.capacity.physical` | Gauge `By` | `senhub.powerstore.capacity.state` (`used`/`total`) | `POST /metrics/generate` (`physical_used`/`physical_total`) |
| `senhub.powerstore.capacity.logical` | Gauge `By` | `senhub.powerstore.capacity.state` (`used`) | `logical_used` |
| `senhub.powerstore.capacity.used_ratio` | Gauge `1` | — | `physical_used ÷ physical_total` — émis en `%` (0-100) par la probe, ÷100 par le mapper (le PRTG affiche 42 %, l'OTLP exporte 0.42) |
| `senhub.powerstore.data_reduction_ratio` | Gauge `1` | — | `data_reduction` |
| `senhub.powerstore.efficiency_ratio` | Gauge `1` | — | `efficiency_ratio` |
| `senhub.powerstore.iops` | Gauge `{operation}/s` | `senhub.powerstore.operation` (`read`/`write`/`total`) | `performance_metrics_by_cluster` (`avg_*_iops`) |
| `senhub.powerstore.bandwidth` | Gauge `By/s` | `senhub.powerstore.operation` | `avg_*_bandwidth` |
| `senhub.powerstore.latency` | Gauge `s` | `senhub.powerstore.operation` | `avg_*_latency` — la probe émet des ms, `value_scale: 0.001` → secondes (les vues PRTG/Nagios affichent des ms) |
| `senhub.powerstore.io_size` | Gauge `By` | — | `avg_io_size` |
| `senhub.powerstore.cpu.utilization` | Gauge `1` | — | `performance_metrics_by_appliance.avg_io_workload_cpu_utilization`, émis en `%` (0-100) par la probe, ÷100 par le mapper |
| `senhub.powerstore.replication.sessions` | Gauge `{session}` | — | `/replication_session` (0 si non configuré) |
| `senhub.powerstore.volumes` | Gauge `{volume}` | — | `/volume` (total) |
| `senhub.powerstore.volumes.not_ready` | Gauge `{volume}` | — | `/volume.state != Ready` |
| `senhub.powerstore.alerts.active` | Gauge `{alert}` | `senhub.powerstore.alert.severity` (`Critical`/`Major`/`Minor`/`Info`) | `/alert.state == ACTIVE` |

**Health state (`hw.state` on the entity)** — derived each cycle: a component
`faulted` ou une alerte `Critical` active ⇒ `failed` ; une alerte `Major` active
⇒ `degraded` ; sinon `ok`. Une transition émet un `entity.state_changed`.

**Lifecycle sain vs en défaut** : `Healthy` compte comme sain ; `Empty`,
`Initializing`, `Instantiated` et l'absence de valeur (ligne appliance = null) ne
comptent NI sain NI en défaut (slot vide / composant en cours de démarrage) ;
tout autre état (`Degraded`, `Failed`, `Unavailable`, `PoweredOff`…) est `faulted`.

**Auth** : Basic pour les GET ; le `POST /metrics/generate` rejoue le CSRF
`DELL-EMC-TOKEN` capturé sur le premier GET + le cookie de session.

#### 4.36.2 Séries par-ressource (multi-instance)

En plus des agrégats niveau cluster ci-dessus, la probe émet une série par
ressource. Chaque série porte un tag ressource (`volume`, `appliance`, `node`,
`drive`, `session`) **mappé en attribut OTel via `tag_to_attribute`** — sans quoi
les instances s'écraseraient en OTLP/Prometheus (une seule série au lieu de N).

| OTel metric | Type / unité | Attribut ressource (+ autres) | Source REST |
|---|---|---|---|
| `senhub.powerstore.volume.state` | Gauge `1` | `senhub.powerstore.volume.name` | `/volume.state` (Ready=1, autre=0) |
| `senhub.powerstore.volume.logical_used` | Gauge `By` | `…volume.name` | `/volume.logical_used` |
| `senhub.powerstore.volume.size` | Gauge `By` | `…volume.name` | `/volume.size` (provisioned) |
| `senhub.powerstore.volume.iops` | Gauge `{operation}/s` | `…volume.name` + `operation` (`read`/`write`/`total`) | `performance_metrics_by_volume` (opt-in, borné — voir `volume_perf`) |
| `senhub.powerstore.volume.bandwidth` | Gauge `By/s` | `…volume.name` + `operation` | idem |
| `senhub.powerstore.volume.latency` | Gauge `s` | `…volume.name` + `operation` | idem (ms → s via value_scale) |
| `senhub.powerstore.drive.state` | Gauge `1` | `senhub.powerstore.drive.name` | `/hardware` (type=Drive) lifecycle (Healthy=1) |
| `senhub.powerstore.appliance.state` | Gauge `1` | `senhub.powerstore.appliance.name` | `/appliance` lifecycle (Healthy=1) |
| `senhub.powerstore.appliance.capacity.physical` | Gauge `By` | `…appliance.name` + `capacity.state` (`used`/`total`) | `space_metrics_by_appliance` |
| `senhub.powerstore.appliance.capacity.logical` | Gauge `By` | `…appliance.name` + `capacity.state` (`used`) | idem |
| `senhub.powerstore.appliance.iops` | Gauge `{operation}/s` | `…appliance.name` + `operation` (`read`/`write`/`total`) | `performance_metrics_by_appliance` |
| `senhub.powerstore.appliance.bandwidth` | Gauge `By/s` | `…appliance.name` + `operation` (`total`) | idem |
| `senhub.powerstore.appliance.latency` | Gauge `s` | `…appliance.name` + `operation` (`total`) | idem (ms → s via value_scale) |
| `senhub.powerstore.appliance.cpu.utilization` | Gauge `1` | `…appliance.name` | idem (émis en `%`, ÷100 par le mapper) |
| `senhub.powerstore.node.cpu.utilization` | Gauge `1` | `senhub.powerstore.node.name` | `performance_metrics_by_node` (émis en `%`, ÷100 par le mapper) |
| `senhub.powerstore.node.iops` | Gauge `{operation}/s` | `…node.name` + `operation` (`total`) | idem |
| `senhub.powerstore.replication.state` | Gauge `1` | `senhub.powerstore.replication.session_id` | `/replication_session.state` (OK=2, transitional=1, error=0) |

**Cardinalité** : la perf **par-volume** (IOPS/latence par volume) n'est PAS émise
— elle coûterait un `POST /metrics/generate` par volume par cycle (voir issue de
suivi). Seules la capacité + l'état par-volume sont exposés. La perf par-appliance
et par-node réutilise la même forme `perfMetrics`/`spaceMetrics` (cardinalité
faible : 1-4 appliances, 2-8 nœuds).

### 4.37 Probe `ad_hybrid` (Azure AD Connect Health)

Hybrid identity synchronisation health (Azure AD Connect Health). No
semconv OTel pour ce domaine — métriques sous `senhub.ad_hybrid.*` (même statut
que `senhub.veeam.*`). Émission : ids courts snake_case côté probe (enterprise
`probes/ad_hybrid/`), noms/unités/types déclarés par le transformer
`transformers/definitions/ad_hybrid.yaml`.

| Metric | Type / unit | Attributes | Notes |
|---|---|---|---|
| `senhub.ad_hybrid.up` | Gauge `1` | — | 1 si l'API a répondu ce cycle, sinon 0 |
| `senhub.ad_hybrid.sync.health` | Gauge `1` | `senhub.ad_hybrid.service.name` | Healthy=2, Warning=1, Error/autre=0 |
| `senhub.ad_hybrid.sync.agents.healthy` | Gauge `{agent}` | `…service.name` | agents de sync en état healthy |
| `senhub.ad_hybrid.sync.agents.total` | Gauge `{agent}` | `…service.name` | agents de sync enregistrés |
| `senhub.ad_hybrid.sync.export_errors` | Gauge `{error}` | `…service.name` + `senhub.ad_hybrid.error.bucket` | erreurs d'export d'annuaire par bucket |
| `senhub.ad_hybrid.agent.last_seen` | Gauge `s` | `…service.name` + `senhub.ad_hybrid.agent.server` | secondes depuis le dernier report de l'agent |

### 4.38 Probe `exchange_online` (Exchange Online)

Flux de messagerie et santé de service Exchange Online (API reporting Microsoft
365). Pas de semconv OTel — métriques sous `senhub.exchange_online.*`. Émission :
ids courts snake_case côté probe (enterprise `probes/exchange_online/`), déclarés
par le transformer `transformers/definitions/exchange_online.yaml`.

| Metric | Type / unit | Attributes | Notes |
|---|---|---|---|
| `senhub.exchange_online.up` | Gauge `1` | — | 1 si l'API a répondu ce cycle, sinon 0 |
| `senhub.exchange_online.service.health` | Gauge `1` | `senhub.exchange_online.service.display_name` | Healthy=2, Degraded=1, Error/autre=0 |
| `senhub.exchange_online.mail.sent` | Counter `{mail}` | — | messages envoyés (fenêtre de reporting) |
| `senhub.exchange_online.mail.received` | Counter `{mail}` | — | messages reçus |
| `senhub.exchange_online.mail.delivered` | Counter `{mail}` | — | messages délivrés |
| `senhub.exchange_online.mail.failed` | Counter `{mail}` | — | messages en échec de délivrance |
| `senhub.exchange_online.mailboxes` | Gauge `{mailbox}` | — | nombre total de boîtes aux lettres |
| `senhub.exchange_online.mailboxes.active` | Gauge `{mailbox}` | — | boîtes actives |
| `senhub.exchange_online.mailbox.storage.used` | Gauge `By` | — | stockage total consommé (tous mailboxes) |
| `senhub.exchange_online.mailbox.quota_exceeded` | Gauge `{mailbox}` | — | boîtes ayant dépassé le quota d'avertissement |

### 4.39 Probe `hyperv_ha`

Hyper-V Replica and Windows Failover Cluster health, read from local WMI
(`root\virtualization\v2`, `root\MSCluster`). No OTel semconv exists for
Hyper-V HA; all metrics live under the `senhub.hyperv_ha.*` extension namespace.
Cluster metrics are emitted only when the Failover Clustering feature is present.

| Metric | Type / unité | Attributes | Notes |
|---|---|---|---|
| `senhub.hyperv_ha.up` | Gauge `1` | — | 1 si le namespace WMU Replica a répondu ce cycle, sinon 0 |
| `senhub.hyperv_ha.replica.health` | Gauge `1` | `senhub.hyperv_ha.vm.name` | Replication health (1 = Normal, 0 = Warning/Critical) |
| `senhub.hyperv_ha.replica.state` | Gauge `1` | `senhub.hyperv_ha.vm.name` | Valeur brute `ReplicationState` |
| `senhub.hyperv_ha.replica.lag` | Gauge `s` | `senhub.hyperv_ha.vm.name` | Secondes depuis la dernière réplication réussie |
| `senhub.hyperv_ha.cluster.node.state` | Gauge `1` | `senhub.hyperv_ha.cluster.node` | Node state (1 = Up, 0 = Down/Paused/Joining) |
| `senhub.hyperv_ha.cluster.group.state` | Gauge `1` | `senhub.hyperv_ha.cluster.group` | Resource-group state (1 = Online, 0 = Offline/Failed/Partial) |

### 4.40 Probe `mssql_ha`

SQL Server AlwaysOn Availability Group replication health. No OTel semconv
pour la réplication AG ; métriques sous `senhub.mssql_ha.*` (même statut que
`senhub.veeam.*`). Complète la probe `mssql` (lecture seule, semconv `sqlserver.*`).

| Metric | Type / unité | Attributes | Notes |
|---|---|---|---|
| `senhub.mssql_ha.up` | Gauge `1` | — | 1 si le dernier ping a atteint le serveur ce cycle, sinon 0 |
| `senhub.mssql_ha.replica.role` | Gauge `1` | `senhub.mssql_ha.ag.name`, `senhub.mssql_ha.replica.name` | Rôle du réplica (Primary=1, Secondary=0) |
| `senhub.mssql_ha.replica.health` | Gauge `1` | `…ag.name`, `…replica.name` | Synchronisation health (Healthy=1, otherwise 0) |
| `senhub.mssql_ha.replica.connected` | Gauge `1` | `…ag.name`, `…replica.name` | Connectivité (Connected=1, Disconnected=0) |
| `senhub.mssql_ha.database.lag` | Gauge `s` | `…ag.name`, `senhub.mssql_ha.database.name` | Lag estimé du réplica secondaire |
| `senhub.mssql_ha.log_send_queue` | Gauge `By` | `…ag.name`, `…database.name` | Log sur le primaire pas encore envoyé au secondaire |
| `senhub.mssql_ha.redo_queue` | Gauge `By` | `…ag.name`, `…database.name` | Log reçu par le secondaire pas encore rejoué |
| `senhub.mssql_ha.log_send_rate` | Gauge `By/s` | `…ag.name`, `…database.name` | Log send rate, primary → secondary |
| `senhub.mssql_ha.redo_rate` | Gauge `By/s` | `…ag.name`, `…database.name` | Log redo rate on the secondary |

### 4.41 Probe `oracle_enterprise` (Oracle EE / Diagnostics Pack)

Performance et disponibilité d'Oracle Database Enterprise Edition avec l'option
Diagnostics Pack (vues v$sysmetric, v$active_session_history, gv$ RAC,
v$dataguard_stats). Pas de semconv OTel — métriques sous
`senhub.oracle_enterprise.*` (même statut que `senhub.veeam.*`). Émission : ids
courts snake_case côté probe, déclarés par le transformer
`transformers/definitions/oracle_enterprise.yaml`.

| Metric | Type / unité | Attributes | Notes |
|---|---|---|---|
| `senhub.oracle_enterprise.up` | Gauge `1` | — | 1 si l'instance a répondu ce cycle, sinon 0 |
| `senhub.oracle_enterprise.awr.db_time` | Gauge `s` | — | DB time par seconde (v$sysmetric) |
| `senhub.oracle_enterprise.awr.db_cpu` | Gauge `s` | — | DB CPU par seconde |
| `senhub.oracle_enterprise.awr.parse.hard` / `.soft` | Gauge `{parse}/s` | — | hard / soft parses par seconde |
| `senhub.oracle_enterprise.awr.logical_reads` / `physical_reads` / `physical_writes` | Gauge `{read}/s` / `{write}/s` | — | reads / writes par seconde |
| `senhub.oracle_enterprise.awr.executions` | Gauge `{execution}/s` | — | exécutions SQL par seconde |
| `senhub.oracle_enterprise.ash.active_sessions` | Gauge `{session}` | `senhub.oracle_enterprise.wait_class` | sessions actives (5 min) par wait class |
| `senhub.oracle_enterprise.ash.cpu_sessions` | Gauge `{session}` | — | sessions actives sur CPU (5 min) |
| `senhub.oracle_enterprise.rac.instances` | Gauge `{instance}` | — | instances de cluster ouvertes (gv$instance) |
| `senhub.oracle_enterprise.rac.network.io` | Counter `By` | `senhub.oracle_enterprise.rac.instance` | octets SQL*Net cumulés, par instance RAC |
| `senhub.oracle_enterprise.rac.gc.blocks_received` | Counter `{block}` | `senhub.oracle_enterprise.rac.instance` | blocs global-cache CR reçus (cumul), par instance |
| `senhub.oracle_enterprise.dataguard.apply_lag` / `transport_lag` | Gauge `s` | — | apply / transport lag du standby (v$dataguard_stats) |

### 4.42 Probe `vsphere_ha` (VMware vSphere HA — vSAN + NSX-T)

vSphere HA health from a vCenter: vSAN health (govmomi vSAN health) and,
optionnellement, état de l'overlay NSX-T (API REST du NSX manager). Pas de semconv
OTel — métriques sous `senhub.vsphere_ha.*`. NSX-T n'est interrogé que si
`nsx_endpoint` + `nsx_username` sont configurés.

| Metric | Type / unité | Attributes | Source |
|---|---|---|---|
| `senhub.vsphere_ha.up` | Gauge `1` | — | 1 si la session vCenter est vivante et vSAN a répondu, sinon 0 |
| `senhub.vsphere_ha.vsan.health` | Gauge `1` | `senhub.vsphere_ha.cluster.name` | `overallHealth` (green=2, yellow=1, red/autre=0) |
| `senhub.vsphere_ha.vsan.disk_groups` | Gauge `{group}` | `…cluster.name` | nombre de `physicalDisksHealth` |
| `senhub.vsphere_ha.vsan.objects` | Gauge `{object}` | `senhub.vsphere_ha.vsan.object.state` (`healthy`/`degraded`) + `…cluster.name` | `objectHealth.objectHealthDetail` |
| `senhub.vsphere_ha.vsan.resync` | Gauge `By` | `…cluster.name` | `totalBytesToSync` |
| `senhub.vsphere_ha.nsx.manager.health` | Gauge `1` | — | `mgr_connectivity_status == CONNECTED` |
| `senhub.vsphere_ha.nsx.transport_nodes.total` / `.up` | Gauge `{node}` | — | `/transport-nodes/status` |
| `senhub.vsphere_ha.nsx.logical_switches` | Gauge `{switch}` | — | `/logical-switches.result_count` |
| `senhub.vsphere_ha.nsx.edge_cluster.health` | Gauge `1` | `senhub.vsphere_ha.nsx.edge_cluster.id` | `/edge-clusters` (1 si tous membres UP, sinon 0) |

### 4.43 Probe `os_updates` (free, #603)

Posture de patching OS de la machine locale. Aucun receiver otelcol-contrib ne
couvre ce domaine → namespace `senhub.os.updates.*`. Probe host-local
cross-platform ; le backend natif interrogé (apt, dnf/yum, Windows Update Agent)
est porté par l'attribut `os.package_manager` (`apt` | `dnf` | `yum` | `wua`),
mappé depuis le tag `package_manager`. Requêtes read-only, sans escalade de
privilèges.

| OTel metric | Unit | Type | Source wire |
|---|---|---|---|
| `senhub.os.updates.up` | `1` | gauge | 1 quand le backend a répondu, 0 sinon (backend KO ou plateforme non supportée — darwin) |
| `senhub.os.updates.pending` | `{update}` | gauge | apt-check / `apt-get -s upgrade` (lignes `Inst`) / `dnf -q updateinfo list` / WUA `Search("IsInstalled=0 and IsHidden=0 and Type='Software'")` |
| `senhub.os.updates.pending.security` | `{update}` | gauge | volet security du même backend : champ 2 d'apt-check, origines `*-security`, `updateinfo list --security`, MsrcSeverity ou catégorie "Security Updates" (WUA) |
| `senhub.os.updates.reboot_required` | `1` | gauge | `/var/run/reboot-required` (apt), `needs-restarting -r` exit 1 (dnf/yum), `Microsoft.Update.SystemInfo.RebootRequired` (WUA) |

En échec backend, seul `senhub.os.updates.up=0` est émis — dégradation douce,
la série ne disparaît pas. Remplace le workaround historique `exec` +
script apt-check déployé à la main (et couvre enfin Windows).

## 6. Processus d'ajout d'une convention

1. Lire les sources §1 pour le domaine concerné
2. Si convention existe → adopter telle quelle (attributs, unités, types)
3. Si inexistante → créer sous `senhub.*`, documenter ici avec :
   - Justification (pourquoi pas de convention existante)
   - Sources consultées (liens)
   - Alignement sur un pattern existant (windows_exporter, node_exporter…) si pertinent
4. Valider avec l'équipe avant publication
5. Mettre à jour le YAML de la probe concernée

## 6bis. Single vocabulary, two transports — Prometheus + OTLP

À partir de **0.1.89-beta** l'agent expose les mêmes métriques via deux
transports : pull Prometheus (`/metrics`) et push OTLP/gRPC (storage
`otlp`). Les deux chemins consomment **le même flux d'`OtelRecord`**
produit par `internal/agent/services/data_store/otelmapper/`.

```
probe data
   │
   ▼
otelmapper.Resolve  ──►  []OtelRecord  ──┬──►  Prometheus serializer  →  /metrics
                                          │
                                          └──►  OTLP exporter           →  otelcol / vmagent
```

**Conséquence pratique :** le chemin OTLP n'introduit aucune nouvelle
convention. Tout ce qui est documenté dans §4 s'applique à l'identique
côté push. Ce que change le mapper de sortie :

| Sink              | Préfixe `senhub_` | Dots dans nom | Suffixes d'unité  | Ratios (`unit:1`) |
|-------------------|-------------------|---------------|--------------------|-------------------|
| Prometheus        | ajouté            | `_`           | `_seconds/_bytes/...` | converti côté serializer |
| OTLP (wire OTLP)  | **non**           | `.` conservés | absent (porté par le champ `unit`) | géré côté mapper |

Le `prometheusremotewrite` du collecteur applique ensuite ses propres
règles, qui correspondent **exactement** aux règles du serializer
Prometheus de l'agent — sauf le préfixe `senhub_` qui est local au
serializer. Un opérateur qui ingère le push OTLP dans VictoriaMetrics
interroge :

```promql
# Push OTLP via collecteur (prometheusremotewrite)
system_memory_usage_bytes{system_memory_state="used"}
# Pull Prometheus direct
senhub_system_memory_usage_bytes{system_memory_state="used"}
```

Les **dimensions** (probe_name, probe_type, attributs sémantiques type
`cpu.mode`, `system.memory.state`, `hw.state`) sont **identiques** sur
les deux chemins. Aliasing PromQL → un seul vocabulaire à apprendre.

### Resource attributes (OTLP only)

Le push OTLP attache des **resource attributes** par batch que le pull
Prometheus n'a pas (Prometheus colle ces dimensions sur chaque série
directement). Mappage standard :

| Attribut OTel              | Source côté agent                            |
|----------------------------|----------------------------------------------|
| `service.name`             | `storage[otlp].params.resource.service.name` (défaut `senhub-agent`) |
| `service.instance.id`      | 8 premiers caractères de `agent.key` par défaut, override possible   |
| `service.version`          | version de build (ldflags)                   |
| `deployment.environment`   | operator override                            |
| Extras                     | n'importe quel autre couple clé-valeur sous `resource:` |

Les receivers convertissent généralement ces attributs en labels
Prometheus via `resource_to_telemetry_conversion: enabled: true` côté
collector. Sans cette option, le push OTLP perd `service.name` dans
VictoriaMetrics — bug courant à diagnostiquer.

### Logs signal — convention OTel respectée

Le signal logs (probes `syslog`, `event`, `linux_logs`) est purement
OTel : aucune convention `senhub.*` au niveau du log record lui-même,
les attributs sont les attributs standards (`syslog.facility`,
`syslog.hostname`, `syslog.appname`, `host.name`, `systemd.unit`,
`process.pid`, `process.executable.name`). Seul le payload du probe
`event` (libre par construction) est namespacé `senhub.event.*`.

Mapping severité : la table RFC 5424 → OTel SeverityNumber appliquée
côté producteur (helper `agentstate.SyslogPriorityToSeverity`). Les
chemins du probe `event` (qui accepte des sévérités texte type EMERG,
ERR, WARNING, …) utilisent une table équivalente — mêmes valeurs
numériques en sortie OTel.

## 6ter. Entity events — contrat wire + filtrage

Le rail entités suit la **spec OTel entity-events mergée** (modèle
embarqué). Les events sont des LogRecords transportés sur le signal logs
OTLP ; il n'existe **aucun event de relation séparé**.

### Marqueurs sur le fil

| Niveau | Marqueur | Valeur |
|---|---|---|
| Scope | `scope.name` | `senhub-agent/otlp-entities` |
| Scope | attribut `otel.entity.entity_event` | `true` |
| LogRecord | `EventName` | `entity.state` \| `entity.delete` |
| LogRecord | attributs nus | `entity.type`, `entity.id.*`, `entity.description.*`, `entity.report.interval` |
| LogRecord | `entity.relationships` | tableau embarqué de descripteurs nus `{relationship.type, entity.type, entity.id}` |

Les relations vivent **dans** l'event `entity.state` de leur entité
source ; une relation que la source cesse de lister est retirée
(removal-by-absence). Il n'y a donc rien d'autre à router que les events
d'entité eux-mêmes.

### Filtre otelcol canonique

Pour ne transmettre que les entity events à un consommateur dédié
(backend de graphe type Toise), filtrer sur la présence de `entity.type` :

```yaml
processors:
  filter/entity_events:
    error_mode: ignore
    logs:
      log_record:
        - 'attributes["entity.type"] == nil'   # drop tout ce qui n'est pas un entity event
```

Un seul prédicat suffit : les relations étant embarquées, **elles suivent
automatiquement** — aucun second marqueur à conserver.

### Note historique (#227)

Le modèle pré-mergé (retiré au resync #222, lots 0a/0b) émettait les
relations comme enregistrements séparés sous `entity.relation.event.type`.
Un filtre qui ne gardait que `otel.entity.event.type` perdait alors
**silencieusement** toutes les relations (constaté sur le POC Toise du
2026-06-05 : 5 entités, 0 relation, zéro erreur). Ce mode de défaillance
est impossible par construction dans le modèle embarqué — c'est l'une des
raisons du choix. Si un déploiement expose encore un filtre à deux
marqueurs, il date de l'ancien modèle et peut être réduit au prédicat
unique ci-dessus.

## 6quater. Corrélation cross-signal — contexte agent (#294)

Objectif : rendre métriques, logs et traces **joignables** dans les
backends finaux. Les backends joignent au niveau **Resource** (attributs
indexés). Trois signaux, deux régimes d'identité :

- **Signaux propres de l'agent** (métriques, logs, spans générés par
  l'agent) : partagent la **même Resource** — `host.id`, `host.name`,
  `service.instance.id`, `deployment.environment`, + `global_tags`
  (tenant/site/region). Corrélation forte, native.
- **Traces relayées** (spans reçus d'apps tierces via le receiver OTLP,
  réémis) : portent la Resource de **l'app émettrice** (son propre
  `service.name`/`service.instance.id`/`host.*`). Identité étrangère,
  **jamais écrasée**.

### Enrichissement des traces relayées (`relay_enrichment`, défaut on)

Au flush du relay, **merge-not-overwrite**, copy-on-write sur la Resource
(les spans sont partagés, jamais mutés). **Clés standard / opérateur
uniquement — aucun attribut à namespace produit** :

| Attribut | Régime | Source |
|---|---|---|
| `tenant` / `site` / `region` | inséré **si absent** | `global_tags` de l'agent |
| `deployment.environment` | inséré **si absent** | environnement de l'agent |

`service.*` / `host.*` posés par l'app ne sont **jamais** touchés. Override
par source : `signals.traces.relay_tenant_overrides` (`match: {key,value}` →
`tags:`) pour le cas passerelle mono-agent multi-clients.

> **Marqueur « relayé-par » différé.** Un marqueur d'identité de l'agent
> relayeur (« quel agent a relayé cette trace ») serait utile pour joindre
> la trace au nœud host dans le graphe topologie. Mais OTel n'a **aucune
> clé ratifiée** pour l'identité d'un relais/collecteur sur de la télémétrie
> pass-through, et on ne bake pas de nom produit dans un contrat qu'on veut
> standard. Le nom de cette clé est donc à **aligner avec Toise + le SIG
> Semconv** avant introduction (#698) — d'ici là, l'enrichissement reste
> 100 % clés standard.

### Vérité de corrélation (contrat de jointure)

**`service.instance.id` n'est PAS une clé de jointure** entre la télémétrie
de l'agent et une trace tierce relayée — ce sont des services différents.
Un lien Grafana trace→metrics construit dessus renverra vide (correctement).
La jointure réelle et utile : **tenant/site** — pivot « trace app lente →
télémétrie d'infra du même tenant ». Les clés garanties cross-signal :
`tenant`, `site`/`region`, `deployment.environment` (insert-only partout).
La jointure par **host** entre agent et trace tierce nécessiterait un
marqueur d'identité de l'agent relayeur, **différé** faute de clé standard
(voir l'encart « marqueur relayé-par », #698).

> Note : les *exemplars* (trace_id sur datapoints) sont le mécanisme OTel
> natif metric→trace ; non applicable ici (les métriques de l'agent sont
> collectées hors contexte de trace actif). Hors périmètre.

## 7. Versioning

Ce document n'a pas (encore) de numéro de version. Une fois la V1 complète (15 probes mappées) publiée dans 0.1.88, il passera en SemVer 1.0.0. Tout changement de nom/attribut/unité = major bump.
