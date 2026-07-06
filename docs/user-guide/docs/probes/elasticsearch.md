<img src="https://cdn.simpleicons.org/elasticsearch" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:9200` | Elasticsearch base URL |
| `username` | — | Basic-auth username (for clusters with security enabled) |
| `password` | — | Basic-auth password — reference via `${secret:elasticsearch.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |

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
| `elasticsearch.indexing.document.merges.count` | {merge} | Segment merges by type, tagged with `collector` |
| `elasticsearch.indexing.request.operations.count` | {operation} | Indexing operations (index/delete/…), tagged with `operation` |
| `elasticsearch.search.query.count` | {query} | Completed search queries |
| `elasticsearch.search.fetch.count` | {fetch} | Completed fetch phases |
| `elasticsearch.thread_pool.tasks.queued` | {task} | Tasks queued per thread pool, tagged with `thread_pool` |

## Operational notes

- The probe queries `/_cluster/health` (cluster-level) and `/_nodes/_local/stats` (node-level).
- For TLS-secured clusters, prefix the endpoint with `https://` and ensure the certificate is valid or configure system trust.
- Metrics align with the OpenTelemetry Collector contrib `elasticsearchreceiver` where naming equivalents exist.
