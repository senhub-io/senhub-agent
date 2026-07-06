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

| Parameter | Default | Description |
|---|---|---|
| `jolokia_url` | `http://localhost:8161/api/jolokia` | Jolokia REST endpoint on the ActiveMQ broker |
| `username` | `admin` | Basic-auth username |
| `password` | `admin` | Basic-auth password — reference via `${secret:activemq.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |
| `broker_name` | `localhost` | Broker name used to scope MBean queries |

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
