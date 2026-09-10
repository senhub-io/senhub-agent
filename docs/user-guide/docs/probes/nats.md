<img src="https://api.iconify.design/logos/nats-icon.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# NATS Server

The `nats` probe monitors a NATS Server via its HTTP management API (`/varz`,
`/routez`, `/jsz`), reporting connections, subscriptions, message throughput,
cluster routes and JetStream stream and consumer health. No external
dependencies — uses the stdlib HTTP client.

## Quick start

```yaml
# probes.d/20-nats.yaml — each file under probes.d/ is a YAML array of probes
- name: nats
  type: nats
  params:
    endpoint: http://localhost:8222
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | No | `http://localhost:8222` | Base URL of the NATS monitoring HTTP API. Example: `http://nats.example.com:8222` |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this server instead of the server id it reports |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8222` | NATS management HTTP API base URL |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nats.up` | 1 | 1 when `/varz` responds with HTTP 200 |
| `nats.connections.count` | {connection} | Current active client connections |
| `nats.connections.total` | {connection} | Total connections since server start |
| `nats.subscriptions.count` | {subscription} | Active subscriptions |
| `nats.messages.received` | {message} | Messages received since start |
| `nats.messages.sent` | {message} | Messages sent to subscribers since start |
| `nats.bytes.received` | By | Bytes received since start |
| `nats.bytes.sent` | By | Bytes sent since start |
| `nats.slow_consumers` | {consumer} | Connections flagged as slow consumers |
| `nats.route.connections` | {connection} | Active cluster route connections |
| `nats.jetstream.streams` | {stream} | JetStream stream count |
| `nats.jetstream.consumers` | {consumer} | JetStream consumer count |
| `nats.jetstream.messages` | {message} | Messages stored in JetStream |
| `nats.jetstream.bytes` | By | Bytes stored in JetStream |

## Operational notes

- The management HTTP API is separate from the NATS client port (4222). Ensure it is enabled in the NATS server configuration: `http: 8222` or `http_port: 8222`.
- JetStream metrics are only emitted when JetStream is enabled on the server.
