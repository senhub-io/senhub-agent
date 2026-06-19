!!! info
    **License: Free** — part of the universal collection tier.

# Apache Cassandra

The `cassandra` probe monitors Apache Cassandra via Jolokia HTTP REST,
covering connections, request latency and errors (read/write), compaction
pending tasks, storage load, JVM heap and garbage collection.

## Quick start

```yaml
probes:
  - name: cassandra
    type: cassandra
    params:
      jolokia_url: http://localhost:8778/jolokia
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `jolokia_url` | `http://localhost:8778/jolokia` | URL to the Jolokia agent endpoint on the Cassandra node |
| `instance_name` | — | Override for the entity instance id (useful in multi-agent setups monitoring the same cluster) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.cassandra.up` | 1 | 1 when the Jolokia endpoint is reachable, 0 otherwise |
| `cassandra.client.connections` | {connection} | Native CQL client connections |
| `cassandra.client.requests.count` | {request} | Request count by operation (read/write), tagged with `operation` |
| `cassandra.client.requests.failed` | {request} | Failed requests by operation |
| `cassandra.client.request.latency.50p` | ms | Median request latency by operation |
| `cassandra.client.request.latency.99p` | ms | 99th-percentile request latency by operation |
| `cassandra.compaction.tasks.pending` | {task} | Compaction tasks waiting to run |
| `cassandra.storage.load` | By | Disk space used by the local node |
| `jvm.memory.heap.used` | By | JVM heap in use |
| `jvm.gc.collections.count` | {collection} | GC collections by collector (Minor / Major), tagged with `collector` |

## Operational notes

- Requires Jolokia deployed as a Java agent or embedded in the Cassandra process. The `bitnami/cassandra` image ships Jolokia at port 8778 by default; vanilla Cassandra requires the javaagent to be added to `cassandra-env.sh`.
- Latency values from Cassandra MBeans are in microseconds; the probe converts them to milliseconds.
- Metrics align with the OpenTelemetry Collector contrib `cassandraReceiver` where names exist.
