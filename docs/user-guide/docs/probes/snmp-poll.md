<img src="https://api.iconify.design/mdi/lan.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

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
# probes.d/10-snmp-poll.yaml — each file under probes.d/ is a YAML array of probes
- name: core-switch
  type: snmp_poll
  params:
    target: 192.168.1.10
    community: ${secret:core-switch.community}   # OS secret store; inline plaintext is auto-sealed on install
    mibs: [mib-2, if-mib]
    interval: 60
```

This polls system and interface tables every 60 seconds and emits
one series per interface (`if_index` tag).

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `target` | Yes | - | Device address or hostname |
| `port` | No | `161` | SNMP UDP port |
| `version` | No | `v2c` | SNMP version; v1 is refused. One of `v2c`, `v3`, `2c`, `3`, `2` |
| `community` | No | `public` | Community string (v2c). A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `v3` | No | - | USM credentials, required with version v3 |
| `v3.username` | Yes | - | USM user |
| `v3.auth_protocol` | No | - | Authentication protocol; empty for none. One of `MD5`, `SHA`, `SHA224`, `SHA256`, `SHA384`, `SHA512` |
| `v3.auth_passphrase` | No | - | Required with auth_protocol. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `v3.priv_protocol` | No | - | Privacy protocol; needs auth_protocol. One of `DES`, `AES`, `AES192`, `AES256` |
| `v3.priv_passphrase` | No | - | Required with priv_protocol. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `retries` | No | `2` | Retries per request |
| `timeout` | No | `5s` | Per-request timeout |
| `interval` | No | `60s` | Metric polling cadence |
| `topology_interval` | No | `10m` | Entity and topology sweep cadence |
| `mibs` | No | - | Built-in MIB modules to poll; this or custom_mappings is required. One of `mib-2`, `if-mib` |
| `mib_paths` | No | - | Local MIB files or folders used to name custom mappings |
| `custom_mappings` | No | - | OID to metric mappings |
| `custom_mappings[].oid` | Yes | - | OID, leading dot optional |
| `custom_mappings[].metric` | No | - | Metric name; resolved from mib_paths when omitted |
| `custom_mappings[].type` | No | `gauge` | A string. One of `gauge`, `counter` |
| `custom_mappings[].index_label` | No | - | Walk the OID as a table and tag rows with this label |
| `discovery` | No | - | Topology crawl from seed devices |
| `discovery.seeds` | Yes | - | Entry-point device addresses |
| `discovery.profile` | Yes | - | Credentials for crawled devices (v2c only) |
| `discovery.profile.version` | No | `v2c` | A string. One of `v2c`, `2c`, `2` |
| `discovery.profile.community` | Yes | - | A string. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `discovery.allowed_cidrs` | Yes | - | The crawl never leaves these ranges |
| `discovery.max_devices` | No | `200` | A int |
| `discovery.max_hops` | No | `4` | A int |
| `discovery.interval` | No | - | Crawl cadence; topology_interval by default |
| `discovery.governance_rules` | No | - | Per-device governance by match |
| `discovery.governance_rules[].match` | No | - | A block of settings |
| `discovery.governance_rules[].match.cidr` | No | - | A string |
| `discovery.governance_rules[].match.vendor` | No | - | A string |
| `discovery.governance_rules[].match.sysname` | No | - | Regular expression |
| `discovery.governance_rules[].governance` | No | - | A block of settings |
| `discovery.governance_rules[].governance.owner` | No | - | Who owns what this instance observes |
| `discovery.governance_rules[].governance.owner.team` | No | - | Owning team |
| `discovery.governance_rules[].governance.owner.contact` | No | - | Contact for the team |
| `discovery.governance_rules[].governance.criticality` | No | - | Business criticality. One of `critical`, `high`, `medium`, `low` |
| `discovery.governance_rules[].governance.location` | No | - | Where it is |
| `discovery.governance_rules[].governance.location.site` | No | - | A string |
| `discovery.governance_rules[].governance.location.datacenter` | No | - | A string |
| `discovery.governance_rules[].governance.location.rack` | No | - | A string |
| `discovery.governance_rules[].governance.location.room` | No | - | A string |
| `discovery.governance_rules[].governance.lifecycle` | No | - | active, maintenance, decommissioning or retired |
| `discovery.governance_rules[].governance.labels` | No | - | Free-form labels, emitted as entity.label.<key>; use application to name the application chain |
| `governance` | No | - | Ownership, criticality and location of the device |
| `governance.owner` | No | - | Who owns what this instance observes |
| `governance.owner.team` | No | - | Owning team |
| `governance.owner.contact` | No | - | Contact for the team |
| `governance.criticality` | No | - | Business criticality. One of `critical`, `high`, `medium`, `low` |
| `governance.location` | No | - | Where it is |
| `governance.location.site` | No | - | A string |
| `governance.location.datacenter` | No | - | A string |
| `governance.location.rack` | No | - | A string |
| `governance.location.room` | No | - | A string |
| `governance.lifecycle` | No | - | active, maintenance, decommissioning or retired |
| `governance.labels` | No | - | Free-form labels, emitted as entity.label.<key>; use application to name the application chain |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `target` | required | Device IP or hostname |
| `port` | `161` | SNMP UDP port |
| `version` | `2c` | `2c` or `3`. SNMPv1 is rejected (table walks need GETBULK) |
| `community` | `public` | Community string (v2c) — reference a stored secret via `${secret:<name>.community}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `v3` | none | USM credentials, required with `version: 3` (see below) |
| `timeout` | `5s` | Per-request timeout (duration string or seconds) |
| `retries` | `2` | Retries per request before the device counts as unanswered. UDP loses packets; a congested link needs more than one attempt, and each one costs `timeout` |
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
| `governance_rules` | none | Per-device governance, matched as the crawl finds devices (see below) |

The `governance` parameter of this probe and the `governance` block every
probe entry accepts (see [Governance per probe](../configuration.md#governance-per-probe))
share one vocabulary. Both stamp the polled device; on a key present in both,
the probe-level parameter wins, and a matched discovery rule wins over either.

#### Governance for discovered devices

A crawl finds devices you never listed, so their ownership, criticality and
location cannot be written per device. `governance_rules` states them by
match instead: the first rule whose conditions all hold stamps its
`governance` block on the device's entity.

```yaml
discovery:
  seeds: ["10.0.0.1"]
  allowed_cidrs: ["10.0.0.0/16"]
  governance_rules:
    - match:
        cidr: "10.0.10.0/24"
      governance:
        criticality: critical
        owner:
          team: network
        location:
          site: paris
    - match:
        vendor: "cisco"
        sysname: "^edge-"      # regular expression
      governance:
        criticality: high
```

`match` accepts `cidr`, `vendor` and `sysname` — all optional, all must hold
for the rule to apply, and a rule with no `match` applies to every device.
The `governance` block takes the same keys as the per-agent one: `criticality`,
`lifecycle`, `owner`, `location` and free-form `labels`.

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
