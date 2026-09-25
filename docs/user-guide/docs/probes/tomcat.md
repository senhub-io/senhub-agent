<img src="../../assets/probe-logos/tomcat.svg" alt="" class="probe-page-logo probe-page-logo-wm">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache Tomcat

The `tomcat` probe monitors Apache Tomcat via Jolokia HTTP REST, reporting
active HTTP sessions, request throughput, JVM heap and garbage collection,
and the Tomcat thread pool state.

## Quick start

```yaml
# probes.d/10-tomcat.yaml — each file under probes.d/ is a YAML array of probes
- name: tomcat
  type: tomcat
  params:
    jolokia_url: http://localhost:8080/jolokia
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `jolokia_url` | In practice | `http://localhost:8080/jolokia` | URL of the Jolokia agent deployed in Tomcat. Example: `http://tomcat.example.com:8080/jolokia` |
| `username` | No | - | Basic-auth user; empty sends no credentials |
| `password` | No | - | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this server instead of the one derived from the URL |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.tomcat.up` | 1 | 1 when Jolokia is reachable |
| `tomcat.sessions.active` | {session} | Active HTTP sessions per web application context, tagged with `context` |
| `tomcat.requests.total` | {request} | Requests processed per connector, tagged with `connector` |
| `tomcat.errors.total` | {error} | Request errors per connector |
| `tomcat.processing_time` | ms | Cumulative request processing time per connector |
| `tomcat.threads.current` | {thread} | Current thread pool size |
| `tomcat.threads.busy` | {thread} | Threads currently handling a request |
| `tomcat.threads.max` | {thread} | Maximum thread pool size |
| `jvm.memory.heap.used` | By | JVM heap in use |
| `jvm.memory.heap.max` | By | JVM maximum heap size |
| `jvm.gc.collections.count` | {collection} | GC collections by collector (Minor/Major), tagged with `collector` |

## Operational notes

- Jolokia must be deployed in Tomcat as a WAR or Java agent. The Jolokia WAR can be deployed at `/jolokia`; the Java agent is added to `CATALINA_OPTS`.
- Multiple Tomcat instances on the same host can be monitored with multiple probe entries pointing to different Jolokia URLs.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.tomcat.up` | `tomcat_up` | # | 1 when Jolokia is reachable, 0 otherwise |
| `tomcat.sessions.active` | `tomcat_sessions_{context}` | # | Number of active HTTP sessions for the web application context |
| `tomcat.requests.total` | `tomcat_requests_{connector}` | # | Total number of HTTP requests processed by the connector |
| `tomcat.bytes.received` | `tomcat_bytes_received_{connector}` | B | Total bytes received by the connector |
| `tomcat.bytes.sent` | `tomcat_bytes_sent_{connector}` | B | Total bytes sent by the connector |
| `tomcat.processing_time` | `tomcat_processing_time_{connector}` | ms | Cumulative request processing time in milliseconds |
| `tomcat.errors.total` | `tomcat_errors_{connector}` | # | Total number of HTTP errors produced by the connector |
| `tomcat.threads.current` | `tomcat_threads_current_{connector}` | # | Current number of threads in the connector thread pool |
| `tomcat.threads.busy` | `tomcat_threads_busy_{connector}` | # | Number of threads currently processing requests |
| `tomcat.threads.max` | `tomcat_threads_max_{connector}` | # | Maximum number of threads allowed in the connector thread pool |
| `jvm.memory.heap.used` | `jvm_heap_used` | B | Amount of heap memory currently used by the JVM |
| `jvm.memory.heap.committed` | `jvm_heap_committed` | B | Amount of heap memory committed (guaranteed available) to the JVM |
| `jvm.memory.heap.max` | `jvm_heap_max` | B | Maximum heap memory that can be used by the JVM |
| `jvm.gc.collections.count` | `jvm_gc_count_{collector}` | # | Number of garbage collection cycles performed by the collector |
| `jvm.gc.collections.elapsed` | `jvm_gc_elapsed_{collector}` | ms | Total time spent in garbage collection by the collector |
| `jvm.threads.count` | `jvm_threads_count` | # | Current number of live threads in the JVM |

<!-- schema:metrics:end -->
