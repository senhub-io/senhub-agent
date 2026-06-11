# Migrating from PRTG

This guide maps PRTG sensor types to SenHub agent probes, shows the
two migration paths (progressive and direct), and states plainly
what the agent does not replace today.

## Why teams migrate

PRTG licenses by sensor count; a single device easily consumes 10+
sensors. The SenHub agent's collection tier is free — host metrics,
SNMP polling and traps, ICMP/HTTP/TCP/DNS checks, log shipping —
with per-device licensing only for the deep vendor probes
(NetScaler, Citrix, Veeam, databases). Data lands in open backends
(Prometheus / VictoriaMetrics, any OTLP collector) instead of a
proprietary store.

## Two migration paths

**Progressive (recommended).** The agent speaks PRTG natively: its
HTTP output serves a JSON format that PRTG's HTTP Data Advanced
sensor consumes directly. Deploy agents next to your existing
install, replace expensive sensor groups with one HTTP Data Advanced
sensor per agent, and keep the PRTG console your team knows. Sensor
count drops immediately; nothing else changes. Move the console to
Grafana later, at your pace.

**Direct.** Deploy agents, point them at Prometheus /
VictoriaMetrics or an OTLP collector, build dashboards in Grafana,
alert with vmalert or Grafana alerting, then decommission PRTG.

## Sensor mapping

### Network checks

| PRTG sensor | Agent probe | License |
|---|---|---|
| Ping | [ICMP check](probes/icmp-check.md) | Free |
| HTTP / HTTP Advanced | [HTTP check](probes/http-check.md) | Free |
| SSL Certificate | [HTTP check](probes/http-check.md) — `tls.expiry` metric | Free |
| Port | [TCP dial](probes/tcp-dial.md) | Free |
| DNS | [DNS latency](probes/dns-latency.md) | Free |

All four are multi-target: one probe instance checks a list of
targets, where PRTG needs one sensor per target.

### SNMP

| PRTG sensor | Agent probe | License |
|---|---|---|
| SNMP Traffic | [SNMP poll](probes/snmp-poll.md) — IF-MIB module | Free |
| SNMP System Uptime | [SNMP poll](probes/snmp-poll.md) — MIB-2 module | Free |
| SNMP Custom / SNMP Custom Advanced | [SNMP poll](probes/snmp-poll.md) — `custom_mappings` | Free |
| SNMP Trap Receiver | [SNMP trap](probes/snmp-trap.md) | Free |

One `snmp_poll` instance covers a whole device — every interface,
uptime, plus any custom OIDs — where PRTG typically burns one sensor
per interface.

Current limits stated plainly: polling is SNMPv2c only (the trap
receiver does v2c and v3); built-in modules cover MIB-2 and IF-MIB,
everything else goes through `custom_mappings` (one OID per line —
there is no MIB-file compiler for polling yet).

### Host metrics

| PRTG sensor | Agent probe | License |
|---|---|---|
| CPU Load (WMI / SSH) | [CPU](probes/cpu.md) | Free |
| Memory (WMI / SSH) | [Memory](probes/memory.md) | Free |
| Disk Free (WMI / SSH) | [Logical disk](probes/logicaldisk.md) | Free |
| Network traffic (WMI / SSH) | [Network](probes/network.md) | Free |

Architectural difference: PRTG polls hosts remotely over WMI or SSH
(credentials on the wire, firewall rules, WMI fragility). The agent
runs on the host and reads the OS directly — no remote credentials,
and the metrics keep flowing during network partitions.

### Logs and events

| PRTG sensor | Agent probe | License |
|---|---|---|
| Windows Event Log (WMI) | [Windows Event Log](probes/windows-eventlog.md) | Free |
| Syslog Receiver | [Syslog](probes/syslog.md) | Pro |
| File / Log (content) | [File tail](probes/filetail.md) | Free |
| HTTP Push Data | [Event](probes/event.md) or [OTLP receiver](probes/otlp-receiver.md) | Pro / Free |

Log-flow probes ship full structured records to a log backend
(VictoriaLogs, Loki, any OTLP collector) — richer than PRTG's
match-count model, and queryable after the fact.

### Vendor infrastructure

