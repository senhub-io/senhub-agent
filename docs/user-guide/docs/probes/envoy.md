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

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.envoy.up` | `envoy_up` | # | 1 when the Envoy admin interface answered /stats?format=prometheus successfully |
| `envoy.server.uptime` | `envoy_server_uptime` | s | Time since the Envoy process started (seconds) |
| `envoy.server.memory.allocated` | `envoy_server_memory_allocated` | B | Current memory allocated by the Envoy process |
| `envoy.server.memory.heap_size` | `envoy_server_memory_heap_size` | B | Current heap size reported by the Envoy process |
| `envoy.listener.downstream.connections.total` | `envoy_listener_downstream_cx_total` | # | Total downstream connections accepted across all listeners (cumulative) |
| `envoy.listener.downstream.connections.active` | `envoy_listener_downstream_cx_active` | # | Currently active downstream connections across all listeners |
| `envoy.http.downstream.requests.total` | `envoy_http_downstream_rq_total` | # | Total HTTP downstream requests received across all HTTP connection managers (cumulative) |
| `envoy.cluster.upstream.connections.total` | `envoy_cluster_{cluster}_upstream_cx_total` | # | Total upstream connections opened to cluster members (cumulative) |
| `envoy.cluster.upstream.requests.total` | `envoy_cluster_{cluster}_upstream_rq_total` | # | Total upstream requests dispatched to cluster members (cumulative) |
| `envoy.cluster.upstream.requests.time` | `envoy_cluster_{cluster}_upstream_rq_time_sum` | ms | Cumulative upstream request latency across all requests to cluster members |

<!-- schema:metrics:end -->
