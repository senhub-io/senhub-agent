<img src="../../assets/probe-logos/activemq.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

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

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `jolokia_url` | In practice | `http://localhost:8161/api/jolokia` | Jolokia REST endpoint of the broker. Example: `http://broker.example.com:8161/api/jolokia` |
| `broker_name` | No | `localhost` | Broker name the MBean queries are scoped to |
| `username` | In practice | `admin` | Basic-auth user; empty sends no credentials |
| `password` | In practice | `admin` | Basic-auth password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `queue_filter` | No | - | Glob patterns of the destinations to report; empty reports every queue and topic. Example: `orders.*` |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this broker; set it when the broker is reachable under several names |

<!-- schema:params:end -->

`queue_filter` takes shell-style globs (`orders.*`, `*.dlq`), matched against
the destination name; a destination matching any pattern is reported. A broker
with hundreds of short-lived queues otherwise emits a series per queue.

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
| `activemq.message.enqueued` | {message} | Messages enqueued per destination (cumulative) |
| `activemq.message.dequeued` | {message} | Messages dequeued per destination (cumulative) |

## Operational notes

- Jolokia must be installed and enabled on the broker. The classic ActiveMQ distribution ships Jolokia at `/api/jolokia` by default; broker-only installs without the web console may require separate Jolokia configuration.
- Per-destination metrics tag on `destination` + `destination_type` (queue or topic). A broker with many destinations generates a large number of PRTG channels; restrict them with `queue_filter`. In a network-of-brokers setup, `broker_name` selects which broker the queries are scoped to.

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
| `senhub.activemq.up` | `senhub.activemq.up` | ActiveMQ Broker Up | # | 1 when the Jolokia endpoint responded successfully, 0 otherwise |
| `activemq.producer.count` | `activemq.producer.count` | ActiveMQ Producers | # | Total number of message producers connected to the broker |
| `activemq.consumer.count` | `activemq.consumer.count` | ActiveMQ Consumers | # | Total number of message consumers connected to the broker |
| `activemq.message.current` | `activemq.message.current` | ActiveMQ Messages In Flight | # | Total number of messages currently held in all destinations |
| `activemq.memory.usage` | `activemq.memory.usage` | ActiveMQ Memory Usage | % | Broker JVM memory usage as a percentage of the configured memory limit (0–100) |
| `activemq.store.usage` | `activemq.store.usage` | ActiveMQ Store Usage | % | Persistent message store usage as a percentage of the configured store limit (0–100) |
| `activemq.temp.usage` | `activemq.temp.usage` | ActiveMQ Temp Usage | % | Temporary storage usage as a percentage of the configured temp limit (0–100) |
| `activemq.message.enqueued` | `activemq.message.enqueued` | ActiveMQ {destination_type} {destination} Enqueued | # | Cumulative number of messages enqueued since broker start |
| `activemq.message.dequeued` | `activemq.message.dequeued` | ActiveMQ {destination_type} {destination} Dequeued | # | Cumulative number of messages dequeued (consumed) since broker start |
| `activemq.message.queue_size` | `activemq.message.queue_size` | ActiveMQ {destination_type} {destination} Queue Size | # | Number of messages currently waiting in the destination |
| `activemq.destination.consumer.count` | `activemq.destination.consumer.count` | ActiveMQ {destination_type} {destination} Consumers | # | Number of consumers currently subscribed to the destination |
| `activemq.destination.producer.count` | `activemq.destination.producer.count` | ActiveMQ {destination_type} {destination} Producers | # | Number of producers currently attached to the destination |

<!-- schema:metrics:end -->