| PRTG sensor | Agent probe | License |
|---|---|---|
| Citrix XenDesktop family | [Citrix](probes/citrix.md) | Pro |
| NetScaler family | [NetScaler](probes/netscaler.md) | Pro |
| Veeam Backup family | [Veeam](probes/veeam.md) | Pro |
| Dell / HPE / Lenovo hardware | [Redfish](probes/redfish.md) | Pro |
| MySQL | [MySQL / MariaDB](probes/mysql.md) | Pro |
| PostgreSQL | [PostgreSQL](probes/postgresql.md) | Pro |

## Alerting

PRTG bundles thresholds and notifications per sensor. In the agent
model, collection and alerting are separate: the agent ships
metrics, and alert rules live in the backend — vmalert
(VictoriaMetrics) or Grafana alerting, both of which route to email,
Slack, Teams, PagerDuty and webhooks.

The equivalents of PRTG's stock thresholds are one-line PromQL
rules. Examples:

```yaml
groups:
  - name: senhub-availability
    rules:
      - alert: TargetDown
        expr: senhub_icmp_up_ratio == 0
        for: 3m
      - alert: TLSCertExpiringSoon
        expr: senhub_httpcheck_tls_expiry < 30
      - alert: DiskAlmostFull
        expr: senhub_system_filesystem_utilization_ratio{system_filesystem_state="used"} > 0.9
        for: 10m
      - alert: InterfaceDown
        expr: senhub_snmp_interface_oper_status == 2 and senhub_snmp_interface_admin_status == 1
```

Metric names above are the Prometheus exposition names; the
[metrics reference](prometheus/metrics-reference.md) maps every
probe metric to its exported name.

A ready-to-import rule pack covering the free probes is in
progress; until it ships, the examples above plus the per-probe
metric tables are the reference.

## What the agent does not replace

Stated honestly, so you can plan around it:

- **Flow protocols.** No NetFlow / sFlow / jFlow / IPFIX sensors.
  If you depend on flow analysis, keep that tooling.
- **Maps.** No equivalent of PRTG Maps. Grafana dashboards cover
  the visualization need differently; auto-generated network
  topology views are on the roadmap (topology discovery already
  ships in `snmp_poll`).
- **Reports.** No built-in PDF reporting. Grafana's reporting
  (Enterprise) or grafana-image-renderer fills this gap.
- **Auto-discovery breadth.** PRTG's device auto-discovery
  proposes sensors automatically. The agent's discovery is
  narrower today: LLDP topology crawl in `snmp_poll`.
- **Remote-only polling.** WMI/SSH-style agentless host monitoring
  is intentionally out of scope: the agent runs on the host. For
  unreachable boxes (appliances, locked-down systems), SNMP and the
  active checks are the coverage.

## Worked example

A typical PRTG branch-office setup — 1 firewall, 2 switches, 3
Windows servers, 1 web app — commonly uses 40-60 sensors. The agent
equivalent:

```yaml
# probes.d/10-host.yaml  (on each Windows server)
- name: cpu
  type: cpu
- name: memory
  type: memory
- name: disk
  type: logicaldisk
- name: net
  type: network
- name: events
  type: windows_eventlog
  params:
    channels: [System, Application]
    levels: [Critical, Error, Warning]
```

```yaml
# probes.d/20-network.yaml  (on one collector host)
- name: fw
  type: snmp_poll
  params:
    target: 192.168.1.1
    community: "${env:SNMP_COMMUNITY}"
    mibs: [mib-2, if-mib]
- name: sw1
  type: snmp_poll
  params:
    target: 192.168.1.2
    community: "${env:SNMP_COMMUNITY}"
    mibs: [if-mib]
- name: sw2
  type: snmp_poll
  params:
    target: 192.168.1.3
    community: "${env:SNMP_COMMUNITY}"
    mibs: [if-mib]
- name: traps
  type: snmp_trap
  params:
    community: "${env:SNMP_COMMUNITY}"
- name: web
  type: http_check
  params:
    targets: ["https://app.example.com/healthz"]
- name: reachability
  type: icmp_check
  params:
    targets: [192.168.1.1, 192.168.1.2, 192.168.1.3]
```

Everything above is in the free tier.

## See also

- [Installation](installation.md)
- [Configuration](configuration.md)
- [Probe catalog](probes/index.md)
- [Prometheus / VictoriaMetrics output](prometheus/index.md)
- [OTLP output](otlp.md)
