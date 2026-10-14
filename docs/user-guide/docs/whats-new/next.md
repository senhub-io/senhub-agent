# Next (unreleased)

Nothing released yet since 0.6.3. Changes land here as they are merged.

<div class="rn-filter"></div>

## Features

- **Ethernet aggregates on the map (opt-in).** LAG / LACP port-channels on switches and bonds or teams on Linux hosts and hypervisors: each member port is linked to its aggregate, with the aggregate's mode and, where it exists, each member's LACP or active-backup state, so a path no longer stops at an aggregate. `entities.aggregation.enabled`, off until the receiving Toise registers the relation.
- **Hosts say whether they route.** `senhub.host.ip_forwarding.ipv4` and `.ipv6` on the host entity, so a multi-homed server is not drawn as a gateway.

- **Full routing tables on the map.** Hosts and SNMP devices publish every route with its type (connected, remote, reject, blackhole), its origin (static, OSPF, BGP… in the IANA vocabulary), its egress interface and metric; each next hop of a multipath route is its own route, IPv6 included. Connected routes give the last hop of a path. A bound of 500 routes per host or device, with `senhub.routes.omitted` saying how many were left out. See [SNMP Poll → Routes](../probes/snmp-poll.md#routes).

- **SNMP discovery maps the network from the agent's host.** The `discovery` block of `snmp_poll` now runs: from its seeds it follows LLDP neighbours, within bounds, and puts every device found on the map with its interfaces, addresses, routes and links. Without `seeds` it starts from the host's default gateways, and without `allowed_cidrs` it stays inside the host's private networks, so a profile with the community is enough. `senhub.snmp.discovery.*` metrics report devices, runs, duration, failures and seeds.
