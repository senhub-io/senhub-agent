# Topology

The agent can describe what it sees as entities and relations: the host,
its interfaces and addresses, what listens on it, how traffic leaves it,
and the network devices around it. An OpenTelemetry entity consumer such
as [Toise](https://github.com/toise-dev/toise) assembles these
descriptions, sent by many agents, into one graph.

!!! info "Off by default, OTLP only"
    - Topology is **off** unless you turn it on with the `entities`
      block described below. The container image and the Helm chart turn
      it on (`SENHUB_ENTITIES`, default `true`).
    - Only the [OTLP output](otlp.md) carries entities. PRTG, Nagios,
      Prometheus and Zabbix receive measurements only.
    - Entity events travel as OTLP log records, so the same OTLP output
      must be configured. Without it nothing leaves the agent.

## Turning it on

```yaml
# agent.yaml
entities:
  enabled: true
  interval: 5m
```

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Runs entity detection. Off means nothing is produced and nothing is polled |
| `interval` | `5m` | Heartbeat. Every entity is described again at this cadence, and the value travels with the event as the consumer's staleness hint |
| `depends_on.enabled` | `false` | Maps the outbound connections of the host. Needs root, see [OTLP](otlp.md#signalsentities) |
| `neighbors.enabled` | `false` | Reports ARP and NDP neighbours |
| `neighbors.resolves_to` | `false` | Links a neighbour to the interface that owns its MAC address |
| `aggregation.enabled` | `false` | Links the members of an Ethernet aggregate to it |

Each key is described in [Configuration](configuration.md#entities-section).
An OTLP output that declares `signals.entities.enabled` still works when
there is no `entities` block; a block that exists replaces it.

## What the agent reports

| Source | Entities | Relations |
|---|---|---|
| Host | `host`, with its machine facts, and `senhub.host.ip_forwarding.ipv4` / `.ipv6` | |
| Interfaces | `network.interface` | `has_interface` from the host |
| Addresses | `network.address` | `bound_to` the interface |
| Listeners | `service.listener` (address, port, process) | `listens_on` the interface that owns its address; `runs_on` the host for a wildcard or loopback bind |
| Routes | `network.route`: type, origin, egress interface, metric. At most 500 per host (`senhub.routes.omitted` counts the rest) | `has_route` from the host, `next_hop_via` the next hop address |
| Outbound dependencies (opt-in) | | `depends_on` towards the peer endpoint |
| System services | `host.service` from the `systemd` and `winservices` probes | `runs_on` the host |
| Neighbours (opt-in) | `network.neighbor`, identified by this host and the MAC address | `neighbor_of` the interface it was seen on; `resolves_to` the interface that owns the MAC (opt-in) |
| Ethernet aggregates (opt-in) | | `aggregated_into` from a member port to its aggregate |
| Kubernetes | `k8s.node`, see [Kubernetes](probes/kubernetes.md#nodes-on-the-map) | `same_as` the host of the node |

Probes add the entities they monitor (databases, services, containers,
devices). They share their identity keys with the metrics and logs of
the same probe, so a series can be joined to its entity.

### Why some sources are off

Each opt-in source puts a new entity type or relation on the wire. A
consumer rejects a type or relation it has not registered. Turn
`neighbors` and `aggregation` on only after the receiving graph knows
`network.neighbor`, `resolves_to` and `aggregated_into`. The dependency
map is off for a different reason: which peers a host talks to can be
sensitive.

## Network devices over SNMP

The [`snmp_poll`](probes/snmp-poll.md) probe puts a polled device on the
map with its interfaces, addresses, routes and LLDP links. A `discovery`
block extends this to the devices around it.

```yaml
- name: lan
  type: snmp_poll
  params:
    target: 192.168.1.10
    discovery:
      profile:
        community: "${secret:snmp.community}"
```

The crawl starts at the seeds, reads the LLDP neighbours of each device
and follows them. Discovered devices give topology only, no metrics.

| Key | Default | Meaning |
|---|---|---|
| `seeds` | none | Entry-point addresses |
| `seed_from_host` | `true` | Also start from the default gateways of the agent's host |
| `allowed_cidrs` | the private networks of the host | The crawl never leaves these ranges |
| `max_devices` | `200` | Hard cap on discovered devices |
| `max_hops` | `4` | Depth from the seeds |
| `interval` | `topology_interval` (`1m`) | Crawl cadence |
| `profile` | required | SNMP version (v2c only) and community of the crawled devices |

The agent reads LLDP. It does not read CDP. Only neighbours that
advertise a named port are linked, and a link is not reported while
either of its ports reads down. See [SNMP Poll](probes/snmp-poll.md#discovery)
for the rest, including governance rules by match.

The crawl reports itself with the `senhub.snmp.discovery.*` metrics.

## When an entity disappears

An entity leaves the graph with a delete event. The event carries
`entity.delete.reason`, the producer's motive. Read it before concluding
anything about the resource.

| Reason | Meaning | What it says about the resource |
|---|---|---|
| `terminated` | The source read successfully and no longer lists it | It ended |
| `parent_removed` | It lost the relation that anchored it | Follows its parent |
| `unmonitored` | The agent stopped observing: the probe was removed or stopped, or the source failed for too long | Nothing. It may still be running |
| `rekeyed` | The resource is the same, the agent now identifies it differently, as when `systemd` units moved from `service.instance` to `host.service` | Nothing happened |
| `aged_out` | A cache the source reads forgot the entry, such as an ARP entry | Nothing was seen leaving |

The consumer adds its own account of who decided the deletion. The two
are independent.

An entity whose source fails for a short time is described again with
the time of its last successful read, not the current time, so a
consumer can tell how old the fact is.
