# Next (unreleased)

Nothing released yet since 0.6.3. Changes land here as they are merged.

<div class="rn-filter"></div>

## Features

- **Series metering (Enterprise).** `senhub.agent.series.active`, `.created` and `.untracked` count, per output and probe, the series the agent exports over OTLP, the figure a time-series backend bills on. See [Series metering](../otlp.md#series-metering-enterprise).
- **Ethernet aggregates on the map (opt-in).** LAG / LACP port-channels on switches and bonds or teams on Linux hosts and hypervisors: each member port is linked to its aggregate, with the aggregate's mode and, where it exists, each member's LACP or active-backup state, so a path no longer stops at an aggregate. `entities.aggregation.enabled`, off until the receiving Toise registers the relation.
- **Hosts say whether they route.** `senhub.host.ip_forwarding.ipv4` and `.ipv6` on the host entity, so a multi-homed server is not drawn as a gateway.

- **Full routing tables on the map.** Hosts and SNMP devices publish every route with its type (connected, remote, reject, blackhole), its origin (static, OSPF, BGP… in the IANA vocabulary), its egress interface and metric; each next hop of a multipath route is its own route, IPv6 included. Connected routes give the last hop of a path. A bound of 500 routes per host or device, with `senhub.routes.omitted` saying how many were left out. See [SNMP Poll → Routes](../probes/snmp-poll.md#routes).

- **SNMP discovery maps the network from the agent's host.** The `discovery` block of `snmp_poll` now runs: from its seeds it follows LLDP neighbours, within bounds, and puts every device found on the map with its interfaces, addresses, routes and links. Without `seeds` it starts from the host's default gateways, and without `allowed_cidrs` it stays inside the host's private networks, so a profile with the community is enough. `senhub.snmp.discovery.*` metrics report devices, runs, duration, failures and seeds.

## Fixes

- **A cut link leaves the map, and a silent device leaves as unmonitored.** `snmp_poll` no longer reports a link while either of its ports reads down, even when the far switch still lists the neighbour. A device that stops answering is no longer replayed after three topology intervals and leaves with the reason "unmonitored", not as gone; a device answering nothing at all no longer appears under an address-only identity. The local interface of a link whose far end was not read recently carries `senhub.link.peer_state` (`unread`, `stale`, `unreachable`).
- **A neighbour the ARP cache forgot is not reported as ended.** When an ARP or NDP entry ages out, on the host or in a device's tables read over SNMP, its `network.neighbor` now leaves with the reason "aged_out" instead of "terminated".
- **Redis 8 key-size lines are no longer read as databases.** Channels such as `DB 0_distrib_strings_sizes Keys` disappear; only `dbN` lines of the keyspace make a database.
- **An empty Redis keeps its db0 channels.** Redis prints no keyspace line for a database without keys; the probe now reports `DB 0 Keys`, `DB 0 Expiring Keys` and `DB 0 Average TTL` at 0, so an alert on the key count keeps working when the instance is emptied. A Sentinel, which has no keyspace, gets none.
- **The Redis replication role reads "Sentinel or unknown" at -1.** The PRTG lookup said "Unknown" with a warning on every Sentinel; the Redis page now gives the three values and how to install the lookups.
