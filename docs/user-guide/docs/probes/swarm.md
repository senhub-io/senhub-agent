# Docker Swarm

**Tier: Free** · Probe type: `swarm`

Cluster-wide Docker Swarm state read from a manager node: nodes and quorum,
service convergence, task failures, and the overlay segments that decide what
can reach what.

This is a different probe from [`docker`](docker.md), and the line is the
observation scope. `docker` watches the containers of one host. Everything here
is cluster-wide and answerable only by a manager. Run both on a manager node if
you want per-container resource metrics as well.

## Requirements

- The agent must run on a **manager** node. On a worker the Engine answers 503
  to every cluster query, and the probe reports that explicitly rather than
  showing an empty, healthy-looking cluster.
- Read access to the Docker Engine: the socket `/var/run/docker.sock` on Linux and macOS, the named pipe `npipe://./pipe/docker_engine` on Windows.

Pointed at a worker or at an engine that is not in swarm mode, the probe still
emits `senhub.swarm.up 0` plus a state series naming the reason — `worker`,
`not_in_swarm` or `unreachable`. The distinction matters: one is a probe on the
wrong node, another is a machine that was never clustered.

## Configuration

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `socket_path` | No | - | Engine socket; /var/run/docker.sock on Unix, npipe://./pipe/docker_engine on Windows by default |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Engine request timeout in seconds |

<!-- schema:params:end -->

```yaml
probes:
  - name: swarm
    type: swarm
    params:
      socket_path: /var/run/docker.sock   # default
      interval: 60                        # seconds, default 60
      timeout: 10                         # seconds, default 10
```

On Windows the pipe may also be written `\\.\pipe\docker_engine`.

## What it reports

### Nodes and quorum

A Swarm that has lost manager quorum is the failure worth catching, and it is
invisible from any single host: every machine keeps running its containers
while the cluster silently refuses every change — no deploy, no rescheduling,
no scaling.

`swarm.cluster.quorum` answers it directly rather than leaving you to derive it
from two counters. The rule is a **strict** majority: two managers with one
reachable is *not* quorum, which is exactly the shape of a two-manager cluster
that has just lost one.

Per node: `swarm.node.ready`, the one-hot `swarm.node.state`
(ready/down/unknown/disconnected) and `swarm.node.availability`
(active/pause/drain). Both families are needed — availability is the operator's
intent, state is the reality, and a node drained on purpose looks identical to
a node that fell over if you only have one of them.

### Services and tasks

`swarm.service.replicas.desired` vs `.running`, plus `swarm.service.converged`.
Running replicas are counted from the tasks, not read off the service: Swarm
publishes no running count, and a service can declare five replicas while five
tasks sit in `rejected`.

A **global** service declares no replica count at all — it wants one task per
eligible node. Its desired count comes from the tasks Swarm actually created,
so a global service does not read as permanently unconverged.

`swarm.task.state` counts tasks per service per lifecycle state, which is what
separates "not there yet" (`pending`, `preparing`, `starting`) from "will never
get there" (`failed`, `rejected`, `orphaned`). A service stuck at 2/3 reads the
same in both cases from the service object alone.

### Overlay networks

An overlay is a virtual L2 segment stretched across every node. It is the
**reachability boundary**: two services on the same overlay resolve each other
by name and can talk; two services on different overlays cannot, whatever the
firewall says. When a deploy "cannot reach the database", that is the first
question, and answering it today means reading several `docker network inspect`
outputs on the right node.

- `swarm.service.network.attached` — one series per (service, segment) pair,
  carrying the service's **virtual IP** on that segment: the address its peers
  actually resolve, and the one a reachability problem is about.
- `swarm.network.services` / `.tasks` — how many are on each segment.
- `swarm.network.ingress` — marks the routing-mesh segment. A port published
  there answers on **every** node, including ones running none of the service.
  That surprises people, so the publish mode rides as a label on
  `swarm.service.port.published` rather than being flattened away.
- `swarm.network.address.capacity` — assignable addresses in the subnet. An
  overlay that runs out refuses new tasks with a scheduling error naming
  neither the network nor the exhaustion; a /24 with 250 tasks on it is a
  deploy about to fail.

!!! note "Traffic volume between services is not reported"
    The probe maps **who can reach whom**, not how much flows between them.
    The Engine API exposes no per-peer counters, and per-container interface
    counters cannot be attributed to a named overlay — the stats endpoint keys
    them by interface name (`eth0`, `eth1`), which the API never maps back to a
    network. A real traffic matrix needs conntrack or eBPF on each node: a
    different instrument with different privileges. Deriving one from an
    attachment map would be inventing numbers.

## Topology

The probe emits the cluster itself as a `service.instance` entity keyed on the
Swarm cluster id, with a `monitors` edge from the agent.

It deliberately emits **no entity** for Swarm nodes or overlay networks:

- A Swarm node reports its hostname, never its `machine-id`, and the agent's
  host entity is keyed on `machine-id`. A host minted from a hostname would be
  a permanent duplicate of a machine that already has an entity. Run an agent
  inside the node and it emits the real host itself.
- There is no registered entity type for a network segment, so an overlay is
  carried as metric labels — queryable, but not traversable in the graph.

