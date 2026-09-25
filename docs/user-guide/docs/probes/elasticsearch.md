<img src="../../assets/probe-logos/elasticsearch.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Elasticsearch

The `elasticsearch` probe monitors an Elasticsearch cluster via the REST JSON
API, collecting cluster health, JVM memory, indexing and search throughput,
and thread pool queue depths.

## Quick start

```yaml
# probes.d/10-elasticsearch.yaml — each file under probes.d/ is a YAML array of probes
- name: elasticsearch
  type: elasticsearch
  params:
    endpoint: http://localhost:9200
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:9200` | Base URL of the node; https:// for a secured cluster. Example: `https://es01:9200` |
| `username` | In practice | - | Basic-auth user; empty when security is disabled |
| `password` | In practice | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this node |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.elasticsearch.up` | 1 | 1 when the cluster health endpoint is reachable |
| `elasticsearch.cluster.health` | 1 | Cluster health: 2 = green, 1 = yellow, 0 = red |
| `elasticsearch.cluster.nodes` | {node} | Total nodes in the cluster |
| `elasticsearch.cluster.data_nodes` | {node} | Data nodes in the cluster |
| `elasticsearch.cluster.shards.active` | {shard} | Active primary and replica shards |
| `elasticsearch.cluster.shards.unassigned` | {shard} | Unassigned shards |
| `elasticsearch.jvm.memory.heap.used` | By | JVM heap in use on the local node |
| `elasticsearch.indexing.operations.completed` | # | Indexing operations completed, tagged with `operation` (index, delete) |
| `elasticsearch.search.operations.completed` | # | Search operations completed, tagged with `operation` (query, fetch) |
| `elasticsearch.thread_pool.tasks.queued` | {task} | Tasks queued per thread pool, tagged with `thread_pool` |

## Operational notes

- The probe queries `/_cluster/health` (cluster-level) and `/_nodes/_local/stats` (node-level).
- For TLS-secured clusters, prefix the endpoint with `https://` and ensure the certificate is valid or configure system trust.
- Metrics align with the OpenTelemetry Collector contrib `elasticsearchreceiver` where naming equivalents exist.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.elasticsearch.up` | `elasticsearch_up` | # | 1 when the cluster health endpoint is reachable, 0 otherwise |
| `elasticsearch.cluster.health` | `elasticsearch_cluster_health` | # | Cluster health status encoded as integer: green=2, yellow=1, red=0 |
| `elasticsearch.cluster.nodes` | `elasticsearch_cluster_nodes` | # | Total number of nodes in the cluster |
| `elasticsearch.cluster.data_nodes` | `elasticsearch_cluster_data_nodes` | # | Number of data nodes in the cluster |
| `elasticsearch.cluster.shards.active` | `elasticsearch_shards_active` | # | Number of active primary and replica shards |
| `elasticsearch.cluster.shards.unassigned` | `elasticsearch_shards_unassigned` | # | Number of shards not assigned to any node |
| `elasticsearch.cluster.shards.relocating` | `elasticsearch_shards_relocating` | # | Number of shards being relocated between nodes |
| `elasticsearch.cluster.pending_tasks` | `elasticsearch_pending_tasks` | # | Number of cluster-level changes not yet executed |
| `elasticsearch.jvm.memory.heap.used` | `elasticsearch_jvm_heap_used` | B | JVM heap memory currently used by the node |
| `elasticsearch.jvm.memory.heap.max` | `elasticsearch_jvm_heap_max` | B | Maximum JVM heap memory available to the node |
| `elasticsearch.jvm.gc.collections.count` | `elasticsearch_gc_{collector}_count` | # | Number of GC collections for the given collector (young\|old) |
| `elasticsearch.jvm.gc.collections.elapsed` | `elasticsearch_gc_{collector}_time` | ms | Cumulative wall-clock time spent in GC for the given collector |
| `elasticsearch.indexing.operations.completed` | `elasticsearch_indexing_{operation}_completed` | # | Cumulative number of indexing operations completed (operation=index) |
| `elasticsearch.indexing.operations.time` | `elasticsearch_indexing_{operation}_time` | ms | Cumulative time spent in indexing operations |
| `elasticsearch.search.operations.completed` | `elasticsearch_search_{operation}_completed` | # | Cumulative search operations (operation=query\|fetch) |
| `elasticsearch.search.operations.time` | `elasticsearch_search_{operation}_time` | ms | Cumulative time spent in search operations |
| `elasticsearch.process.cpu.usage` | `elasticsearch_process_cpu` | % | CPU utilisation of the Elasticsearch process as a percentage (0-100) |
| `elasticsearch.os.memory.used` | `elasticsearch_os_memory_used` | B | Physical memory used by the OS on the node |
| `elasticsearch.thread_pool.tasks.queued` | `elasticsearch_tp_{thread_pool}_queued` | # | Number of tasks currently queued in the thread pool |
| `elasticsearch.thread_pool.tasks.completed` | `elasticsearch_tp_{thread_pool}_completed` | # | Cumulative tasks completed by the thread pool |
| `elasticsearch.thread_pool.tasks.rejected` | `elasticsearch_tp_{thread_pool}_rejected` | # | Cumulative tasks rejected by the thread pool (queue full) |

<!-- schema:metrics:end -->
