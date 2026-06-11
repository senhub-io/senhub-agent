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
| `version` | `2c` | `2c` or `3`. SNMPv1 is rejected (table walks need GETBULK) |
| `community` | `public` | Community string (v2c). Use `${env:...}` or `${file:...}` substitution rather than a literal |
| `v3` | none | USM credentials, required with `version: 3` (see below) |
| `timeout` | `5s` | Per-request timeout (duration string or seconds) |
| `interval` | `60s` | Metric polling cadence |
| `topology_interval` | `10m` | Entity/topology sweep cadence (slower rail, independent of metrics) |
| `mibs` | `[]` | Built-in MIB modules to poll: `mib-2`, `if-mib` |
| `mib_paths` | `[]` | Local directories or files of MIB modules used to name custom mappings (never fetched over the network) |
| `custom_mappings` | `[]` | Operator-supplied OID-to-metric mappings (see below) |
| `discovery` | none | Topology crawl from seed devices (see below) |

At least one entry under `mibs` or `custom_mappings` is required.
Configuration errors are accumulated and reported together at
startup, not one at a time.

### SNMPv3 (USM)

```yaml
params:
  target: 192.168.1.10
  version: "3"
  mibs: [mib-2, if-mib]
  v3:
    username: monitoring
    auth_protocol: SHA256
    auth_passphrase: "${file:/etc/senhub-agent/snmp_auth}"
    priv_protocol: AES256
    priv_passphrase: "${file:/etc/senhub-agent/snmp_priv}"
```

| Field | Description |
|---|---|
| `username` | required |
| `auth_protocol` | `MD5`, `SHA`, `SHA224`, `SHA256`, `SHA384`, `SHA512`, or omitted for no authentication |
| `auth_passphrase` | Required with `auth_protocol` |
| `priv_protocol` | `DES`, `AES`, `AES192`, `AES256`; requires an `auth_protocol` |
| `priv_passphrase` | Required with `priv_protocol` |

The security level (noAuthNoPriv / authNoPriv / authPriv) is derived
from which protocols are set — there is no separate field to
contradict it. Unknown protocol names are startup errors, never a
silent downgrade. The discovery crawl profile remains v2c-only.

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
| `metric` | required unless `mib_paths` is set | Metric name to emit. When omitted and `mib_paths` is configured, the name is resolved from your MIB files at startup (e.g. `upsAdvBatteryCapacity`); an unresolvable OID is a startup error, never a silent gap |
| `type` | `gauge` | `gauge` or `counter` |
| `index_label` | none | When set, the OID is walked as a table and the row index becomes this tag |

### Discovery

!!! warning "Not active yet"
    The `discovery` block is parsed and validated but **not wired to
    the poll lifecycle yet** — configuring it does nothing today
    beyond a startup warning. It is documented here because the
    configuration shape is final. Per-device topology (LLDP
    neighbors, routes, bridge tables of the polled `target`) is
    active and independent of this block. Tracking:
    [#156](https://github.com/senhub-io/senhub-agent/issues/156).

When the crawl ships, a `discovery` block will make the probe crawl
outward from seed devices using LLDP neighbor tables, bounded by
CIDR ranges and depth/device caps, and report discovered devices and
links on the entity rail:

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

- **v2c and v3.** SNMPv1 is rejected at startup (no GETBULK). The
  [SNMP trap receiver](snmp-trap.md) accepts v2c and v3 as well.
- **No network MIB fetching.** The probe never downloads MIB files
  at runtime. Built-in modules cover MIB-2 and IF-MIB; everything
  else goes through `custom_mappings`.
- **Two rails, two cadences.** Metrics poll at `interval`; topology
  and entity sweeps run at the slower `topology_interval` so a dense
  crawl never delays traffic counters.
- **Counters are raw.** `in_octets` and friends are emitted as
  counters; compute rates in the backend
  (`rate(snmp_interface_in_octets[5m])` in VictoriaMetrics).
