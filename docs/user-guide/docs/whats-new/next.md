# Next (unreleased)

Nothing released yet since 0.6.3. Changes land here as they are merged.

<div class="rn-filter"></div>

## Features

- **SNMP discovery maps the network from the agent's host.** The `discovery` block of `snmp_poll` now runs: from its seeds it follows LLDP neighbours, within bounds, and puts every device found on the map with its interfaces, addresses, routes and links. Without `seeds` it starts from the host's default gateways, and without `allowed_cidrs` it stays inside the host's private networks, so a profile with the community is enough. `senhub.snmp.discovery.*` metrics report devices, runs, duration, failures and seeds.
