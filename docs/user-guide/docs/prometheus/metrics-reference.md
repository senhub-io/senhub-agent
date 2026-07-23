# Prometheus Metrics Reference

Complete catalog of metrics exposed by SenHub Agent on the `/metrics`
endpoint, grouped by probe. Names follow the OTel → Prometheus
conversion rules — see the [Prometheus guide](index.md)
for the underlying naming convention.

> **Reading the tables:**
> - **Prometheus name** is what you query in PromQL / VictoriaMetrics.
> - **Type** is `gauge` (snapshot value), `counter` (monotonically increasing,
>   use `rate()`), or `updowncounter` (can go up and down — emitted as a
>   gauge in Prometheus).
> - **Key labels** lists labels specific to this metric (in addition to
>   the systematic `probe_name`, `probe_type` and `custom_tags` labels).

## Agent self-observability

Always emitted when the `prometheus` endpoint is active, regardless of
configured probes.

| Prometheus name | Type | Description | Key labels |
|---|---|---|---|
| `senhub_agent_uptime_seconds` | gauge | Process uptime since start | – |
| `senhub_agent_cache_entries` | gauge | Distinct time series in shared cache | – |
| `senhub_agent_probes_active` | gauge | Probes that have emitted ≥1 datapoint in cache window | – |
| `senhub_agent_probes_total` | gauge | Configured probes currently running | – |
| `senhub_agent_probes_healthy` | gauge | Probes reporting `IsHealthy() == true` | – |
| `senhub_agent_collect_errors_total` | counter | Probe collection errors since start, per probe type and failure reason | `probe`, `reason` |
| `senhub_agent_transformer_fallback_total` | counter | Datapoints processed without a transformer definition (no unit injection or corrections) | – |
| `senhub_agent_otlp_receiver_ingested_total` | counter | Items accepted by the OTLP receiver, per signal (metrics datapoints, log records, spans) | `signal` |
| `senhub_agent_otlp_receiver_dropped_total` | counter | Items the OTLP receiver discarded, by signal and reason (`no_sink`, `unmapped`) | `signal`, `reason` |
| `senhub_agent_http_requests_total` | counter | HTTP requests served per route template | `endpoint` |
| `senhub_agent_build_info` | gauge (=1) | Agent build metadata | `version`, `commit` |

`senhub_agent_collect_errors_total` carries two bounded labels so a spike can be
attributed without log correlation:

- `probe` — the probe **type** (`cpu`, `redfish`, `mysql`, …), not the
  per-instance name, so cardinality stays bounded by the registry.
- `reason` — `collect` (a `Collect()` cycle failed), `timeout` (that failure was
  a deadline/timeout) or `route` (routing the collected data to an output
  strategy failed).

Each series appears only once its first error occurs; sum across labels for the
lifetime total: `sum(senhub_agent_collect_errors_total)`.

## System probes

Probes flagged `host_level: true` — filtered out when
`expose_host_metrics: false` is set.

### CPU (`type: cpu`)

