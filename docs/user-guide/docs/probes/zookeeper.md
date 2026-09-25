<img src="../../assets/probe-logos/zookeeper.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache ZooKeeper

The `zookeeper` probe monitors an Apache ZooKeeper node via the `mntr`
four-letter command over raw TCP, reporting request latency, packet counts,
connection counts, znode and watch counts, file descriptor usage and ensemble
state (leader/follower/observer).

## Quick start

```yaml
# probes.d/10-zookeeper.yaml — each file under probes.d/ is a YAML array of probes
- name: zookeeper
  type: zookeeper
  params:
    host: localhost
    port: 2181
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | In practice | `localhost` | Node hostname or address |
| `port` | In practice | `2181` | Client port the four-letter commands are sent to |
| `timeout` | No | `10` | Connection and command timeout in seconds |
| `interval` | No | `30` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this node instead of host:port |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.zookeeper.up` | 1 | 1 when the node answered the `mntr` command |
| `zookeeper.latency.avg` | ms | Average request processing latency (`zk_avg_latency`) |
| `zookeeper.latency.max` | ms | Maximum request processing latency |
| `zookeeper.connections` | {connection} | Current client connections |
| `zookeeper.packets.received` | {packet} | Packets received since start |
| `zookeeper.packets.sent` | {packet} | Packets sent since start |
| `zookeeper.znodes` | {znode} | Number of znodes in the data tree |
| `zookeeper.watches` | {watch} | Number of active watches |
| `zookeeper.file_descriptors.open` | {fd} | Open file descriptors |
| `zookeeper.synced_followers` | # | Followers in sync with the leader (leader only) |
| `zookeeper.pending_syncs` | {sync} | Pending sync operations (leader only) |

## Operational notes

- The `mntr` four-letter command must be enabled in `zoo.cfg`: `4lw.commands.whitelist=mntr` (required since ZooKeeper 3.5).
- Leader-only metrics (`leader_elections`, `pending_syncs`) are only emitted by the ensemble leader; follower nodes emit zero or omit them.
- For multi-node ensembles, configure one probe instance per node to monitor the ensemble comprehensively.

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
| `senhub.zookeeper.up` | `senhub.zookeeper.up` | ZooKeeper Up | # | 1 when the ZooKeeper node answered the mntr command, 0 otherwise |
| `zookeeper.latency.avg` | `zookeeper.latency.avg` | ZooKeeper Avg Request Latency | ms | Average request processing latency in milliseconds (zk_avg_latency) |
| `zookeeper.latency.max` | `zookeeper.latency.max` | ZooKeeper Max Request Latency | ms | Maximum request processing latency in milliseconds (zk_max_latency) |
| `zookeeper.latency.min` | `zookeeper.latency.min` | ZooKeeper Min Request Latency | ms | Minimum request processing latency in milliseconds (zk_min_latency) |
| `zookeeper.packets.received` | `zookeeper.packets.received` | ZooKeeper Packets Received | # | Total packets received (zk_packets_received) |
| `zookeeper.packets.sent` | `zookeeper.packets.sent` | ZooKeeper Packets Sent | # | Total packets sent (zk_packets_sent) |
| `zookeeper.connections` | `zookeeper.connections` | ZooKeeper Alive Connections | # | Number of active client connections (zk_num_alive_connections) |
| `zookeeper.outstanding_requests` | `zookeeper.outstanding_requests` | ZooKeeper Outstanding Requests | # | Number of queued requests waiting to be processed (zk_outstanding_requests) |
| `zookeeper.znodes` | `zookeeper.znodes` | ZooKeeper ZNode Count | # | Total number of znodes in the data tree (zk_znode_count) |
| `zookeeper.watches` | `zookeeper.watches` | ZooKeeper Watch Count | # | Total number of active watches (zk_watch_count) |
| `zookeeper.ephemerals` | `zookeeper.ephemerals` | ZooKeeper Ephemeral Nodes | # | Number of ephemeral znodes (zk_ephemerals_count) |
| `zookeeper.data_size` | `zookeeper.data_size` | ZooKeeper Data Size | B | Approximate size of all znode data in bytes (zk_approximate_data_size) |
| `zookeeper.file_descriptors.open` | `zookeeper.file_descriptors.open` | ZooKeeper Open File Descriptors | # | Number of open file descriptors (zk_open_file_descriptor_count) |
| `zookeeper.file_descriptors.max` | `zookeeper.file_descriptors.max` | ZooKeeper Max File Descriptors | # | Maximum allowed file descriptors (zk_max_file_descriptor_count) |
| `zookeeper.followers` | `zookeeper.followers` | ZooKeeper Followers | # | Number of followers in the ensemble — leader only (zk_followers) |
| `zookeeper.synced_followers` | `zookeeper.synced_followers` | ZooKeeper Synced Followers | # | Number of followers in sync with the leader — leader only (zk_synced_followers) |
| `zookeeper.pending_syncs` | `zookeeper.pending_syncs` | ZooKeeper Pending Syncs | # | Number of pending sync operations — leader only (zk_pending_syncs) |
| `zookeeper.server_state` | `zookeeper.server_state` | ZooKeeper Server Role | # | Always 1; the 'state' attribute carries the role: leader, follower, or standalone |

<!-- schema:metrics:end -->
