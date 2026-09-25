<img src="../../assets/probe-logos/memcached.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Memcached

The `memcached` probe monitors a Memcached server via the TCP text protocol
(`stats` command), reporting connection counts, item counts, memory usage,
cache hit/miss ratios, command throughput and eviction counters.

## Quick start

```yaml
# probes.d/10-memcached.yaml — each file under probes.d/ is a YAML array of probes
- name: memcached
  type: memcached
  params:
    host: localhost
    port: 11211
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:fe7afd69a1c0dd220ec6b8f8430f8d45133893b9f3f0bbb0dd0688acb1cc1aa9 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | In practice | `localhost` | Server hostname or address |
| `port` | In practice | `11211` | Server TCP port |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `5` | Connection and command timeout in seconds |
| `instance_name` | No | - | Stable identity of this server instead of host:port |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.memcached.up` | 1 | 1 when the server responds to `stats`, 0 otherwise |
| `memcached.uptime` | s | Seconds since the Memcached server started |
| `memcached.current.connections` | {connection} | Active client connections |
| `memcached.connections.total` | {connection} | Total connections accepted since start |
| `memcached.current.items` | {item} | Items currently stored |
| `memcached.items.total` | {item} | Items stored since start |
| `memcached.bytes` | By | Current memory used for item storage |
| `memcached.limit_maxbytes` | By | Configured memory limit |
| `memcached.operations` | {operation} | Get/set/delete/etc. operations, tagged with `command` |
| `memcached.operations` | # | Cache get operations by result, tagged with `result` (hit, miss) |
| `memcached.evictions` | {eviction} | Items evicted to free memory |

## Operational notes

- Memcached has no authentication mechanism; ensure the port is not exposed to untrusted networks.
- Metric names follow the OpenTelemetry Collector contrib `memcachedreceiver` convention where equivalents exist.

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
| `senhub.memcached.up` | `senhub.memcached.up` | Memcached Reachability | # | 1 when the Memcached server is reachable and responding to stats, 0 otherwise |
| `memcached.uptime` | `memcached.uptime` | Memcached Uptime | s | Time since the Memcached server started |
| `memcached.current.connections` | `memcached.current.connections` | Memcached Current Connections | # | Number of currently open connections to the Memcached server |
| `memcached.connections.total` | `memcached.connections.total` | Memcached Total Connections | # | Total connections opened since server start |
| `memcached.current.items` | `memcached.current.items` | Memcached Current Items | # | Number of items currently stored in the cache |
| `memcached.items.total` | `memcached.items.total` | Memcached Total Items | # | Total items stored since server start |
| `memcached.bytes` | `memcached.bytes` | Memcached Memory Used | B | Current number of bytes used to store items |
| `memcached.limit_maxbytes` | `memcached.limit_maxbytes` | Memcached Memory Limit | B | Maximum number of bytes the server is allowed to use for storage |
| `memcached.network` | `memcached.network` | Memcached Network {direction} | B | Total bytes transferred since server start, by direction (transmit=sent to clients, receive=received from clients) |
| `memcached.operations` | `memcached.operations` | Memcached Operations {result} | # | Number of cache get operations by result (hit or miss) |
| `memcached.commands` | `memcached.commands` | Memcached Commands {command} | # | Number of commands executed, by command type (get/set/flush) |
| `memcached.evictions` | `memcached.evictions` | Memcached Evictions | # | Number of items evicted from the cache due to memory pressure |
| `memcached.cpu.usage` | `memcached.cpu.usage` | Memcached CPU {state} | s | CPU time consumed by the Memcached process, by state (user or system) |

<!-- schema:metrics:end -->
