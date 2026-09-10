<img src="https://api.iconify.design/mdi/message-processing.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache ActiveMQ

The `activemq` probe monitors an Apache ActiveMQ broker via Jolokia HTTP REST,
reporting broker-level resource usage (memory, store, temp) and per-destination
(queue/topic) message throughput counters.

## Quick start

```yaml
# probes.d/10-activemq.yaml — each file under probes.d/ is a YAML array of probes
- name: activemq
  type: activemq
  params:
    jolokia_url: http://localhost:8161/api/jolokia
    username: admin
    password: ${secret:activemq.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `jolokia_url` | No | `http://localhost:8161/api/jolokia` | Jolokia REST endpoint of the broker. Example: `http://broker.example.com:8161/api/jolokia` |
| `broker_name` | No | `localhost` | Broker name the MBean queries are scoped to |
| `username` | No | `admin` | Basic-auth user; empty sends no credentials |
| `password` | No | `admin` | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `queue_filter` | No | - | Glob patterns of the destinations to report; empty reports every queue and topic. Example: `orders.*` |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this broker; set it when the broker is reachable under several names |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `jolokia_url` | `http://localhost:8161/api/jolokia` | Jolokia REST endpoint on the ActiveMQ broker |
| `username` | `admin` | Basic-auth username |
| `password` | `admin` | Basic-auth password — reference via `${secret:activemq.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |
| `broker_name` | `localhost` | Broker name used to scope MBean queries |
| `queue_filter` | all queues | Only report these destinations, by exact name. A broker with hundreds of short-lived queues otherwise emits a series per queue |
| `instance_name` | derived | Stable identity override for this broker, so the entity does not split when the broker is reachable under several names |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.activemq.up` | 1 | 1 when the Jolokia endpoint is reachable, 0 otherwise |
| `activemq.producer.count` | {producer} | Total producers connected to the broker |
| `activemq.consumer.count` | {consumer} | Total consumers connected to the broker |
| `activemq.message.current` | {message} | Messages currently enqueued across all destinations |
| `activemq.memory.usage` | % | Broker memory utilization (percentage of configured limit) |
| `activemq.store.usage` | % | Persistent store utilization (percentage of configured limit) |
| `activemq.destination.producer.count` | {producer} | Producers per destination (queue/topic) |
| `activemq.destination.consumer.count` | {consumer} | Consumers per destination |
| `activemq.destination.messages.enqueued` | {message} | Messages enqueued per destination (cumulative) |
| `activemq.destination.messages.dequeued` | {message} | Messages dequeued per destination (cumulative) |

## Operational notes

- Jolokia must be installed and enabled on the broker. The classic ActiveMQ distribution ships Jolokia at `/api/jolokia` by default; broker-only installs without the web console may require separate Jolokia configuration.
- Per-destination metrics tag on `destination` + `destination_type` (queue or topic). A broker with many destinations generates a large number of PRTG channels — consider restricting with `broker_name` if monitoring a specific broker in a network-of-brokers setup.
