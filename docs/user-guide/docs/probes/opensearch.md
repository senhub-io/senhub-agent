<img src="../../assets/probe-logos/opensearch.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# OpenSearch

The `opensearch` probe monitors an OpenSearch cluster via the REST JSON API,
collecting cluster health, JVM memory, indexing and search throughput, and
thread pool queue depths. The probe shares the same REST surface as
Elasticsearch; metric names use the `opensearch.*` namespace.

## Quick start

```yaml
# probes.d/20-opensearch.yaml — each file under probes.d/ is a YAML array of probes
- name: opensearch
  type: opensearch
  params:
    endpoint: http://localhost:9200
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:9200` | Base URL of the node; https:// for a cluster with the security plugin. Example: `https://os01:9200` |
| `username` | In practice | - | Basic-auth user; empty when the security plugin is disabled |
| `password` | In practice | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this node |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.opensearch.up` | 1 | 1 when the cluster health endpoint is reachable |
| `opensearch.cluster.health` | 1 | Cluster health: 2 = green, 1 = yellow, 0 = red |
| `opensearch.cluster.nodes` | {node} | Total nodes in the cluster |
| `opensearch.cluster.data_nodes` | {node} | Data nodes |
| `opensearch.cluster.shards.active` | {shard} | Active shards |
| `opensearch.cluster.shards.unassigned` | {shard} | Unassigned shards |
| `opensearch.jvm.memory.heap.used` | By | JVM heap in use on the local node |
| `opensearch.indexing.operations.completed` | # | Indexing operations completed, tagged with `operation` |
| `opensearch.search.operations.completed` | # | Search operations completed, tagged with `operation` (query, fetch) |
| `opensearch.thread_pool.tasks.queued` | {task} | Queued tasks per thread pool, tagged with `thread_pool` |

## Operational notes

- The probe queries `/_cluster/health` and `/_nodes/_local/stats`.
- For TLS-secured clusters (including OpenSearch Serverless), prefix the endpoint with `https://` and provide credentials.
- The OpenSearch security plugin is disabled in the Docker `opensearchproject/opensearch` image by default; for production clusters, security is typically enabled.

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
| `senhub.opensearch.up` | `senhub.opensearch.up` | OpenSearch Up | # | 1 when the cluster health endpoint is reachable, 0 otherwise |
| `opensearch.cluster.health` | `opensearch.cluster.health` | OpenSearch Cluster Health | # | Cluster health status encoded as integer: green=2, yellow=1, red=0 |
| `opensearch.cluster.nodes` | `opensearch.cluster.nodes` | OpenSearch Cluster Nodes | # | Total number of nodes in the cluster |
| `opensearch.cluster.data_nodes` | `opensearch.cluster.data_nodes` | OpenSearch Cluster Data Nodes | # | Number of data nodes in the cluster |
| `opensearch.cluster.shards.active` | `opensearch.cluster.shards.active` | OpenSearch Active Shards | # | Number of active primary and replica shards |
| `opensearch.cluster.shards.unassigned` | `opensearch.cluster.shards.unassigned` | OpenSearch Unassigned Shards | # | Number of shards not assigned to any node |
| `opensearch.cluster.shards.relocating` | `opensearch.cluster.shards.relocating` | OpenSearch Relocating Shards | # | Number of shards being relocated between nodes |
| `opensearch.cluster.pending_tasks` | `opensearch.cluster.pending_tasks` | OpenSearch Pending Tasks | # | Number of cluster-level changes not yet executed |
| `opensearch.jvm.memory.heap.used` | `opensearch.jvm.memory.heap.used` | OpenSearch JVM Heap Used | B | JVM heap memory currently used by the node |
| `opensearch.jvm.memory.heap.max` | `opensearch.jvm.memory.heap.max` | OpenSearch JVM Heap Max | B | Maximum JVM heap memory available to the node |
| `opensearch.jvm.gc.collections.count` | `opensearch.jvm.gc.collections.count` | OpenSearch GC {collector} Collections | # | Number of GC collections for the given collector (young\|old) |
| `opensearch.jvm.gc.collections.elapsed` | `opensearch.jvm.gc.collections.elapsed` | OpenSearch GC {collector} Time | ms | Cumulative wall-clock time spent in GC for the given collector |
| `opensearch.indexing.operations.completed` | `opensearch.indexing.operations.completed` | OpenSearch Indexing {operation} Completed | # | Cumulative number of indexing operations completed (operation=index) |
| `opensearch.indexing.operations.time` | `opensearch.indexing.operations.time` | OpenSearch Indexing {operation} Time | ms | Cumulative time spent in indexing operations |
| `opensearch.search.operations.completed` | `opensearch.search.operations.completed` | OpenSearch Search {operation} Completed | # | Cumulative search operations (operation=query\|fetch) |
| `opensearch.search.operations.time` | `opensearch.search.operations.time` | OpenSearch Search {operation} Time | ms | Cumulative time spent in search operations |
| `opensearch.process.cpu.usage` | `opensearch.process.cpu.usage` | OpenSearch Process CPU Usage | % | CPU utilisation of the OpenSearch process as a percentage (0-100) |
| `opensearch.os.memory.used` | `opensearch.os.memory.used` | OpenSearch OS Memory Used | B | Physical memory used by the OS on the node |
| `opensearch.thread_pool.tasks.queued` | `opensearch.thread_pool.tasks.queued` | OpenSearch Thread Pool {thread_pool} Queued | # | Number of tasks currently queued in the thread pool |
| `opensearch.thread_pool.tasks.completed` | `opensearch.thread_pool.tasks.completed` | OpenSearch Thread Pool {thread_pool} Completed | # | Cumulative tasks completed by the thread pool |
| `opensearch.thread_pool.tasks.rejected` | `opensearch.thread_pool.tasks.rejected` | OpenSearch Thread Pool {thread_pool} Rejected | # | Cumulative tasks rejected by the thread pool (queue full) |

<!-- schema:metrics:end -->
