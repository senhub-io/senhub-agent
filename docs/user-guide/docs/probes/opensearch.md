<img src="https://cdn.simpleicons.org/opensearch" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:9200` | OpenSearch base URL |
| `username` | — | Basic-auth username (for clusters with security plugin enabled) |
| `password` | — | Basic-auth password |

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
| `opensearch.indexing.request.operations.count` | {operation} | Indexing operations by type, tagged with `operation` |
| `opensearch.search.query.count` | {query} | Completed search queries |
| `opensearch.thread_pool.tasks.queued` | {task} | Queued tasks per thread pool, tagged with `thread_pool` |

## Operational notes

- The probe queries `/_cluster/health` and `/_nodes/_local/stats`.
- For TLS-secured clusters (including OpenSearch Serverless), prefix the endpoint with `https://` and provide credentials.
- The OpenSearch security plugin is disabled in the Docker `opensearchproject/opensearch` image by default; for production clusters, security is typically enabled.
