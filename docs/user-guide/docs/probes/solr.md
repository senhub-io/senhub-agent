!!! info
    **License: Free** — part of the universal collection tier.

# Apache Solr

The `solr` probe monitors Apache Solr via the native metrics API
(`/solr/admin/metrics?wt=json&group=all`) and the core status endpoint
(`/solr/admin/cores?action=STATUS`), reporting JVM heap and threads,
node-level request and cache counters, and per-core document count and index
size.

## Quick start

```yaml
probes:
  - name: solr
    type: solr
    params:
      endpoint: http://localhost:8983
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8983` | Solr base URL (without trailing slash) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.solr.up` | 1 | 1 when the Solr admin metrics endpoint responded |
| `jvm.memory.heap.used` | By | JVM heap memory used by the Solr process |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.threads.count` | {thread} | Current live JVM thread count |
| `solr.request.count` | {request} | Requests processed by the node |
| `solr.request.errors` | {error} | Request errors on the node |
| `solr.request.latency.avg` | ms | Average request latency |
| `solr.cache.lookups` | {lookup} | Cache lookups (filter / query / document cache) |
| `solr.cache.hits` | {hit} | Cache hits |
| `solr.core.document.count` | {document} | Number of indexed documents per core, tagged with `core` |
| `solr.core.index.size` | By | Index size on disk per core |

## Operational notes

- No authentication is required by default. If Solr is configured with Basic Auth, the probe does not yet support credentials — use the unauthenticated path or a local loopback address.
- For SolrCloud, point the probe at one node; cluster-wide aggregates are not covered in this release.
