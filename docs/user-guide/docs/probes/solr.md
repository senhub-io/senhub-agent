<img src="../../assets/probe-logos/solr.svg" alt="" class="probe-page-logo probe-page-logo-si">

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
# probes.d/10-solr.yaml — each file under probes.d/ is a YAML array of probes
- name: solr
  type: solr
  params:
    endpoint: http://localhost:8983
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8983` | Base URL of the node, without the /solr path. Example: `http://solr01:8983` |
| `jolokia_url` | No | - | Alternative spelling of endpoint kept for consistency with the JVM probes; its scheme, host and port replace endpoint, the path is dropped |
| `timeout` | No | `10` | HTTP request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity override for this node |

<!-- schema:params:end -->

`jolokia_url` does not switch the probe to Jolokia: the native metrics API
is always what it reads. It is accepted so a configuration written for the
JVM probes keeps working, and only its scheme, host and port are used.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.solr.up` | 1 | 1 when the Solr admin metrics endpoint responded |
| `jvm.memory.heap.used` | By | JVM heap memory used by the Solr process |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.threads.count` | {thread} | Current live JVM thread count |
| `solr.requests.count` | {request} | Requests processed by the node |
| `solr.errors.count` | {error} | Request errors on the node |
| `solr.requests.time` | ms | Time spent handling QUERY requests (cumulative) |
| `solr.cache.inserts` | # | Query result cache inserts (cumulative) |
| `solr.cache.hits` | {hit} | Cache hits |
| `solr.document.count` | {document} | Number of indexed documents per core, tagged with `core` |
| `solr.index.size` | By | Index size on disk per core |

## Operational notes

- No authentication is required by default. If Solr is configured with Basic Auth, the probe does not yet support credentials — use the unauthenticated path or a local loopback address.
- For SolrCloud, point the probe at one node; cluster-wide aggregates are not covered in this release.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.solr.up` | `solr_up` | # | 1 when the Solr admin metrics endpoint responded successfully, 0 otherwise |
| `jvm.memory.heap.used` | `jvm_heap_used` | B | JVM heap memory currently used by the Solr process |
| `jvm.threads.count` | `jvm_thread_count` | # | Number of live threads in the Solr JVM |
| `solr.requests.count` | `solr_requests_count` | # | Cumulative number of requests handled by QUERY handlers |
| `solr.requests.time` | `solr_requests_time` | ms | Cumulative time spent handling QUERY requests (meanMs * count, ms) |
| `solr.errors.count` | `solr_errors_count` | # | Cumulative number of errors across all QUERY handlers |
| `solr.cache.hits` | `solr_cache_hits` | # | Cumulative query result cache hits |
| `solr.cache.inserts` | `solr_cache_inserts` | # | Cumulative query result cache inserts |
| `solr.document.count` | `solr_doc_count_{core}` | # | Number of indexed documents in the core |
| `solr.index.size` | `solr_index_size_{core}` | B | On-disk index size in bytes for the core |

<!-- schema:metrics:end -->
