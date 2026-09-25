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
