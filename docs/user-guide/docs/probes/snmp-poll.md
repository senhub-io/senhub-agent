!!! info
    **License: Free** — part of the universal collection tier. SNMP
    polling is the backbone of network monitoring and the agent ships
    it without a license key.

# SNMP Poll Probe

The `snmp_poll` probe polls a network device over SNMPv2c and turns
standard MIB objects into typed metrics: system uptime, per-interface
traffic, errors, discards, speed and status. Custom OID mappings
cover the vendor-specific long tail. An optional discovery mode
crawls the network topology from seed devices (LLDP) and reports
devices and links as entities.

One probe instance polls one device; declare one instance per device
(or use discovery to enumerate them).

## Quick start

```yaml
probes:
  - name: core-switch
    type: snmp_poll
    params:
      target: 192.168.1.10
      community: "${env:SNMP_COMMUNITY}"
      mibs: [mib-2, if-mib]
      interval: 60
```

This polls system and interface tables every 60 seconds and emits
one series per interface (`if_index` tag).

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `target` | required | Device IP or hostname |
| `port` | `161` | SNMP UDP port |
| `version` | `2c` | SNMP version. Only `2c` is supported; v1 and v3 are rejected with a clear error |
| `community` | `public` | Community string. Use `${env:...}` or `${file:...}` substitution rather than a literal |
| `timeout` | `5s` | Per-request timeout (duration string or seconds) |
| `interval` | `60s` | Metric polling cadence |
| `topology_interval` | `10m` | Entity/topology sweep cadence (slower rail, independent of metrics) |
| `mibs` | `[]` | Built-in MIB modules to poll: `mib-2`, `if-mib` |
| `custom_mappings` | `[]` | Operator-supplied OID-to-metric mappings (see below) |
| `discovery` | none | Topology crawl from seed devices (see below) |

At least one entry under `mibs` or `custom_mappings` is required.
Configuration errors are accumulated and reported together at
startup, not one at a time.

### Custom mappings

Map any OID — scalar or table column — to a named metric:

```yaml
params:
  target: 192.168.1.20
  custom_mappings:
    - oid: .1.3.6.1.4.1.318.1.1.1.2.2.1     # APC UPS battery capacity
      metric: snmp.ups.battery_capacity
      type: gauge
    - oid: .1.3.6.1.2.1.2.2.1.10            # ifInOctets (table walk)
      metric: snmp.interface.in_octets
      type: counter
      index_label: if_index
```

| Field | Default | Description |
|---|---|---|
| `oid` | required | OID, leading dot optional |
| `metric` | required | Metric name to emit |
| `type` | `gauge` | `gauge` or `counter` |
| `index_label` | none | When set, the OID is walked as a table and the row index becomes this tag |

### Discovery

When a `discovery` block is present the probe crawls outward from
seed devices using LLDP neighbor tables, bounded by CIDR ranges and
depth/device caps, and reports discovered devices and links on the
entity rail:

```yaml
params:
  target: 192.168.1.10
  mibs: [mib-2, if-mib]
  discovery:
    seeds: [192.168.1.10, 192.168.1.11]
    profile:
      version: 2c
      community: "${env:SNMP_COMMUNITY}"
    allowed_cidrs: [192.168.0.0/16]
    max_devices: 200
    max_hops: 4
```

| Field | Default | Description |
|---|---|---|
| `seeds` | required | Entry-point device IPs |
| `profile` | required | Credentials used for crawled devices (`version`, `community`) |
| `allowed_cidrs` | required | The crawl never leaves these ranges |
| `max_devices` | `200` | Hard cap on discovered devices |
| `max_hops` | `4` | BFS depth bound from the seeds |
| `interval` | `topology_interval` | Crawl cadence |

## Metrics

One series per device (`instance` tag); interface metrics add
`if_index`.

| Metric | Type | Description |
|---|---|---|
| `senhub.snmp.up` | gauge | 1 when the device answered this cycle, 0 when not |
| `senhub.snmp.poll.duration` | gauge | Wall-clock poll time |
| `snmp.sys.uptime` | gauge | Device uptime |
| `snmp.interface.in_octets` / `out_octets` | counter | Interface traffic |
| `snmp.interface.in_errors` / `out_errors` | counter | Interface errors |
| `snmp.interface.in_discards` / `out_discards` | counter | Interface discards |
| `snmp.interface.speed` | gauge | Negotiated interface speed |
| `snmp.interface.admin_status` / `oper_status` | gauge | Interface status (up=1) |

An unreachable device is a measurement (`senhub.snmp.up = 0`), never
a probe failure — the agent keeps polling.

## Operational notes

- **SNMPv2c only.** v1 and v3 are rejected at startup. v3 polling is
  on the roadmap; the [SNMP trap receiver](snmp-trap.md) already
  supports v3.
- **No network MIB fetching.** The probe never downloads MIB files
  at runtime. Built-in modules cover MIB-2 and IF-MIB; everything
  else goes through `custom_mappings`.
- **Two rails, two cadences.** Metrics poll at `interval`; topology
  and entity sweeps run at the slower `topology_interval` so a dense
  crawl never delays traffic counters.
- **Counters are raw.** `in_octets` and friends are emitted as
  counters; compute rates in the backend
  (`rate(snmp_interface_in_octets[5m])` in VictoriaMetrics).
