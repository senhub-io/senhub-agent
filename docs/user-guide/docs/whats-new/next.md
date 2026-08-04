# Next (unreleased)

<div class="rn-filter"></div>


## New

### `snmp_poll` collects IPv6 routes

Route collection walked only `ipCidrRouteTable` (RFC 2096), which is IPv4-only,
so an IPv6 or dual-stack router surfaced none of its IPv6 routing table.

`snmp_poll` now also walks `inetCidrRouteTable` (RFC 4292), the
address-family-agnostic successor, and emits IPv6 destinations as
`network.route` entities exactly like IPv4 ones — canonical CIDR identity, host
bits zeroed, RFC 5952 form (`2001:db8:abcd::/48`, `::/0` for the default
route).

Devices implementing both tables list their IPv4 routes twice; the first entity
per destination wins and the IPv4 table is walked first, so IPv4 route
identities are unchanged. A device implementing only one of the two tables is
normal and no longer treated as a failure — collection fails only when neither
table answers, and the error then names both causes.

Zoned address families (`ipv4z`, `ipv6z`) are skipped: their destination is
only meaningful inside one scope and would collide with its unzoned twin.
(#716)