OTel-aligned via `system.cpu.*`. Time-mode breakdown collapsed under
`cpu_mode` label.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_system_cpu_utilization_ratio` | gauge | 1 (0-1) | `cpu_mode` ∈ {user, system, idle, nice, iowait, interrupt, softirq, steal}; per-core via `cpu_logical_number`; `senhub_cpu_perfmon_instance` on Windows |
| `senhub_system_cpu_load_1m` | gauge | {thread} | – (Unix-family) |
| `senhub_system_cpu_load_5m` | gauge | {thread} | – (Unix-family) |
| `senhub_system_cpu_load_15m` | gauge | {thread} | – (Unix-family) |
| `senhub_system_cpu_dpcs_per_second` | gauge | 1/s | `cpu_logical_number`, `senhub_cpu_perfmon_instance` (Windows) |
| `senhub_system_cpu_dpcs_queued_per_second` | gauge | 1/s | – |
| `senhub_system_cpu_interrupts_per_second` | gauge | 1/s | `cpu_logical_number`, `senhub_cpu_perfmon_instance` (Windows) |
| `senhub_system_cpu_queue_length` | gauge | {thread} | – |
| `senhub_system_processes_count` | gauge | {process} | – |

Notes (since 0.1.91):
- **Per-mode utilization** is exposed as `senhub_system_cpu_utilization_ratio` with a `cpu_mode` label, value in [0,1]. On Unix the probe diffs `cpu.Times()` between two collect cycles; on Windows it reads `\Processor\% User Time` and siblings directly from PDH. The previous `senhub_system_cpu_time_seconds_total` counter was retired because Windows could not produce a true cumulative seconds value through PDH — the metric is now honest about being a gauge rate.
- **Cross-OS harmonization**: Windows `privileged_time` is mapped to `cpu_mode="system"` so one PromQL query reflects kernel time on both Linux and Windows.
- **Load average** dropped the `linux.` infix in 0.1.91. The metric is emitted on every Unix-family host (Linux, BSD, macOS) — Windows still has no load average concept and does not emit these series.

### Memory (`type: memory`)

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_system_memory_limit_bytes` | updowncounter | By | – |
| `senhub_system_memory_usage_bytes` | updowncounter | By | `system_memory_state` ∈ {used, free, buffers, cached, committed, modified, nonpaged_pool, paged_pool} |
| `senhub_system_memory_utilization_ratio` | gauge | 1 (0-1) | – |
| `senhub_system_paging_utilization_ratio` | gauge | 1 (0-1) | `system_paging_state="used"` (Windows pagefile) |
| `senhub_system_paging_utilization_peak_ratio` | gauge | 1 (0-1) | – (Windows pagefile peak) |
| `senhub_system_paging_faults_per_second` | gauge | 1/s | – (Windows page faults) |
| `senhub_system_paging_operations_per_second` | gauge | 1/s | `direction` ∈ {in, out} (Windows pages in/out) |

Notes:
- **`system_memory_state="free"`** receives both Linux `MemFree` and
  Windows `Available` (harmonized — Available ≈ free in Windows
  semantics).
- Windows-only states (`committed`, `modified`, `nonpaged_pool`,
  `paged_pool`) are extension values for `system_memory_state`.

### Network (`type: network`)

100 % OTel-native — no senhub.* extensions.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_system_network_io_bytes_per_second` | gauge | By/s | `network_interface_name`, `network_io_direction` ∈ {receive, transmit}, `ip` (primary IP) |
| `senhub_system_network_packet_count_per_second` | gauge | {packet}/s | `network_interface_name`, `network_io_direction` |
| `senhub_system_network_errors_per_second` | gauge | {error}/s | `network_interface_name`, `network_io_direction` |
| `senhub_system_network_packet_dropped_per_second` | gauge | {packet}/s | `network_interface_name`, `network_io_direction` |

Notes (since 0.1.91):
- These four metrics ship as **gauges in `_per_second`**, not cumulative counters. The agent already computes the rate (probe-side delta over the collect interval, or `\Network Interface\Bytes Sent/sec` on Windows), so wrapping the metric in `rate()` would be wrong — query the gauge directly.
- Only the **primary IP** is exposed via the `ip` label. The positional `ip_1, ip_2, … ip_N` labels of pre-0.1.91 releases produced unstable cardinality on interfaces with multiple IPv6 addresses and have been dropped.

### Logical Disk → Filesystem (`type: logicaldisk`)

The probe type identifier remains `logicaldisk` for backward compatibility,
but the metrics it emits use the OTel `system.filesystem.*` convention
(plus `senhub.system.disk.*` for Windows I/O rates and
`senhub.system.filesystem.inode.*` for Linux inode counters).

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_system_filesystem_limit_bytes` | updowncounter | By | `system_device`, `system_filesystem_mountpoint`, `system_filesystem_type` |
| `senhub_system_filesystem_usage_bytes` | updowncounter | By | `system_filesystem_state` ∈ {used, free, available} + identifiers above |
| `senhub_system_filesystem_utilization_ratio` | gauge | 1 (0-1) | `system_filesystem_state` ∈ {used, free} + identifiers |
| `senhub_system_filesystem_inode_limit` | updowncounter | – | (Linux only) |
| `senhub_system_filesystem_inode_usage` | updowncounter | – | `system_filesystem_state` ∈ {used, free} (Linux) |
| `senhub_system_filesystem_inode_utilization_ratio` | gauge | 1 (0-1) | (Linux) |
| `senhub_system_disk_operations_per_second` | gauge | 1/s | `disk_io_direction` ∈ {read, write} (Windows) |
| `senhub_system_disk_io_bytes_per_second` | gauge | By/s | `disk_io_direction` (Windows) |
| `senhub_system_disk_queue_length` | gauge | {operation} | (Windows) |

