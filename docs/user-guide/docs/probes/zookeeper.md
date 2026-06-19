!!! info
    **License: Free** — part of the universal collection tier.

# Apache ZooKeeper

The `zookeeper` probe monitors an Apache ZooKeeper node via the `mntr`
four-letter command over raw TCP, reporting request latency, packet counts,
connection counts, znode and watch counts, file descriptor usage and ensemble
state (leader/follower/observer).

## Quick start

```yaml
probes:
  - name: zookeeper
    type: zookeeper
    params:
      host: localhost
      port: 2181
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `host` | `localhost` | ZooKeeper node hostname or IP |
| `port` | `2181` | ZooKeeper client port |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.zookeeper.up` | 1 | 1 when the node answered the `mntr` command |
| `zookeeper.latency.avg` | ms | Average request processing latency (`zk_avg_latency`) |
| `zookeeper.latency.max` | ms | Maximum request processing latency |
| `zookeeper.connections.count` | {connection} | Current client connections |
| `zookeeper.packets.received` | {packet} | Packets received since start |
| `zookeeper.packets.sent` | {packet} | Packets sent since start |
| `zookeeper.znodes.count` | {znode} | Number of znodes in the data tree |
| `zookeeper.watches.count` | {watch} | Number of active watches |
| `zookeeper.file_descriptors.open` | {fd} | Open file descriptors |
| `zookeeper.leader_elections` | {election} | Leader elections triggered (leader nodes only) |
| `zookeeper.pending_syncs` | {sync} | Pending sync operations (leader only) |

## Operational notes

- The `mntr` four-letter command must be enabled in `zoo.cfg`: `4lw.commands.whitelist=mntr` (required since ZooKeeper 3.5).
- Leader-only metrics (`leader_elections`, `pending_syncs`) are only emitted by the ensemble leader; follower nodes emit zero or omit them.
- For multi-node ensembles, configure one probe instance per node to monitor the ensemble comprehensively.
