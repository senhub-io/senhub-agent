# Next (unreleased)

:material-progress-clock: In progress — these changes will ship as the next stable release.

<div class="rn-filter"></div>

## New

<ul class="rn">
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> supports <strong>SNMPv3</strong> (USM auth + privacy, security level derived from the configured protocols). (#156)</li>
<li><span class="tag t-new">New</span> <span class="tag t-area">SNMP</span> <code>snmp_poll</code> gains <code>mib_paths</code>: a custom mapping may omit <code>metric</code> and have its name resolved from operator-supplied MIB files at startup. (#291)</li>
</ul>

## Fixed

<ul class="rn">
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">OTLP</span> Stale series eviction (<code>staleness_ttl</code>, default 10m) — series whose probe was removed or denied no longer re-export forever with fresh timestamps from the persisted store (zombie metrics seen twice in production recettes). (#308)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">topology</span> A transient sweep failure no longer deletes the device tree in the consumer; sources are unregistered on probe shutdown; unchanged entity heartbeats are suppressed between refreshes. (#272)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">SNMP</span> <code>senhub.snmp.up</code> now reflects whether the device answered, not whether a UDP socket opened — a powered-off switch finally reports down. (#156)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">security</span> SNMPv3 passphrases and community strings are masked in logs. (#156)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">security</span> <code>agent config show</code> masks secrets by default; cleartext output now requires the explicit <code>--resolved</code> flag. (#279)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">PRTG</span> The PRTG push closes its HTTP response — it leaked one connection per sync cycle. (#277)</li>
<li><span class="tag t-fixed">Fixed</span> <span class="tag t-area">checks</span> <code>icmp_check</code> defaults to privileged raw sockets on Linux when running as root — unprivileged datagram ICMP is disabled by default on Ubuntu/Debian (<code>ping_group_range</code>) and silently reported every target down; the permission error now carries an actionable hint. (#357)</li>
</ul>

## Changed

<ul class="rn">
<li><span class="tag t-removed">Changed</span> <span class="tag t-area">outputs</span> The never-implemented Zabbix HTTP endpoint (always 501) is removed; <code>endpoints: [zabbix]</code> now fails fast at startup. The Zabbix integration is redesigned as a native active agent, deferred (#169).</li>
</ul>

## Changed (metric names)

The OTel names of eight Windows/hardware rate metrics carried a
`per_second` suffix in the NAME, which the Prometheus-compatibility
layers then doubled with the unit-derived suffix
(`senhub_system_disk_io_per_second_bytes_per_second` through an OTel
collector). The unit now drives the suffix alone; the resulting
Prometheus names are the ones the metrics reference always documented.

| Before (OTel name) | After (OTel name) | Prometheus name (both ingestion paths) |
|---|---|---|
| `senhub.system.cpu.dpcs_per_second` | `senhub.system.cpu.dpcs` | `senhub_system_cpu_dpcs_per_second` |
| `senhub.system.cpu.dpcs_queued_per_second` | `senhub.system.cpu.dpcs_queued` | `senhub_system_cpu_dpcs_queued_per_second` |
| `senhub.system.cpu.interrupts_per_second` | `senhub.system.cpu.interrupts` | `senhub_system_cpu_interrupts_per_second` |
| `senhub.system.paging.faults_per_second` | `senhub.system.paging.faults` | `senhub_system_paging_faults_per_second` |
| `senhub.system.paging.operations_per_second` | `senhub.system.paging.operations` | `senhub_system_paging_operations_per_second` |
| `senhub.system.disk.operations_per_second` | `senhub.system.disk.operations` | `senhub_system_disk_operations_per_second` |
| `senhub.system.disk.io_per_second` | `senhub.system.disk.io` | `senhub_system_disk_io_bytes_per_second` |
| `senhub.hardware.physical_disk.link_speed_bits_per_second` | `senhub.hardware.physical_disk.link_speed` | `senhub_hardware_physical_disk_link_speed_bits_per_second` |

## Internal

<ul class="rn">
<li><span class="tag t-internal">Internal</span> Shared <code>snmpcore</code> package — one printability semantics, value rendering, version mapping and v3 USM tables consumed by both SNMP probes. (#291)</li>
</ul>