Notes:
- Windows drive letters and Linux mount points both surface as
  `system_filesystem_mountpoint`.
- The `system_filesystem_state="available"` extension carries the Linux
  `statfs.f_bavail` (free space for non-root) — distinct from `free`.

## Network probes

### Ping Gateway (`type: ping_gateway`) and Ping WebApp (`type: ping_webapp`)

ICMP probes share the same metric names; `ping_webapp` adds a `url_full`
label to distinguish targets.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_probe_icmp_duration_seconds` | gauge | s | `url_full` (ping_webapp only) |
| `senhub_probe_icmp_packet_loss_ratio` | gauge | 1 (0-1) | `url_full` (ping_webapp only) |

### Load WebApp (`type: load_webapp`)

HTTP phase timing aligned on `blackbox_exporter` convention.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_probe_http_duration_seconds` | gauge | s | `phase` ∈ {resolve, connect, tls, processing, total}, `url_full` |

### WiFi Signal Strength (`type: wifi_signal_strength`)

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_system_network_wifi_signal_strength_dbm` | gauge | dBm | `senhub_network_wifi_ssid`, `senhub_network_wifi_bssid` |
| `senhub_system_network_wifi_quality_ratio` | gauge | 1 (0-1) | `senhub_network_wifi_ssid`, `senhub_network_wifi_bssid` |

## Active checks

All four check probes share the same semantics: a failing target is a
measurement (`up = 0`), never a missing series. Durations are seconds.

### ICMP Check (`type: icmp_check`)

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_icmp_up_ratio` | gauge | bool 0/1 | `icmp_target`, `icmp_target_ip` |
| `senhub_icmp_packet_loss_ratio` | gauge | 1 (0-1) | `icmp_target` |
| `senhub_icmp_packets_sent` / `_received` | gauge | {packet} | `icmp_target` |
| `senhub_icmp_rtt_min_seconds` / `_avg_` / `_max_` / `_stddev_` | gauge | s | `icmp_target` |

### HTTP Check (`type: http_check`)

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_httpcheck_up_ratio` | gauge | bool 0/1 | `httpcheck_target` |
| `senhub_httpcheck_status_code` | gauge | {code} | `httpcheck_target` |
| `senhub_httpcheck_duration_seconds` | gauge | s | `httpcheck_target` |
| `senhub_httpcheck_duration_dns_seconds` / `_connect_` / `_tls_` / `_ttfb_` | gauge | s | `httpcheck_target` |
| `senhub_httpcheck_response_size_bytes` | gauge | By | `httpcheck_target` |
| `senhub_httpcheck_tls_expiry` | gauge | days (negative once expired) | `httpcheck_target` (TLS targets only) |
| `senhub_httpcheck_content_match_ratio` | gauge | bool 0/1 | `httpcheck_target` (only with `content_match`) |

### TCP Dial (`type: tcp_dial`)

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_tcpdial_up_ratio` | gauge | bool 0/1 | `tcpdial_target` |
| `senhub_tcpdial_duration_seconds` | gauge | s | `tcpdial_target` |

### DNS Latency (`type: dns_latency`)

