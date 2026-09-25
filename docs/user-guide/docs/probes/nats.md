<img src="../../assets/probe-logos/nats.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8222` | Base URL of the NATS monitoring HTTP API. Example: `http://nats.example.com:8222` |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this server instead of the server id it reports |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nats.up` | 1 | 1 when `/varz` responds with HTTP 200 |
| `nats.connections.count` | {connection} | Current active client connections |
| `nats.connections.total` | {connection} | Total connections since server start |
| `nats.subscriptions.count` | {subscription} | Active subscriptions |
| `nats.messages.in` | {message} | Messages received since start |
| `nats.messages.out` | {message} | Messages sent to subscribers since start |
| `nats.bytes.in` | By | Bytes received since start |
| `nats.bytes.out` | By | Bytes sent since start |
| `nats.slow_consumers` | {consumer} | Connections flagged as slow consumers |
| `nats.routes.count` | {connection} | Active cluster route connections |
| `nats.jetstream.streams` | {stream} | JetStream stream count |
| `nats.jetstream.consumers` | {consumer} | JetStream consumer count |
| `nats.jetstream.messages` | {message} | Messages stored in JetStream |
| `nats.jetstream.storage` | By | Bytes stored in JetStream |

## Operational notes

- The management HTTP API is separate from the NATS client port (4222). Ensure it is enabled in the NATS server configuration: `http: 8222` or `http_port: 8222`.
- JetStream metrics are only emitted when JetStream is enabled on the server.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.nats.up` | `nats_up` | # | 1 when the NATS management API (/varz) responds with HTTP 200 |
| `nats.connections.count` | `nats_connections_count` | # | Number of currently active client connections |
| `nats.connections.total` | `nats_connections_total` | # | Total number of client connections since server start (monotonic counter) |
| `nats.subscriptions.count` | `nats_subscriptions_count` | # | Number of active subscriptions across all clients |
| `nats.messages.in` | `nats_messages_in` | # | Total number of messages received by the server since start |
| `nats.messages.out` | `nats_messages_out` | # | Total number of messages sent by the server since start |
| `nats.bytes.in` | `nats_bytes_in` | B | Total bytes received by the server since start |
| `nats.bytes.out` | `nats_bytes_out` | B | Total bytes sent by the server since start |
| `nats.slow_consumers` | `nats_slow_consumers` | # | Number of slow consumer events detected since server start |
| `nats.routes.count` | `nats_routes_count` | # | Number of active cluster routes (server-to-server connections) |
| `nats.jetstream.streams` | `nats_jetstream_streams` | # | Number of JetStream streams (present only when JetStream is enabled) |
| `nats.jetstream.consumers` | `nats_jetstream_consumers` | # | Number of JetStream consumers across all streams |
| `nats.jetstream.messages` | `nats_jetstream_messages` | # | Total number of messages stored in JetStream across all streams |
| `nats.jetstream.storage` | `nats_jetstream_storage` | B | Total bytes stored in JetStream across all streams |

<!-- schema:metrics:end -->
