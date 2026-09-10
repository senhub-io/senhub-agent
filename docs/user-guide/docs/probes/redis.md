<img src="https://cdn.simpleicons.org/redis" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Required | Default | Description |
|---|---|---|---|
| `host` | No | `127.0.0.1` | Server hostname or address |
| `port` | No | `6379` | Server port |
| `password` | No | - | AUTH password, when required. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `tls` | No | `false` | Use TLS for the connection |
| `tls_cert_file` | No | - | Client certificate (PEM) for mutual TLS; needs tls_key_file and tls: true |
| `tls_key_file` | No | - | Private key (PEM) matching tls_cert_file |
| `tls_ca_file` | No | - | CA bundle (PEM) to verify the server; system trust store by default |
| `timeout` | No | `5` | Connection and command timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity instead of host:port |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `host` | `127.0.0.1` | Redis server hostname or IP |
| `port` | `6379` | Redis server port |
| `password` | — | Redis `AUTH` password (if required) — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |
| `tls` | `false` | Enable TLS for the Redis connection |
| `tls_cert_file` | — | Path to a PEM client certificate, presented to the server for mutual TLS (requires `tls_key_file`) |
| `tls_key_file` | — | Path to the PEM private key matching `tls_cert_file` (requires `tls_cert_file`) |
| `tls_ca_file` | — | Path to a PEM CA bundle used to verify the server certificate (defaults to the system trust store) |
| `timeout` | `5` | Connection and command timeout in seconds |
| `interval` | `60` | Seconds between collections |
| `instance_name` | — | Logical name used as the entity instance identifier instead of `host:port`; set it to keep the identity stable if the address changes |

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
| `redis.memory.fragmentation_ratio` | 1 | Memory fragmentation ratio |
| `redis.commands.processed` | {command} | Total commands processed since start |
| `redis.keyspace.hits` | {hit} | Successful key lookups |
| `redis.keyspace.misses` | {miss} | Failed key lookups |
| `redis.keys.expired` | {key} | Keys expired since start |
| `redis.keys.evicted` | {key} | Keys evicted due to `maxmemory` policy |
| `redis.replication.lag` | s | Replica lag in seconds (replica instances only) |
| `redis.rdb.last_save.duration` | s | Duration of the last successful RDB save |

## Operational notes

- Valkey (the Redis fork) exposes the same `INFO` interface; configure it identically.
- For Redis Cluster, point the probe at one node; cluster-wide stats are reported per node, not aggregated.
- Metric names align with the community `redis_exporter` baseline.