## Troubleshooting

**Everything reads zero.** Check `senhub.swarm.node_role_state`: if `worker` is
1, the agent is on a worker and no cluster query can succeed. Move it to a
manager.

**A service shows no network attachment.** Services declare networks in either
of two spec slots depending on the Engine version that created them, by id or
by name. The probe reads both slots and both spellings; if an attachment is
still missing, the service genuinely has none — which is also what correct
isolation looks like.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.swarm.up` | `swarm_cluster_up` | # | 1 when this node is a swarm manager and answered; 0 for worker, non-swarm or unreachable — the state series says which |
| `senhub.swarm.node_role_state` | `swarm_cluster_state_{state}` | # | one-hot over manager / worker / not_in_swarm / unreachable: why the probe sees what it sees |
| `swarm.cluster.nodes` | `swarm_cluster_nodes` | # | nodes known to the cluster |
| `swarm.cluster.managers` | `swarm_cluster_managers` | # | manager nodes |
| `swarm.cluster.managers.reachable` | `swarm_cluster_managers_reachable` | # | managers currently reachable by the Raft leader |
| `swarm.cluster.workers` | `swarm_cluster_workers` | # | worker nodes |
| `swarm.cluster.quorum` | `swarm_cluster_quorum` | # | 1 when a strict majority of managers is reachable; 0 means the cluster accepts no change at all — no deploy, no rescheduling |
| `swarm.cluster.tasks.orphaned` | `swarm_cluster_tasks_orphaned` | # | tasks whose service no longer exists; invisible from every per-service view |
| `swarm.cluster.networks` | `swarm_cluster_networks` | # | swarm-scoped overlay segments |
| `swarm.node.ready` | `swarm_node_{swarm.node.name}_ready` | # | 1 when the node status is ready |
| `swarm.node.state` | `swarm_node_{swarm.node.name}_state_{state}` | # | one-hot over ready / down / unknown / disconnected |
| `swarm.node.availability` | `swarm_node_{swarm.node.name}_availability_{availability}` | # | one-hot over active / pause / drain — the operator's intent, as opposed to the node's actual state |
| `swarm.node.cpu.allocatable` | `swarm_node_{swarm.node.name}_cpu` | # | CPU cores the node advertises to the scheduler |
| `swarm.node.memory.allocatable` | `swarm_node_{swarm.node.name}_memory` | # | memory the node advertises to the scheduler |
| `swarm.node.manager.leader` | `swarm_node_{swarm.node.name}_leader` | # | 1 on the Raft leader |
| `swarm.node.manager.reachable` | `swarm_node_{swarm.node.name}_manager_reachable` | # | 1 when this manager is reachable by the leader |
| `swarm.node.manager.reachability` | `swarm_node_{swarm.node.name}_reachability_{reachability}` | # | one-hot over reachable / unreachable / unknown |
| `swarm.node.tasks.running` | `swarm_node_{swarm.node.id}_tasks_running` | # | running tasks placed on this node |
| `swarm.service.replicas.desired` | `swarm_service_{swarm.service.name}_desired` | # | replicas asked for; for a global service, the tasks Swarm intends to run |
| `swarm.service.replicas.running` | `swarm_service_{swarm.service.name}_running` | # | replicas actually running, counted from tasks |
| `swarm.service.converged` | `swarm_service_{swarm.service.name}_converged` | # | 1 when running replicas have caught up with the declared count |
| `swarm.service.update.state` | `swarm_service_{swarm.service.name}_update_{state}` | # | one-hot over the rolling-update lifecycle; a service stuck in paused is a deploy waiting for a human |
| `swarm.service.tasks.failed` | `swarm_service_{swarm.service.name}_tasks_failed` | # | tasks in a terminal failure state (failed, rejected, orphaned) |
| `swarm.service.port.published` | `swarm_service_{swarm.service.name}_port_{swarm.port.published}` | # | one series per published port; in ingress mode the port answers on every node, not only where the service runs |
| `swarm.task.state` | `swarm_service_{swarm.service.name}_task_{state}` | # | tasks per service per lifecycle state; separates 'not there yet' from 'will never get there' |
| `swarm.service.network.attached` | `swarm_service_{swarm.service.name}_net_{swarm.network.name}` | # | 1 per (service, overlay) pair — the reachability map: two services share a segment or they cannot talk |
| `swarm.network.services` | `swarm_net_{swarm.network.name}_services` | # | services attached to this overlay |
| `swarm.network.tasks` | `swarm_net_{swarm.network.name}_tasks` | # | task attachments on this overlay |
| `swarm.network.ingress` | `swarm_net_{swarm.network.name}_ingress` | # | 1 on the routing-mesh segment that carries every published port |
| `swarm.network.attachable` | `swarm_net_{swarm.network.name}_attachable` | # | 1 when standalone containers may join this overlay |
| `swarm.network.internal` | `swarm_net_{swarm.network.name}_internal` | # | 1 when the overlay has no external route |
| `swarm.network.address.capacity` | `swarm_net_{swarm.network.name}_capacity` | # | assignable addresses in the overlay subnet; an overlay running out refuses new tasks with an error naming neither |

<!-- schema:metrics:end -->
