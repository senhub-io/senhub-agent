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
