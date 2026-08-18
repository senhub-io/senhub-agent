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
- Read access to the Docker socket (`/var/run/docker.sock`).

Pointed at a worker or at an engine that is not in swarm mode, the probe still
emits `senhub.swarm.up 0` plus a state series naming the reason — `worker`,
`not_in_swarm` or `unreachable`. The distinction matters: one is a probe on the
wrong node, another is a machine that was never clustered.

## Configuration

```yaml
probes:
  - name: swarm
    type: swarm
    params:
      socket_path: /var/run/docker.sock   # default
      interval: 60                        # seconds, default 60
      timeout: 10                         # seconds, default 10
```

| Key | Default | Meaning |
|---|---|---|
| `socket_path` | `/var/run/docker.sock` | Docker Engine socket |
| `interval` | `60` | Collection cadence in seconds |
| `timeout` | `10` | Per-request timeout in seconds |

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