One series per (name x resolver) pair.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_dns_up_ratio` | gauge | bool 0/1 | `dns_question_name`, `dns_resolver` |
| `senhub_dns_lookup_duration_seconds` | gauge | s | `dns_question_name`, `dns_resolver` |
| `senhub_dns_answers` | gauge | {answer} | `dns_question_name`, `dns_resolver` |

## SNMP

### SNMP Poll (`type: snmp_poll`)

One series per device (`snmp_target`); interface metrics add
`network_interface_index`. Counters are raw — compute rates in the
backend (`rate(...[5m])`).

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_snmp_up_ratio` | gauge | bool 0/1 | `snmp_target` |
| `senhub_snmp_poll_duration_seconds` | gauge | s | `snmp_target` |
| `senhub_snmp_sys_uptime` | gauge | centiseconds (native SNMP unit) | `snmp_target` |
| `senhub_snmp_interface_in_octets_bytes_total` / `_out_` | counter | By | `snmp_target`, `network_interface_index` |
| `senhub_snmp_interface_in_errors_total` / `_out_` | counter | {error} | `snmp_target`, `network_interface_index` |
| `senhub_snmp_interface_in_discards_total` / `_out_` | counter | {packet} | `snmp_target`, `network_interface_index` |
| `senhub_snmp_interface_speed_bits_per_second` | gauge | bit/s | `snmp_target`, `network_interface_index` |
| `senhub_snmp_interface_admin_status` / `_oper_status` | gauge | IF-MIB enum (1=up, 2=down, ...) | `snmp_target`, `network_interface_index` |

Custom mappings and dynamic OIDs surface under the name configured in
`custom_mappings` (typed pass-through; counters gain `_total`).

### SNMP Trap (`type: snmp_trap`)

Trap payloads ride the OTLP **log** rail; only the receiver's
self-metrics appear here.

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_snmp_trap_rejected_community_total` | counter | {datagram} | – |
| `senhub_snmp_trap_decode_panics_total` | counter | {datagram} | – |

## Universal ingestion

### Prometheus Scrape (`type: prometheus_scrape`)

Scraped series are re-exported under the `senhub_` prefix with their
original name and labels; counter/gauge types are preserved (counters
gain `_total` if absent). Self-metrics:

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_promscrape_up_ratio` | gauge | bool 0/1 | `promscrape_target` |
| `senhub_promscrape_scrape_duration_seconds` | gauge | s | `promscrape_target` |
| `senhub_promscrape_samples` | gauge | {sample} | `promscrape_target` |
| `senhub_promscrape_dropped` | gauge | {sample} (histogram/summary series) | `promscrape_target` |

### Exec (`type: exec`)

Perfdata / JSON metrics surface as `senhub_exec_<label>` pass-through
gauges (or counters with `_total` for the `c` UOM), time normalized to
seconds and bytes to bytes. Self-metrics:

| Prometheus name | Type | Unit | Key labels |
|---|---|---|---|
| `senhub_exec_status` | gauge | 0 ok / 1 warning / 2 critical / 3 unknown | – |
| `senhub_exec_duration_seconds` | gauge | s | – |
| `senhub_exec_timeout_ratio` | gauge | bool 0/1 | – |
| `senhub_exec_skipped_ratio` | gauge | bool 0/1 (overlap guard) | – |

## Event-conduit probes (skipped)

The `syslog`, `event`, `filetail`, `windows_eventlog` and `linux_logs`
probes are **log-flow conduits** — they relay records, they don't
produce metric time series here. Their records ship through the OTLP
**logs** transport (see the OTLP guide); `snmp_trap` is the same, with
the two self-metrics listed above.

## Hardware (Redfish) — `type: redfish`

Mostly OTel-native via the `hw.*` namespace, plus `senhub.hardware.*`
extensions for RAID pools and Redfish-specific concepts.

### Health (universal `hw.status` pattern)

Every health metric uses **strict OTel expansion** — one series per
possible `hw_state`, value `1` if active else `0`.

| Prometheus name | `hw_type` values | `hw_state` values |
|---|---|---|
| `senhub_hw_status` | `power_supply`, `physical_disk`, `logical_disk`, `disk_controller`, `enclosure` | `ok`, `degraded`, `failed`, `predicted_failure`, `unknown` |

Drive failure prediction is encoded as `hw_state="predicted_failure"` on
the same metric (independent of overall health).

### Drives, volumes, pools (capacity & I/O)

