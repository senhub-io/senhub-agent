<img src="../../assets/probe-logos/snmp-poll.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier. SNMP
    polling is the backbone of network monitoring and the agent ships
    it without a license key.

# SNMP Poll Probe

The `snmp_poll` probe polls a network device over SNMP v2c or v3 and
turns standard MIB objects into typed metrics: system uptime,
per-interface traffic, errors, discards, speed and status. Custom OID
mappings cover the vendor-specific long tail.

The probe also reads the LLDP neighbor table of each polled device and
reports its links. A link is reported when the neighbor advertises a
named port (LLDP port ID subtype interface name or locally assigned);
a port known only by its MAC address is not linked. A link is not reported
while either of its ports was last read down: a device keeps a neighbor in
its LLDP table for a while after the link went down, and a virtual link can
stay up on one side after the other was shut.

When the far end of a link has not been read recently, the local interface
carries `senhub.link.peer_state`: `unread` (the far device is not polled),
`stale` (its last reading is older than three of its sweeps) or
`unreachable` (it stopped answering). The attribute is absent when both ends
were read. A device that stops answering is no longer reported after three
topology intervals; it leaves the map as unmonitored, not as gone.

Topology is off until the `entities` block is enabled, and only the
[OTLP output](../otlp.md) carries it, see [Topology](../topology.md).

