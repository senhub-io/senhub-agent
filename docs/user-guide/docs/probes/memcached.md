<img src="https://cdn.simpleicons.org/memcached" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Required | Default | Description |
|---|---|---|---|
| `host` | No | `localhost` | Server hostname or address |
| `port` | No | `11211` | Server TCP port |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `5` | Connection and command timeout in seconds |
| `instance_name` | No | - | Stable identity of this server instead of host:port |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.memcached.up` | 1 | 1 when the server responds to `stats`, 0 otherwise |
| `memcached.uptime` | s | Seconds since the Memcached server started |
| `memcached.current_connections` | {connection} | Active client connections |
| `memcached.total_connections` | {connection} | Total connections accepted since start |
| `memcached.current_items` | {item} | Items currently stored |
| `memcached.total_items` | {item} | Items stored since start |
| `memcached.bytes` | By | Current memory used for item storage |
| `memcached.limit_maxbytes` | By | Configured memory limit |
| `memcached.operations` | {operation} | Get/set/delete/etc. operations, tagged with `command` |
| `memcached.hits` | {operation} | Cache hits by command, tagged with `command` |
| `memcached.misses` | {operation} | Cache misses by command |
| `memcached.evictions` | {eviction} | Items evicted to free memory |

## Operational notes

- Memcached has no authentication mechanism; ensure the port is not exposed to untrusted networks.
- Metric names follow the OpenTelemetry Collector contrib `memcachedreceiver` convention where equivalents exist.
