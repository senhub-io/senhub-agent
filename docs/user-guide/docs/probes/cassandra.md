<img src="../../assets/probe-logos/cassandra.svg" alt="" class="probe-page-logo probe-page-logo-wm">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache Cassandra

The `cassandra` probe monitors Apache Cassandra via Jolokia HTTP REST,
covering connections, request latency and errors (read/write), compaction
pending tasks, storage load, JVM heap and garbage collection.

## Quick start

```yaml
# probes.d/10-cassandra.yaml — each file under probes.d/ is a YAML array of probes
- name: cassandra
  type: cassandra
  params:
    jolokia_url: http://localhost:8778/jolokia
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `jolokia_url` | In practice | `http://localhost:8778/jolokia` | URL of the Jolokia agent attached to the Cassandra JVM. Example: `http://cassandra01:8778/jolokia` |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this node |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.cassandra.up` | 1 | 1 when the Jolokia endpoint is reachable, 0 otherwise |
| `cassandra.client.connections` | {connection} | Native CQL client connections |
| `cassandra.client.requests.count` | {request} | Request count by operation (read/write), tagged with `operation` |
| `cassandra.client.requests.errors` | {request} | Failed requests by operation |
| `cassandra.client.requests.latency` | ms | Mean client request latency, read or write |
| `cassandra.client.requests.latency.p99` | ms | 99th-percentile client request latency, read or write |
| `cassandra.compaction.tasks.pending` | {task} | Compaction tasks waiting to run |
| `cassandra.storage.load` | By | Disk space used by the local node |
| `jvm.memory.heap.used` | By | JVM heap in use |
| `jvm.gc.collections.count` | {collection} | GC collections by collector (Minor / Major), tagged with `collector` |

## Operational notes

- Requires Jolokia deployed as a Java agent or embedded in the Cassandra process. The `bitnami/cassandra` image ships Jolokia at port 8778 by default; vanilla Cassandra requires the javaagent to be added to `cassandra-env.sh`.
- Latency values from Cassandra MBeans are in microseconds; the probe converts them to milliseconds.
- Metrics align with the OpenTelemetry Collector contrib `cassandraReceiver` where names exist.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.cassandra.up` | `cassandra_up` | # | 1 when the Cassandra Jolokia endpoint is reachable, 0 otherwise |
| `cassandra.client.connections` | `cassandra_connections` | # | Number of clients connected to the native CQL transport |
| `cassandra.client.requests.count` | `cassandra_requests_{operation}` | # | Total number of client requests (Read or Write) |
| `cassandra.client.requests.latency` | `cassandra_latency_mean_{operation}` | ms | Mean client request latency in milliseconds (Read or Write) |
| `cassandra.client.requests.latency.p99` | `cassandra_latency_p99_{operation}` | ms | 99th percentile client request latency in milliseconds (Read or Write) |
| `cassandra.client.requests.errors` | `cassandra_errors_{operation}` | # | Total number of client request errors (Read or Write) |
| `cassandra.compaction.tasks.completed` | `cassandra_compaction_completed` | # | Total number of completed compaction tasks |
| `cassandra.compaction.tasks.pending` | `cassandra_compaction_pending` | # | Number of pending compaction tasks |
| `cassandra.storage.load` | `cassandra_storage_load` | B | Total size of all SSTables on disk in bytes |
| `cassandra.storage.total_hints` | `cassandra_storage_hints` | # | Total number of hints stored since last restart |
| `jvm.memory.heap.used` | `cassandra_jvm_heap_used` | B | JVM heap memory currently in use by the Cassandra process |
| `jvm.gc.collections.count` | `cassandra_gc_count_{collector}` | # | Total number of garbage collections performed by the named GC collector |
| `jvm.gc.collections.elapsed` | `cassandra_gc_time_{collector}` | ms | Total elapsed time in milliseconds spent in garbage collection by the named GC collector |

<!-- schema:metrics:end -->