| Prometheus name | Type | Unit | Notes |
|---|---|---|---|
| `senhub_hw_physical_disk_size_bytes` | updowncounter | By | per drive |
| `senhub_hw_logical_disk_limit_bytes` | updowncounter | By | volume capacity |
| `senhub_hw_logical_disk_usage_bytes` | updowncounter | By | with `hw_logical_disk_state` ∈ {used, free} |
| `senhub_hw_logical_disk_utilization_ratio` | gauge | 1 | with `hw_logical_disk_state` |
| `senhub_hardware_logical_disk_io_operations_total` | counter | {operation} | `disk_io_direction` ∈ {read, write} |
| `senhub_hardware_logical_disk_io_bytes_total` | counter | By | `disk_io_direction` |
| `senhub_hardware_logical_disk_encrypted_ratio` | gauge | 1 | bool 0/1 |
| `senhub_hardware_storage_pool_usage_bytes` | updowncounter | By | `senhub_hardware_storage_pool_state` ∈ {allocated, used} |
| `senhub_hardware_storage_pool_utilization_ratio` | gauge | 1 | `senhub_hardware_storage_pool_state` ∈ {free, used} |
| `senhub_hardware_storage_pool_io_operations_total` | counter | {operation} | `disk_io_direction` |
| `senhub_hardware_storage_pool_io_bytes_total` | counter | By | `disk_io_direction` |
| `senhub_hardware_physical_disk_link_speed_bits_per_second` | gauge | bit/s | per drive (Gbps × 1e9) |
| `senhub_hardware_physical_disk_block_size_bytes` | gauge | By | per drive |
| `senhub_hardware_physical_disk_operation_progress_ratio` | gauge | 1 | per drive (mapper ÷100) |
| `senhub_hardware_physical_disk_has_active_operations_ratio` | gauge | 1 | bool |
| `senhub_hardware_physical_disk_location_indicator_active_ratio` | gauge | 1 | bool |

### System / controllers / redundancy

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_hardware_system_power_state_ratio` | updowncounter | strict-OTel expand: `senhub_hardware_system_power_state_value` ∈ {off, on, powering_on, powering_off, unknown} |
| `senhub_hardware_eventservice_status` | updowncounter | with `senhub_hardware_eventservice_state` ∈ {ok, degraded, failed, unknown} |
| `senhub_hardware_redundancy_status` | updowncounter | with `senhub_hardware_redundancy_state`, `senhub_hardware_redundancy_scope` (chassis/storage) |
| `senhub_hardware_redundancy_controllers_count` | updowncounter | with `senhub_hardware_redundancy_bound` ∈ {active, min, max} |

## Veeam Backup & Replication — `type: veeam`

All metrics under `senhub.veeam.*` (no OTel semconv for backup).

### Jobs

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_veeam_jobs_total` | gauge | `senhub_veeam_job_type` |
| `senhub_veeam_jobs_by_last_result` | gauge | `senhub_veeam_job_last_result` ∈ {success, warning, failed, running} |
| `senhub_veeam_job_status` | updowncounter | strict-OTel expand: `senhub_veeam_job_state` ∈ {none, success, warning, failed, running} |
| `senhub_veeam_job_seconds_since_last_run` | gauge (s) | `senhub_veeam_job_name`, `senhub_veeam_job_type` |
| `senhub_veeam_job_objects` | gauge | per job |
| `senhub_veeam_job_bottleneck_status` | updowncounter | expand: `senhub_veeam_job_bottleneck` ∈ {none, source, proxy, network, target} |
| `senhub_veeam_job_last_run_bytes` | gauge (By) | `senhub_veeam_job_data_phase` ∈ {processed, read, transferred} |

### Repository

| Prometheus name | Type | Unit |
|---|---|---|
| `senhub_veeam_repository_limit_bytes` | updowncounter | By |
| `senhub_veeam_repository_usage_bytes` | updowncounter | By + `senhub_veeam_repository_state` ∈ {used, free} |
| `senhub_veeam_repository_utilization_ratio` | gauge | 1 + `senhub_veeam_repository_state="free"` |

