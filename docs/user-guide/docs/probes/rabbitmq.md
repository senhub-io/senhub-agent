<img src="https://cdn.simpleicons.org/rabbitmq" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# RabbitMQ

The `rabbitmq` probe monitors a RabbitMQ broker via its built-in HTTP
Management API, reporting queue depth, message throughput, per-node resource
usage, connection counts and exchange statistics.

## Quick start

```yaml
# probes.d/10-rabbitmq.yaml — each file under probes.d/ is a YAML array of probes
- name: rabbitmq
  type: rabbitmq
  params:
    endpoint: http://localhost:15672
    username: guest
    password: ${secret:rabbitmq.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:15672` | RabbitMQ Management API base URL |
| `username` | `guest` | RabbitMQ management user |
| `password` | `guest` | Management user password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.rabbitmq.up` | 1 | 1 when the Management API answered successfully |
| `rabbitmq.messages.published` | {message} | Total messages published to the broker |
| `rabbitmq.messages.delivered` | {message} | Total messages delivered to consumers |
| `rabbitmq.messages.acknowledged` | {message} | Total messages acknowledged |
| `rabbitmq.messages.unacknowledged` | {message} | Messages delivered but not yet acknowledged |
| `rabbitmq.messages.ready` | {message} | Messages ready to be delivered |
| `rabbitmq.queue.messages.ready` | {message} | Messages ready per queue, tagged with `queue` / `vhost` |
| `rabbitmq.queue.consumers` | {consumer} | Active consumers per queue |
| `rabbitmq.consumers.count` | {consumer} | Total consumers connected to the broker |
| `rabbitmq.connections.count` | {connection} | Total client connections |
| `rabbitmq.node.mem.used` | By | Memory used by the broker process, tagged with `node` |
| `rabbitmq.node.disk.free` | By | Free disk space on the node |
| `rabbitmq.node.fd.used` | {fd} | Open file descriptors on the node |

## Operational notes

- The `rabbitmq_management` plugin must be enabled: `rabbitmq-plugins enable rabbitmq_management`.
- The default `guest` account is restricted to localhost connections. For remote monitoring, create a dedicated management user with the `monitoring` tag.
- Metric names align with the OpenTelemetry Collector contrib `rabbitmqreceiver` where equivalents exist.
