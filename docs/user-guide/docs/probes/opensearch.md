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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | No | `http://localhost:9200` | Base URL of the node; https:// for a cluster with the security plugin. Example: `https://os01:9200` |
| `username` | No | - | Basic-auth user; empty when the security plugin is disabled |
| `password` | No | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
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
| `opensearch.indexing.request.operations.count` | {operation} | Indexing operations by type, tagged with `operation` |
| `opensearch.search.query.count` | {query} | Completed search queries |
| `opensearch.thread_pool.tasks.queued` | {task} | Queued tasks per thread pool, tagged with `thread_pool` |

## Operational notes

- The probe queries `/_cluster/health` and `/_nodes/_local/stats`.
- For TLS-secured clusters (including OpenSearch Serverless), prefix the endpoint with `https://` and provide credentials.
- The OpenSearch security plugin is disabled in the Docker `opensearchproject/opensearch` image by default; for production clusters, security is typically enabled.
