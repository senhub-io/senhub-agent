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
<!-- sha256:2a6f64553c396df8dedf4bd230fa2f708adcdc892f21184e817c5d28d103b193 -->

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

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.solr.up` | `senhub.solr.up` | Solr Up | # | 1 when the Solr admin metrics endpoint responded successfully, 0 otherwise |
| `jvm.memory.heap.used` | `jvm.memory.heap.used` | JVM Heap Used | B | JVM heap memory currently used by the Solr process |
| `jvm.threads.count` | `jvm.threads.count` | JVM Thread Count | # | Number of live threads in the Solr JVM |
| `solr.requests.count` | `solr.requests.count` | Solr Requests | # | Cumulative number of requests handled by QUERY handlers |
| `solr.requests.time` | `solr.requests.time` | Solr Request Time | ms | Cumulative time spent handling QUERY requests (meanMs * count, ms) |
| `solr.errors.count` | `solr.errors.count` | Solr Errors | # | Cumulative number of errors across all QUERY handlers |
| `solr.cache.hits` | `solr.cache.hits` | Solr Cache Hits | # | Cumulative query result cache hits |
| `solr.cache.inserts` | `solr.cache.inserts` | Solr Cache Inserts | # | Cumulative query result cache inserts |
| `solr.document.count` | `solr.document.count` | Solr {core} Document Count | # | Number of indexed documents in the core |
| `solr.index.size` | `solr.index.size` | Solr {core} Index Size | B | On-disk index size in bytes for the core |

<!-- schema:metrics:end -->