### License

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_veeam_license_status` | updowncounter | expand: `senhub_veeam_license_state` ∈ {valid, expired, invalid} |
| `senhub_veeam_license_days_remaining` | gauge | – |
| `senhub_veeam_license_instances` | gauge | `senhub_veeam_license_instances_state` ∈ {total, used, remaining} |

### Proxies, objects, infrastructure

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_veeam_proxy_status` | updowncounter | expand: `senhub_veeam_proxy_state` ∈ {disabled, offline, online} |
| `senhub_veeam_proxies` | gauge | `senhub_veeam_proxies_state` ∈ {total, enabled, disabled} |
| `senhub_veeam_object_restore_points` | gauge | per object |
| `senhub_veeam_object_last_run_failed_ratio` | gauge | bool |
| `senhub_veeam_objects` | gauge | `senhub_veeam_objects_state` ∈ {total, failed} |
| `senhub_veeam_server_status` | updowncounter | expand: `senhub_veeam_server_state` ∈ {unavailable, available} |
| `senhub_veeam_servers` | gauge | `senhub_veeam_servers_state` ∈ {total, available, unavailable} |

## Citrix Virtual Apps and Desktops — `type: citrix`

All metrics under `senhub.citrix.*` (no Citrix CVAD OTel convention; design from scratch).

### Sessions, machines, license

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_citrix_sessions_count` | gauge | `senhub_citrix_session_state` ∈ {connected, disconnected} |
| `senhub_citrix_machines_total` | gauge | – |
| `senhub_citrix_machines_by_registration_state` | gauge | `senhub_citrix_machine_registration_state` ∈ {registered, unregistered, faulty, maintenance} |
| `senhub_citrix_machines_overloaded` | gauge | – |
| `senhub_citrix_machines_multi_session_fault_total` | gauge | – |
| `senhub_citrix_machines_by_fault_state` | gauge | `senhub_citrix_machine_fault_state` ∈ {boot_failure, stuck_at_boot, unregistered, max_capacity, vm_not_found, unknown} |
| `senhub_citrix_license_sessions_active` | gauge | – |
| `senhub_citrix_license_peak_concurrent_users` | gauge | – |
| `senhub_citrix_license_unique_users` | gauge | – |
| `senhub_citrix_license_grace_sessions_remaining` | gauge | – |
| `senhub_citrix_license_grace_active_ratio` | gauge | bool |
| `senhub_citrix_license_grace_time_remaining_seconds` | gauge | – (mapper hours × 3600) |

### Logon performance

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_citrix_logon_duration_1h_average_seconds` | gauge | – |
| `senhub_citrix_logon_last_session_duration_seconds` | gauge | – |
| `senhub_citrix_logon_sessions_opened` | gauge | – |
| `senhub_citrix_logon_phase_duration_seconds` | gauge | `senhub_citrix_logon_phase` ∈ {brokering, vm_start, hdx, authentication, gpo, scripts, profile, interactive} |

### Connection failures, load index

| Prometheus name | Type | Notes |
|---|---|---|
| `senhub_citrix_connection_failures_total` | gauge | – |
| `senhub_citrix_connection_failures_by_category` | gauge | `senhub_citrix_connection_failure_category` ∈ {client_connection, configuration, machine, capacity_unavailable, licenses_unavailable, other} |
| `senhub_citrix_load_index_ratio` | gauge | `senhub_citrix_load_index_dimension` ∈ {effective, cpu, memory, disk, network, sessions} (mapper ÷100) |

## NetScaler / Citrix ADC — `type: netscaler`

100 metrics organized by NITRO entity, all under `senhub.netscaler.*`. The
table below is an overview — see the source YAML
(`internal/agent/services/data_store/transformers/definitions/netscaler.yaml`)
for exhaustive details.

### State enums (strict-OTel expand)

All vServer/service/servicegroup/csvserver/gslbvserver state metrics share
the NITRO state enum mapping (1=down, 2=unknown, 3=busy, 4=out_of_service,
5=trofs, 7=up, 8=trofs_down):

