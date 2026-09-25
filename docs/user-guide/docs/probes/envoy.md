<img src="../../assets/probe-logos/envoy.svg" alt="" class="probe-page-logo probe-page-logo-wm">

!!! info
    **License: Free** — part of the universal collection tier.

# Envoy Proxy

The `envoy` probe monitors Envoy by scraping its admin interface
(`/stats?format=prometheus`), surfacing server health, listener downstream
connections and requests, and per-cluster upstream metrics.

## Quick start

```yaml
# probes.d/10-envoy.yaml — each file under probes.d/ is a YAML array of probes
- name: envoy
  type: envoy
  params:
    endpoint: http://localhost:9901
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:cbaa2223c32e503bcf78b854bdcc473d53217b112e375bf527c6038714d5b8d8 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:9901` | Base URL of the admin interface |
| `interval` | No | `30` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this proxy; set it when two envoy probes run on one host |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.envoy.up` | 1 | 1 when the admin `/stats` endpoint responded successfully |
| `envoy.server.uptime` | s | Time since the Envoy process started |
| `envoy.server.memory.allocated` | By | Memory currently allocated by the Envoy process |
| `envoy.listener.downstream.connections.active` | # | Downstream connections currently active across all listeners |
| `envoy.listener.downstream.connections.active` | {connection} | Active downstream connections per listener |
| `envoy.cluster.upstream.requests.total` | # | Upstream requests dispatched to cluster members (cumulative) |
| `envoy.cluster.upstream.connections.total` | # | Upstream connections opened to cluster members (cumulative) |

## Operational notes

- The Envoy admin interface is typically bound to `127.0.0.1:9901`. If the agent runs on the same host, the default endpoint works without changes.
- The admin interface should **not** be exposed to untrusted networks — it provides access to configuration and health state without authentication.
- Cluster-level metrics are tagged with `cluster` (derived from the `envoy_cluster_name` Prometheus label).

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
| `senhub.envoy.up` | `senhub.envoy.up` | Envoy Up | # | 1 when the Envoy admin interface answered /stats?format=prometheus successfully |
| `envoy.server.uptime` | `envoy.server.uptime` | Envoy Server Uptime | s | Time since the Envoy process started (seconds) |
| `envoy.server.memory.allocated` | `envoy.server.memory.allocated` | Envoy Memory Allocated | B | Current memory allocated by the Envoy process |
| `envoy.server.memory.heap_size` | `envoy.server.memory.heap_size` | Envoy Memory Heap Size | B | Current heap size reported by the Envoy process |
| `envoy.listener.downstream.connections.total` | `envoy.listener.downstream.connections.total` | Envoy Downstream Connections Total | # | Total downstream connections accepted across all listeners (cumulative) |
| `envoy.listener.downstream.connections.active` | `envoy.listener.downstream.connections.active` | Envoy Downstream Connections Active | # | Currently active downstream connections across all listeners |
| `envoy.http.downstream.requests.total` | `envoy.http.downstream.requests.total` | Envoy HTTP Requests Total | # | Total HTTP downstream requests received across all HTTP connection managers (cumulative) |
| `envoy.cluster.upstream.connections.total` | `envoy.cluster.upstream.connections.total` | Envoy Cluster {cluster} Upstream Connections Total | # | Total upstream connections opened to cluster members (cumulative) |
| `envoy.cluster.upstream.requests.total` | `envoy.cluster.upstream.requests.total` | Envoy Cluster {cluster} Upstream Requests Total | # | Total upstream requests dispatched to cluster members (cumulative) |
| `envoy.cluster.upstream.requests.time` | `envoy.cluster.upstream.requests.time` | Envoy Cluster {cluster} Upstream Request Time | ms | Cumulative upstream request latency across all requests to cluster members |

<!-- schema:metrics:end -->
