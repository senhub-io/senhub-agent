<img src="../../assets/probe-logos/redis.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Redis / Valkey

The `redis` probe monitors Redis (or Valkey) via the `INFO` command, reporting
memory usage, client connections, command throughput, cache hit/miss ratio,
keyspace size, replication state and persistence (RDB/AOF) health.

## Quick start

```yaml
# probes.d/10-redis.yaml — each file under probes.d/ is a YAML array of probes
- name: redis
  type: redis
  params:
    host: 127.0.0.1
    port: 6379
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | In practice | `127.0.0.1` | Server hostname or address |
| `port` | In practice | `6379` | Server port |
| `password` | In practice | - | AUTH password, when required. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `tls` | No | `false` | Use TLS for the connection |
| `tls_cert_file` | No | - | Client certificate (PEM) for mutual TLS; needs tls_key_file and tls: true |
| `tls_key_file` | No | - | Private key (PEM) matching tls_cert_file |
| `tls_ca_file` | No | - | CA bundle (PEM) to verify the server; system trust store by default |
| `timeout` | No | `5` | Connection and command timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity instead of host:port |

<!-- schema:params:end -->

Set `instance_name` to keep the entity identity stable when the address changes.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | 1 | 1 when the agent reached the Redis server |
| `redis.uptime` | s | Server uptime |
| `redis.clients.connected` | {connection} | Connected client count |
| `redis.clients.blocked` | {connection} | Clients blocked on a command (BLPOP, etc.) |
| `redis.connections.received` | {connection} | Total connections accepted since start |
| `redis.memory.used` | By | Memory currently allocated by Redis |
| `redis.memory.peak` | By | Peak memory allocation |
| `redis.memory.fragmentation.ratio` | 1 | Memory fragmentation ratio |
| `redis.commands.processed` | {command} | Total commands processed since start |
| `redis.keyspace.hits` | {hit} | Successful key lookups |
| `redis.keyspace.misses` | {miss} | Failed key lookups |
| `redis.expired_keys` | {key} | Keys expired since start |
| `redis.evicted_keys` | {key} | Keys evicted due to `maxmemory` policy |
| `redis.replication.lag` | s | Replica lag in seconds (replica instances only) |
| `redis.rdb.last_bgsave.duration` | s | Duration of the last RDB background save |

## Operational notes

- Valkey (the Redis fork) exposes the same `INFO` interface; configure it identically.
- For Redis Cluster, point the probe at one node; cluster-wide stats are reported per node, not aggregated.
- Metric names align with the community `redis_exporter` baseline.

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
| `senhub.db.up` | `senhub.db.up` | Redis Up | # | 1 if the agent reached the Redis server, 0 otherwise |
| `redis.uptime` | `redis.uptime` | Uptime | s | Seconds since the Redis server started (uptime_in_seconds) |
| `senhub.db.version.info` | `senhub.db.version.info` | Version | # | Redis version banner — value=1, version string carried in attribute db.system.version |
| `redis.clients.connected` | `redis.clients.connected` | Clients Connected | # | Number of client connections (connected_clients) |
| `redis.clients.blocked` | `redis.clients.blocked` | Clients Blocked | # | Clients blocked waiting on a blocking call (blocked_clients) |
| `redis.connections.received` | `redis.connections.received` | Connections Received | # | Cumulative accepted connections (total_connections_received) |
| `redis.connections.rejected` | `redis.connections.rejected` | Connections Rejected | # | Cumulative rejected connections — maxclients cap (rejected_connections) |
| `redis.memory.used` | `redis.memory.used` | Memory Used | B | Allocator-reported used memory (used_memory) |
| `redis.memory.used.rss` | `redis.memory.used.rss` | Memory RSS | B | OS-reported RSS (used_memory_rss) — includes fragmentation overhead |
| `redis.memory.peak` | `redis.memory.peak` | Memory Peak | B | Peak allocator-reported memory (used_memory_peak) |
| `redis.memory.fragmentation.ratio` | `redis.memory.fragmentation.ratio` | Memory Fragmentation Ratio | # | used_memory_rss / used_memory — greater than 1.5 indicates high fragmentation |
| `redis.commands.processed` | `redis.commands.processed` | Commands Processed | # | Cumulative commands processed (total_commands_processed) |
| `redis.net.input` | `redis.net.input` | Network Input | B | Cumulative bytes received from clients (total_net_input_bytes) |
| `redis.net.output` | `redis.net.output` | Network Output | B | Cumulative bytes sent to clients (total_net_output_bytes) |
| `redis.ops.per_sec` | `redis.ops.per_sec` | Ops/s | # | Instantaneous commands per second (instantaneous_ops_per_sec) |
| `redis.keyspace.hits` | `redis.keyspace.hits` | Keyspace Hits | # | Cumulative successful key lookups (keyspace_hits) |
| `redis.keyspace.misses` | `redis.keyspace.misses` | Keyspace Misses | # | Cumulative failed key lookups (keyspace_misses) |
| `redis.keyspace.hit.ratio` | `redis.keyspace.hit.ratio` | Hit Ratio | % | keyspace_hits / (keyspace_hits + keyspace_misses) — derived gauge, 0 when no traffic |
| `redis.db.keys` | `redis.db.keys` | DB {db} Keys | # | Total keys in logical database (keyspace section: dbN:keys=K) |
| `redis.db.expires` | `redis.db.expires` | DB {db} Expiring Keys | # | Keys with a TTL in logical database (keyspace section: dbN:expires=M) |
| `redis.replication.role` | `redis.replication.role` | Replication Role | # | Instance role: master=1, slave/replica=0, sentinel=-1 |
| `redis.replication.offset` | `redis.replication.offset` | Replication Offset | B | Replication offset in bytes (master_repl_offset on master, slave_repl_offset / replica_repl_offset on replica) |
| `redis.replication.slaves.connected` | `redis.replication.slaves.connected` | Replicas Connected | # | Number of replicas currently connected (connected_slaves, master only) |
| `redis.replication.lag` | `redis.replication.lag` | Replication Lag | s | Seconds since last master communication (master_last_io_seconds_ago, replica only) |
| `redis.rdb.changes` | `redis.rdb.changes` | RDB Changes Since Save | # | Writes since last RDB save (rdb_changes_since_last_save) |
| `redis.aof.enabled` | `redis.aof.enabled` | AOF Enabled | # | 1 when AOF persistence is enabled (aof_enabled) |
| `redis.rdb.last_bgsave.duration` | `redis.rdb.last_bgsave.duration` | RDB Last BGSave Duration | s | Duration of the last RDB background save in seconds (rdb_last_bgsave_time_sec) |
| `redis.rdb.last_save.age` | `redis.rdb.last_save.age` | RDB Last Save Age | s | Seconds elapsed since the last successful RDB save (now − rdb_last_save_time) |
| `redis.latest.fork` | `redis.latest.fork` | Latest Fork Duration | μs | Duration of the latest fork operation in microseconds (latest_fork_usec) |
| `redis.cpu.time` | `redis.cpu.time` | CPU Time ({state}) | s | Cumulative CPU time consumed by the Redis server in the given state (used_cpu_sys / used_cpu_user / used_cpu_sys_children / used_cpu_user_children) |
| `redis.memory.lua` | `redis.memory.lua` | Memory Lua | B | Memory used by the Lua scripting engine (used_memory_lua) |
| `redis.clients.max_input_buffer` | `redis.clients.max_input_buffer` | Client Max Input Buffer | B | Largest input buffer across all current client connections (client_recent_max_input_buffer) |
| `redis.clients.max_output_buffer` | `redis.clients.max_output_buffer` | Client Max Output Buffer | B | Largest output buffer across all current client connections (client_recent_max_output_buffer) |
| `redis.db.avg_ttl` | `redis.db.avg_ttl` | DB {db} Average TTL | ms | Average TTL in milliseconds of keys with an expiry in logical database (keyspace section: dbN:avg_ttl=T) |
| `redis.replication.backlog_first_byte_offset` | `redis.replication.backlog_first_byte_offset` | Replication Backlog First Byte Offset | B | Replication backlog first byte offset (repl_backlog_first_byte_offset) |
| `redis.evicted_keys` | `redis.evicted_keys` | Evicted Keys | # | Cumulative keys evicted due to maxmemory policy (evicted_keys) |
| `redis.expired_keys` | `redis.expired_keys` | Expired Keys | # | Cumulative keys expired by the TTL mechanism (expired_keys) |
| `redis.cmd.calls` | `redis.cmd.calls` | Cmd {cmd} Calls | # | Cumulative call count for the given Redis command (INFO commandstats: cmdstat_X:calls=N) |
| `redis.cmd.usec` | `redis.cmd.usec` | Cmd {cmd} Usec | μs | Cumulative microseconds spent executing the given Redis command (INFO commandstats: cmdstat_X:usec=N) |
| `redis.cluster.state` | `redis.cluster.state` | Cluster State | # | Cluster health: 1=ok, 0=fail (cluster_state from INFO cluster; emitted only when cluster_enabled=1) |
| `redis.cluster.slots.assigned` | `redis.cluster.slots.assigned` | Cluster Slots Assigned | # | Number of slots assigned to cluster nodes (cluster_slots_assigned) |
| `redis.cluster.slots.ok` | `redis.cluster.slots.ok` | Cluster Slots OK | # | Number of slots not in FAIL or PFAIL state (cluster_slots_ok) |
| `redis.cluster.slots.pfail` | `redis.cluster.slots.pfail` | Cluster Slots PFAIL | # | Number of slots in PFAIL state (cluster_slots_pfail) |
| `redis.cluster.slots.fail` | `redis.cluster.slots.fail` | Cluster Slots FAIL | # | Number of slots in FAIL state — serving requests is blocked until fixed (cluster_slots_fail) |
| `redis.cluster.links.created` | `redis.cluster.links.created` | Cluster Messages Sent | # | Cumulative cluster bus messages sent (cluster_stats_messages_sent) |
| `redis.cluster.links.disconnected` | `redis.cluster.links.disconnected` | Cluster Messages Received | # | Cumulative cluster bus messages received (cluster_stats_messages_received) |
| `redis.sentinel.masters` | `redis.sentinel.masters` | Sentinel Masters | # | Number of Redis masters monitored by this Sentinel (sentinel_masters; emitted only in sentinel mode) |
| `redis.sentinel.slaves` | `redis.sentinel.slaves` | Sentinel Slaves (total) | # | Total number of replicas across all monitored masters (aggregated from per-master lines) |
| `redis.sentinel.ok_slaves` | `redis.sentinel.ok_slaves` | Sentinel Slaves OK | # | Replicas belonging to masters whose status=ok (aggregated from per-master lines) |
| `redis.sentinel.sentinels` | `redis.sentinel.sentinels` | Sentinel Sentinels (total) | # | Total number of Sentinel peers across all monitored masters (aggregated from per-master lines) |
| `redis.sentinel.ok_sentinels` | `redis.sentinel.ok_sentinels` | Sentinel Sentinels OK | # | Sentinel peers belonging to masters whose status=ok (aggregated from per-master lines) |
| `redis.sentinel.scripts_queue_length` | `redis.sentinel.scripts_queue_length` | Sentinel Scripts Queue | # | Number of scripts in the Sentinel notification-scripts queue (sentinel_running_scripts) |
| `redis.tracking.clients` | `redis.tracking.clients` | Tracking Clients | # | Number of clients using RESP3 client-side caching tracking (tracking_clients; 0 when absent) |
| `redis.tracking.keys` | `redis.tracking.keys` | Tracking Keys | # | Number of keys in the client-side tracking invalidation table (tracking_table_used_keys; 0 when absent) |

<!-- schema:metrics:end -->