| Metric pattern | strict-OTel expand attribute |
|---|---|
| `senhub_netscaler_lbvserver_status` | `senhub_netscaler_lbvserver_state` |
| `senhub_netscaler_service_status` | `senhub_netscaler_service_state` |
| `senhub_netscaler_servicegroup_status` | `senhub_netscaler_servicegroup_state` |
| `senhub_netscaler_csvserver_status` | `senhub_netscaler_csvserver_state` |
| `senhub_netscaler_gslb_vserver_status` | `senhub_netscaler_gslb_vserver_state` |
| `senhub_netscaler_gslb_site_status` | `senhub_netscaler_gslb_site_state` ∈ {down, up} |
| `senhub_netscaler_gslb_service_status` | `senhub_netscaler_gslb_service_state` ∈ {down, up} |
| `senhub_netscaler_aaa_vserver_status` | `senhub_netscaler_aaa_vserver_state` ∈ {down, up} |
| `senhub_netscaler_vpn_vserver_status` | `senhub_netscaler_vpn_vserver_state` ∈ {down, up} |
| `senhub_netscaler_interface_status` | `senhub_netscaler_interface_state` ∈ {disabled, enabled} |
| `senhub_netscaler_ssl_certificate_status` | `senhub_netscaler_ssl_certificate_state` ∈ {invalid, valid} |
| `senhub_netscaler_ha_role_ratio` | `senhub_netscaler_ha_role_state` ∈ {unknown, secondary, primary} |
| `senhub_netscaler_ha_node_status` | `senhub_netscaler_ha_node_state` ∈ {down, up} |
| `senhub_netscaler_ha_sync_status` | `senhub_netscaler_ha_sync_state` ∈ {failed, success} |

### System (CPU, memory, throughput, HTTP, TCP)

| Prometheus name | Type | Unit | Notes |
|---|---|---|---|
| `senhub_netscaler_system_cpu_utilization_ratio` | gauge | 1 | `senhub_netscaler_cpu_plane` ∈ {data, management} |
| `senhub_netscaler_system_memory_utilization_ratio` | gauge | 1 | – |
| `senhub_netscaler_system_network_throughput_bits_per_second` | gauge | bit/s | `network_io_direction` (Mbps × 1e6) |
| `senhub_netscaler_system_http_messages_rate_per_second` | gauge | 1/s | `senhub_netscaler_http_message_type` ∈ {request, response} |
| `senhub_netscaler_system_tcp_connections_active` | updowncounter | {connection} | `senhub_netscaler_tcp_side` ∈ {client, server} |
| `senhub_netscaler_ns_throughput_bits_per_second` | gauge | bit/s | `senhub_netscaler_traffic_type` ∈ {total, http} |
| `senhub_netscaler_system_network_packets_rate_per_second` | gauge | 1/s | `network_io_direction` |
| `senhub_netscaler_system_network_packets_total` | counter | {packet} | `network_io_direction` |
| `senhub_netscaler_system_dpcs_per_second` etc. | – | – | (windows specifics not present on appliance) |

### Disk (OTel-native filesystem)

The appliance's local disk is mapped to OTel `system.filesystem.*` —
distinguished from host OS filesystem by the `probe_type="netscaler"` label.

### Load balancing / services / GSLB / cache / compression / AAA / VPN / WAF

The remaining metrics follow the same rule set: rx/tx collapsed via
`network.io.direction`; hits/misses, requests/responses, successes/failures
collapsed via dedicated `*_state`/`*_result`/`*_type` attributes; counters
get `_total`. See the YAML definition for the full list of ~65 metric
names in this domain.

## Discovering metrics at runtime

Easiest way to enumerate everything the agent emits in your specific
config:

```bash
# All metric names (one per line)
curl -sf -H "Authorization: Bearer KEY" http://agent:8080/metrics \
  | grep -E '^# TYPE ' | awk '{print $3}' | sort -u

# All series (with their labels)
curl -sf -H "Authorization: Bearer KEY" http://agent:8080/metrics \
  | grep -v '^#' | grep -v '^$'
```

Or query VictoriaMetrics / Prometheus once the scraper has ingested at
least one cycle:

```bash
curl -sf 'http://victoria:8427/api/v1/label/__name__/values' \
  | jq -r '.data[]' | grep '^senhub_'
```
