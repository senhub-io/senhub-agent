<img src="https://cdn.jsdelivr.net/gh/cncf/artwork@main/projects/envoy/icon/color/envoy-icon-color.svg" alt="" class="probe-page-logo probe-page-logo-wm">

!!! info
    **License: Free** — part of the universal collection tier.

# Envoy Proxy

The `envoy` probe monitors Envoy by scraping its admin interface
(`/stats?format=prometheus`), surfacing server health, listener downstream
connections and requests, and per-cluster upstream metrics.

## Quick start

```yaml
probes:
  - name: envoy
    type: envoy
    params:
      endpoint: http://localhost:9901
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:9901` | Envoy admin interface base URL |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.envoy.up` | 1 | 1 when the admin `/stats` endpoint responded successfully |
| `envoy.server.uptime` | s | Time since the Envoy process started |
| `envoy.server.memory.allocated` | By | Memory currently allocated by the Envoy process |
| `envoy.server.connections.active` | {connection} | Active downstream connections across all listeners |
| `envoy.listener.downstream.connections.active` | {connection} | Active downstream connections per listener |
| `envoy.cluster.upstream.requests.active` | {request} | Active upstream requests per cluster, tagged with `cluster` |
| `envoy.cluster.upstream.connections.active` | {connection} | Active upstream connections per cluster |

## Operational notes

- The Envoy admin interface is typically bound to `127.0.0.1:9901`. If the agent runs on the same host, the default endpoint works without changes.
- The admin interface should **not** be exposed to untrusted networks — it provides access to configuration and health state without authentication.
- Cluster-level metrics are tagged with `cluster` (derived from the `envoy_cluster_name` Prometheus label).
