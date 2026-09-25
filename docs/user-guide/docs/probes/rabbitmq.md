<img src="../../assets/probe-logos/rabbitmq.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:15672` | Base URL of the Management API. Example: `http://rabbit.example.com:15672` |
| `username` | In practice | `guest` | Management user |
| `password` | In practice | `guest` | Management user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity of this broker instead of the one derived from the endpoint |

<!-- schema:params:end -->

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
| `rabbitmq.consumers.total` | {consumer} | Total consumers connected to the broker |
| `rabbitmq.connections.total` | {connection} | Total client connections |
| `rabbitmq.node.memory.used` | By | Memory used by the broker process, tagged with `node` |
| `rabbitmq.node.disk.free` | By | Free disk space on the node |
| `rabbitmq.node.fd.used` | {fd} | Open file descriptors on the node |

## Operational notes

- The `rabbitmq_management` plugin must be enabled: `rabbitmq-plugins enable rabbitmq_management`.
- The default `guest` account is restricted to localhost connections. For remote monitoring, create a dedicated management user with the `monitoring` tag.
- Metric names align with the OpenTelemetry Collector contrib `rabbitmqreceiver` where equivalents exist.

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
| `senhub.rabbitmq.up` | `senhub.rabbitmq.up` | RabbitMQ Up | # | 1 when the Management API answered successfully, 0 otherwise |
| `rabbitmq.messages.published` | `rabbitmq.messages.published` | RabbitMQ Messages Published | # | Total messages published to the broker (message_stats.publish) |
| `rabbitmq.messages.delivered` | `rabbitmq.messages.delivered` | RabbitMQ Messages Delivered | # | Total messages delivered to consumers (message_stats.deliver_get) |
| `rabbitmq.messages.acknowledged` | `rabbitmq.messages.acknowledged` | RabbitMQ Messages Acknowledged | # | Total messages acknowledged by consumers (message_stats.ack) |
| `rabbitmq.messages.unacknowledged` | `rabbitmq.messages.unacknowledged` | RabbitMQ Messages Unacknowledged | # | Current number of messages delivered but not yet acknowledged (queue_totals.messages_unacknowledged) |
| `rabbitmq.messages.ready` | `rabbitmq.messages.ready` | RabbitMQ Messages Ready | # | Current number of messages ready for delivery (queue_totals.messages_ready) |
| `rabbitmq.consumers.total` | `rabbitmq.consumers.total` | RabbitMQ Consumers | # | Total number of consumers across all queues (object_totals.consumers) |
| `rabbitmq.queues.total` | `rabbitmq.queues.total` | RabbitMQ Queues | # | Total number of queues (object_totals.queues) |
| `rabbitmq.connections.total` | `rabbitmq.connections.total` | RabbitMQ Connections | # | Total number of open connections (object_totals.connections) |
| `rabbitmq.channels.total` | `rabbitmq.channels.total` | RabbitMQ Channels | # | Total number of open channels (object_totals.channels) |
| `rabbitmq.node.memory.used` | `rabbitmq.node.memory.used` | RabbitMQ {node} Memory Used | bytes | Bytes of RAM used by the Erlang VM on this node (mem_used) |
| `rabbitmq.node.disk.free` | `rabbitmq.node.disk.free` | RabbitMQ {node} Disk Free | bytes | Bytes of free disk space on the node's data partition (disk_free) |
| `rabbitmq.node.fd.used` | `rabbitmq.node.fd.used` | RabbitMQ {node} File Descriptors Used | # | Number of file descriptors in use by the node (fd_used) |
| `rabbitmq.node.sockets.used` | `rabbitmq.node.sockets.used` | RabbitMQ {node} Sockets Used | # | Number of sockets in use by the node (sockets_used) |
| `rabbitmq.node.running` | `rabbitmq.node.running` | RabbitMQ {node} Running | # | 1 when the node reports running=true, 0 otherwise |
| `rabbitmq.node.uptime` | `rabbitmq.node.uptime` | RabbitMQ {node} Uptime | ms | Time in milliseconds since the Erlang VM on this node started (uptime) |
| `rabbitmq.queue.messages.ready` | `rabbitmq.queue.messages.ready` | RabbitMQ {vhost}/{queue} Messages Ready | # | Messages ready for delivery in this queue (messages_ready) |
| `rabbitmq.queue.messages.unacknowledged` | `rabbitmq.queue.messages.unacknowledged` | RabbitMQ {vhost}/{queue} Messages Unacknowledged | # | Messages delivered to consumers but not yet acknowledged in this queue (messages_unacknowledged) |
| `rabbitmq.queue.consumers` | `rabbitmq.queue.consumers` | RabbitMQ {vhost}/{queue} Consumers | # | Number of consumers subscribed to this queue (consumers) |

<!-- schema:metrics:end -->