One probe instance polls one device for its metrics. With a `discovery`
block it also crawls the network outward from seed devices (by default the
host's gateways) and puts every device it finds on the map, with its interfaces and links (see
[Discovery](#discovery)).

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
<!-- sha256:9664d8eb2593e60f0aeb5513bbf7bd05c48c6e42ddebfed01ab4ae5db95cecf7 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `target` | Yes | - | Device address or hostname |
| `port` | No | `161` | SNMP UDP port |
| `version` | In practice | `v2c` | SNMP version; v1 is refused. One of `v2c`, `v3`, `2c`, `3`, `2` |
| `community` | If `version` is `v2c`, `2c`, `2` | `public` | Community string (v2c). A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `v3` | If `version` is `v3`, `3` | - | USM credentials, required with version v3 |
| `v3.username` | Yes | - | USM user |
| `v3.auth_protocol` | No | - | Authentication protocol; empty for none. One of `MD5`, `SHA`, `SHA224`, `SHA256`, `SHA384`, `SHA512` |
| `v3.auth_passphrase` | No | - | Required with auth_protocol. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `v3.priv_protocol` | No | - | Privacy protocol; needs auth_protocol. One of `DES`, `AES`, `AES192`, `AES256` |
| `v3.priv_passphrase` | No | - | Required with priv_protocol. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `retries` | No | `2` | Retries per request |
| `max_routes` | No | `500` | Maximum number of route entities emitted per device |
| `neighbors` | No | `false` | Publish every MAC a device learned (bridge forwarding and ARP tables) as a network.neighbor entity, linked to its port and, when known, to the interface that owns it |
| `max_neighbors` | No | `1000` | Maximum number of network.neighbor entities emitted per device when neighbors is on |
| `timeout` | No | `5s` | Per-request timeout |
| `interval` | No | `60s` | Metric polling cadence |
| `topology_interval` | No | `1m` | Entity and topology sweep cadence |
| `mibs` | In practice | - | Built-in MIB modules to poll; this or custom_mappings is required. One of `mib-2`, `if-mib` |
| `interfaces` | No | - | Selects which interfaces get per-interface metric series (if-mib); topology entities always keep every interface |
| `interfaces.include` | No | - | Regular expressions; an interface is kept when its ifName or ifDescr matches one. Default: all |
| `interfaces.exclude` | No | - | Regular expressions; an interface whose ifName or ifDescr matches one is dropped |
| `interfaces.types` | No | - | ifType allow-list, by name (ethernetCsmacd, ieee8023adLag, l2vlan...) or IANAifType number |
| `interfaces.admin_up_only` | No | `false` | Keep only interfaces whose ifAdminStatus is up; re-read at each topology sweep |
| `interfaces.oper_up_only` | No | `false` | Keep only interfaces whose ifOperStatus is up; re-read at each topology sweep |
| `interfaces.max_interfaces` | No | - | Cap on the interfaces kept, lowest ifIndex first; a warning is logged when it cuts |
| `mib_paths` | No | - | Local MIB files or folders used to name custom mappings |
| `custom_mappings` | No | - | OID to metric mappings |
| `custom_mappings[].oid` | Yes | - | OID, leading dot optional |
| `custom_mappings[].metric` | No | - | Metric name; resolved from mib_paths when omitted |
| `custom_mappings[].type` | No | `gauge` | How the value is reported: a gauge as read, a counter as a monotonic total. One of `gauge`, `counter` |
| `custom_mappings[].index_label` | No | - | Walk the OID as a table and tag rows with this label |
| `discovery` | No | - | Topology crawl from seed devices: every device found through LLDP, within allowed_cidrs, is put on the map (topology only, no metrics) |
| `discovery.seeds` | No | - | Entry-point device addresses; default: the host's default gateways |
| `discovery.seed_from_host` | No | `true` | Add the host's default gateways as crawl seeds |
| `discovery.profile` | Yes | - | Credentials for crawled devices (v2c only) |
| `discovery.profile.version` | No | `v2c` | SNMP version used to probe the discovered devices. One of `v2c`, `2c`, `2` |
| `discovery.profile.community` | Yes | - | Community string used to probe the discovered devices. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `discovery.allowed_cidrs` | No | - | The crawl never leaves these ranges; default: the host's own private networks |
| `discovery.max_devices` | No | `200` | Hard cap on the number of discovered devices |
| `discovery.max_hops` | No | `4` | Crawl depth from the seeds |
| `discovery.interval` | No | - | Crawl cadence; topology_interval by default |
| `discovery.governance_rules` | No | - | Per-device governance by match |
| `discovery.governance_rules[].match` | No | - | Conditions a device must all meet; an empty match applies to every device |
| `discovery.governance_rules[].match.cidr` | No | - | Address range the polled address must fall in |
| `discovery.governance_rules[].match.vendor` | No | - | Vendor name from sysObjectID, compared case-insensitively |
| `discovery.governance_rules[].match.sysname` | No | - | Regular expression the device sysName must match |
| `discovery.governance_rules[].governance` | No | - | A block of settings |
| `discovery.governance_rules[].governance.owner` | No | - | Who owns what this instance observes |
| `discovery.governance_rules[].governance.owner.team` | No | - | Owning team |
| `discovery.governance_rules[].governance.owner.contact` | No | - | Contact for the team |
| `discovery.governance_rules[].governance.criticality` | No | - | Business criticality. One of `critical`, `high`, `medium`, `low` |
| `discovery.governance_rules[].governance.location` | No | - | Where it is |
| `discovery.governance_rules[].governance.location.site` | No | - | Site name |
| `discovery.governance_rules[].governance.location.datacenter` | No | - | Datacenter name |
| `discovery.governance_rules[].governance.location.rack` | No | - | Rack identifier |
| `discovery.governance_rules[].governance.location.room` | No | - | Room identifier |
| `discovery.governance_rules[].governance.lifecycle` | No | - | active, maintenance, decommissioning or retired |
| `discovery.governance_rules[].governance.labels` | No | - | Free-form labels, emitted as entity.label.<key>; use application to name the application chain |
| `governance` | No | - | Ownership, criticality and location of the device |
| `governance.owner` | No | - | Who owns what this instance observes |
| `governance.owner.team` | No | - | Owning team |
| `governance.owner.contact` | No | - | Contact for the team |
| `governance.criticality` | No | - | Business criticality. One of `critical`, `high`, `medium`, `low` |
| `governance.location` | No | - | Where it is |
| `governance.location.site` | No | - | Site name |
| `governance.location.datacenter` | No | - | Datacenter name |
| `governance.location.rack` | No | - | Rack identifier |
| `governance.location.room` | No | - | Room identifier |
| `governance.lifecycle` | No | - | active, maintenance, decommissioning or retired |
| `governance.labels` | No | - | Free-form labels, emitted as entity.label.<key>; use application to name the application chain |

<!-- schema:params:end -->

At least one entry under `mibs` or `custom_mappings` is required.
Configuration errors are accumulated and reported together at
startup, not one at a time.

`retries` matters on a congested link: UDP loses packets, and each
attempt costs one `timeout` before the device counts as unanswered.
`topology_interval` is a slower rail, independent of `interval`: the
topology is swept at most once per `topology_interval`, inside a poll
cycle. A sweep stops its walks once it has run for `interval` (30
seconds at least) and still reads the device identity. It stops at its
first read that times out when the device has answered nothing yet, so
an unreachable device costs one `timeout` and its retries per attempt;
a poll cycle stops its walks the same way.
After a failed sweep the next attempt comes at the next cycle, then twice
as late each time, up to five minutes, until the device answers.
`mib_paths` is read locally; the probe never fetches a MIB over the
network.

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

The security level (noAuthNoPriv / authNoPriv / authPriv) is derived
from which protocols are set — there is no separate field to
contradict it. Unknown protocol names are startup errors, never a
silent downgrade. The discovery crawl profile remains v2c-only.

### Interface selection

With `mibs: [if-mib]` the probe emits nine series per row of the interface
table. A chassis switch with VLAN interfaces and unused ports can expose
hundreds of rows. The `interfaces` block keeps the series of the interfaces
you select and drops the others:

```yaml
params:
  target: 192.0.2.10
  mibs: [if-mib]
  interfaces:
    include: ['^(Gi|Te)', '^Po']
    exclude: ['^Vlan']
    types: [ethernetCsmacd, ieee8023adLag]
    admin_up_only: true
    max_interfaces: 48
```

An interface is kept when it passes every criterion that is set: it matches
one `include` expression (or none is set), matches no `exclude` expression,
has one of the listed `types`, and is up where `admin_up_only` or
`oper_up_only` ask for it. Expressions are matched against `ifName` and
`ifDescr`. `types` takes an IANAifType name (`ethernetCsmacd`,
`ieee8023adLag`, `l2vlan`, `propVirtual`, `softwareLoopback`, `tunnel`...) or
its number. `max_interfaces` keeps the interfaces with the lowest `ifIndex`
and logs a warning with the number matched and kept when it cuts.

- The selection is read again at each topology sweep (`topology_interval`),
  not at each metric poll. An interface that comes up or goes down enters or
  leaves the series at that cadence.
- `oper_up_only` stops the series of an interface that goes down, so its
  `oper_status` can no longer report the fault: use `admin_up_only` for the
  ports you want to alert on.
- A state the device does not answer for (`ifAdminStatus`, `ifDescr`) counts
  as not matching. Before the first sweep nothing is selected, so no
  interface series is emitted until the interfaces are known.
- A sweep that could not read every interface column keeps the previous
  selection, so a timed-out walk does not retract the series.
- The selection narrows what is published, not what is read: the interface
  table is still walked at each `interval`. It also applies to a
  `custom_mappings` entry whose `index_label` is `if_index`.
- Topology is not narrowed: every interface stays an entity, with its links.

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

`metric` may be omitted only when `mib_paths` is set: the name is then
resolved from your MIB files at startup (for example
`upsAdvBatteryCapacity`), and an OID that cannot be resolved is a startup
error, never a silent gap. With `index_label`, the OID is walked as a
table and the row index becomes that tag.

### Discovery

A `discovery` block makes the probe crawl outward from seed devices
through their LLDP neighbor tables (CDP is not read), bounded by CIDR ranges and depth and
device caps, and report every device found on the entity rail: the
device, its interfaces and addresses, its routes and its LLDP links.
Discovered devices give topology only, no metrics: metrics stay with
the polled `target`.

The credentials are the only thing the block needs. Without `seeds`, the
crawl starts from the default gateways of the host the agent runs on;
without `allowed_cidrs`, it stays inside the host's own private networks
(a network wider than /16 is narrowed to the /16 around the host's
address). Both are read again at every run, so a gateway change is
followed:

```yaml
params:
  target: 192.168.1.10
  discovery:
    profile:
      community: "${secret:snmp.community}"
```

The gateway's address is reported as the same `network.address` node the
host's default route points to, so the host and its network meet on the
map without further configuration.

Give `seeds` and `allowed_cidrs` to crawl elsewhere or wider. With
`seed_from_host: false` only the listed seeds are used:

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

`max_devices` is a hard cap on discovered devices and `max_hops` bounds
the crawl depth from the seeds. Only neighbors inside `allowed_cidrs` are
followed (configured seeds are always polled; a host gateway outside the
ranges is not), at most four devices are polled at a
time, and the crawl runs every `interval` of the block (the topology
interval by default), never overlapping itself. A device that stops
answering stays on the map for two more runs, so one lost UDP exchange
does not retract it with all its links. The crawl profile is v2c only. `governance_rules` stamps governance on
devices as the crawl finds them (see below).

The `governance` parameter of this probe and the `governance` block every
probe entry accepts (see [Governance per probe](../configuration.md#governance-per-probe))
share one vocabulary. Both stamp the polled device; on a key present in both,
the probe-level parameter wins, and a matched discovery rule wins over either.

#### Routes

The routing table of the polled device and of every discovered device is
published as `network.route` entities, read from `ipCidrRouteTable` and
`inetCidrRouteTable` (IPv4 and IPv6). A route is identified by its device,
its destination and its next hop, so two next hops to one destination (ECMP,
or a backup route) are two routes. Each route carries:

| Attribute | Meaning |
|---|---|
| `route.type` | `local` (directly connected), `remote` (through a next hop), `reject`, `blackhole` or `other` |
| `route.protocol` | Where the route comes from, in the IANA vocabulary: `netmgmt` (static), `ospf`, `bgp`, `local`, `rip`, `isis`, `cisco_eigrp`… Absent when the code has no name |
| `network.route.protocol.code` | The raw protocol code the device gave |
| `network.interface.name` | The egress interface; absent on `reject` and `blackhole` routes, which end by design |
| `metric` | The route metric, when the device gives one |

A route without a next hop (`local`, `reject`, `blackhole`) carries `0.0.0.0`
(`::` in IPv6) as next hop, the encoding the MIB itself uses. A connected
route is what lets a path reach a destination on the device's own networks.

At most `max_routes` routes are published per device (500 by default).
Connected, default, reject and blackhole routes are always kept. The device
entity carries `senhub.routes.limit` and `senhub.routes.omitted`, the number
of routes it listed and that were not published, so a table cut by the bound
is never mistaken for a complete one. A device carrying a full Internet table
is not meant to be inventoried route by route.

#### Learned neighbours

With `neighbors: true`, the bridge forwarding table (`dot1qTpFdbTable`, or
`dot1dTpFdbTable` on older bridges) and the ARP table
(`ipNetToPhysicalTable`, or `ipNetToMediaTable`) of the polled device and of
every discovered device are published as `network.neighbor` entities, one
per MAC address the device learned. It is off by default: a core switch can
learn thousands of addresses. A neighbour is identified by the device that
saw it and the MAC address (`AA-BB-CC-DD-EE-FF`): two switches that learned
the same machine hold two neighbours, joined later by the receiving graph
rather than merged.

| Attribute | Meaning |
|---|---|
| `network.neighbor.addresses` | The IP addresses the ARP table maps to this MAC; absent for a MAC seen only in the forwarding table. Link-local addresses are left out |

A neighbour is linked with `neighbor_of` to every port of the device it was
learned on: the bridge port for a forwarding entry, the interface of the ARP
entry (often a VLAN interface). It is also linked with `resolves_to` to the
interface that owns the MAC when this agent knows it, one of this host's own
interfaces or an interface of another device this agent polls, without
setting `entities.neighbors.resolves_to`. The relation carries a `basis` of
`fdb` or `arp`. A neighbour found in the forwarding table rides the
`senhub-agent/snmp-fdb` instrumentation scope, one known only from the ARP
table `senhub-agent/snmp-arp`.

A MAC owned by the observing device itself, multicast and broadcast
addresses, entries the device marks as its own and entries it has not learned
are not published. A neighbour leaves, as `aged_out`, when a successful read
of the tables no longer lists it; a read that fails keeps the neighbours of
the previous one.

At most `max_neighbors` neighbours are published per device (1000 by
default). Above the bound, the neighbours resolved to an interface are kept
first, then those with an IP address, then the others by MAC address. The
device entity carries `senhub.neighbors.limit` and
`senhub.neighbors.omitted`, so a cut table is never mistaken for a complete
one, and the agent logs `neighbor table bound: some neighbors omitted` at
Info once per cut. A neighbour the bound leaves out after it was published
is not retired explicitly: it leaves the topology when its liveness interval
expires. VLAN membership (`dot1qVlanStaticTable`) is not published yet.

Publishing neighbours costs CPU in proportion to their number. On a test
switch with 4,000 forwarding-table and 4,000 ARP entries, the sweep time did
not change (about 2 s), and the agent used 1.29 s of CPU over 270 s instead
of 0.65 s without `neighbors`.

At the bound, the neighbours kept depend on facts that change: whether a
MAC resolves to an interface of another polled device, or of this host. An
outage of another polled device longer than three sweeps can therefore swap
out as many kept neighbours as it had resolved interfaces, and swap them back
at its first successful sweep after it returns: measured on a test switch,
one neighbour, two deletions and two creations per outage.

Without `neighbors`, the tables are read only with
`entities.neighbors.resolves_to`, and a neighbour is published only for a MAC
of this host's own interfaces, linked to the switch port it is plugged into.

#### Link aggregation

With `entities.aggregation.enabled`, the probe reads `ifStackTable` (IF-MIB)
and the IEEE 802.3ad LAG MIB on the polled and discovered devices: each member
port is linked to its port-channel (`aggregated_into`), the port-channel
carries its mode (`lacp`, or `static` when the LAG MIB says nothing about it),
and a LACP member carries `active` (collecting and distributing) or `standby`.
A port-channel borrows a member's MAC address, so for MAC-based resolution the
member owns it, never the port-channel.

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
  sweep never delays traffic counters.
- **Counters are raw.** `in_octets` and friends are emitted as
  counters; compute rates in the backend
  (`rate(snmp_interface_in_octets[5m])` in VictoriaMetrics).

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
| `senhub.snmp.up` | `senhub.snmp.up` | SNMP {instance} Reachability | # | 1 when the last poll reached the SNMP target, 0 otherwise |
| `senhub.probe.check.result` | `senhub.probe.check.result` | SNMP {instance} Check Result | # | Why the last poll failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |
| `senhub.snmp.poll.duration` | `senhub.snmp.poll.duration` | SNMP {instance} Poll Duration | s | Wall-clock time taken by the last poll cycle |
| `senhub.snmp.discovery.devices` | `snmp_poll_discovery_devices` | SNMP {instance} Discovered Devices | # | Devices that answered the last discovery crawl |
| `senhub.snmp.discovery.runs` | `snmp_poll_discovery_runs` | SNMP {instance} Discovery Runs | # | Discovery crawls run since the agent started |
| `senhub.snmp.discovery.duration` | `snmp_poll_discovery_last_duration_seconds` | SNMP {instance} Discovery Duration | s | Wall-clock time of the last discovery crawl |
| `senhub.snmp.discovery.failures` | `snmp_poll_discovery_failures` | SNMP {instance} Discovery Failures {reason} | # | Devices the crawl could not use since the agent started, by reason: unreachable (no SNMP answer) or identity (no device id resolvable) |
| `senhub.snmp.identity.collisions` | `snmp_poll_identity_collisions` | SNMP {instance} Identity Collisions | # | Sweeps since the agent started in which this target resolved to a device id another target of the same agent holds for a different device (other sysName or chassis MAC) |
| `senhub.snmp.engine_id.mac_mismatch` | `snmp_poll_engine_id_mac_mismatch` | SNMP {instance} Engine ID MAC Mismatch | # | 1 when the last topology sweep read an snmpEngineID that embeds a MAC other than the LLDP chassis MAC (engine id set by configuration or kept across a stack master change), 0 otherwise or when either is unreadable |
| `senhub.snmp.discovery.seeds` | `snmp_poll_discovery_seeds` | SNMP {instance} Discovery Seeds {source} | # | Seeds the last discovery crawl started from, by source: configured (discovery.seeds) or host (the host's default gateways) |
| `snmp.sys.uptime` | `snmp.sys.uptime` | SNMP {instance} Uptime | centiseconds | sysUpTime — time since the network management portion of the system was last re-initialized (hundredths of a second) |
| `snmp.interface.in_octets` | `snmp.interface.in_octets` | SNMP {instance} IF{if_index} In Octets | bytes | ifInOctets — total octets received on the interface |
| `snmp.interface.out_octets` | `snmp.interface.out_octets` | SNMP {instance} IF{if_index} Out Octets | bytes | ifOutOctets — total octets transmitted on the interface |
| `snmp.interface.in_errors` | `snmp.interface.in_errors` | SNMP {instance} IF{if_index} In Errors | # | ifInErrors — inbound packets containing errors |
| `snmp.interface.out_errors` | `snmp.interface.out_errors` | SNMP {instance} IF{if_index} Out Errors | # | ifOutErrors — outbound packets that could not be transmitted due to errors |
| `snmp.interface.in_discards` | `snmp.interface.in_discards` | SNMP {instance} IF{if_index} In Discards | # | ifInDiscards — inbound packets discarded though no error was detected |
| `snmp.interface.out_discards` | `snmp.interface.out_discards` | SNMP {instance} IF{if_index} Out Discards | # | ifOutDiscards — outbound packets discarded though no error was detected |
| `snmp.interface.speed` | `snmp.interface.speed` | SNMP {instance} IF{if_index} Speed | bits/s | ifSpeed — nominal bandwidth of the interface in bits per second |
| `snmp.interface.admin_status` | `snmp.interface.admin_status` | SNMP {instance} IF{if_index} Admin Status | # | ifAdminStatus — desired interface state (1=up, 2=down, 3=testing) |
| `snmp.interface.oper_status` | `snmp.interface.oper_status` | SNMP {instance} IF{if_index} Oper Status | # | ifOperStatus — current operational interface state (1=up, 2=down, 3=testing, ...) |

<!-- schema:metrics:end -->
