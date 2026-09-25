<img src="../../assets/probe-logos/pulsar.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache Pulsar

The `pulsar` probe monitors an Apache Pulsar broker via its Admin REST API
(`/admin/v2/brokers/ready`) and Prometheus metrics endpoint (`/metrics`),
reporting broker health, throughput, storage and backlog at the broker level.

## Quick start

```yaml
# probes.d/10-pulsar.yaml — each file under probes.d/ is a YAML array of probes
- name: pulsar
  type: pulsar
  params:
    endpoint: http://localhost:8080
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:451fa4adfe10c1ad67cf93341b14f8f36f42cc73840edb829969df3cb45e38a0 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8080` | Base URL of the broker admin and metrics HTTP service. Example: `http://pulsar.example.com:8080` |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `60` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this broker instead of the one derived from the endpoint |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.pulsar.up` | 1 | 1 when the broker answered `/admin/v2/brokers/ready` with HTTP 200 |
| `pulsar.topics.count` | {topic} | Number of topics on the broker |
| `pulsar.rate.messages.in` | {message}/s | Incoming message rate (broker-level aggregate) |
| `pulsar.rate.messages.out` | {message}/s | Outgoing message rate |
| `pulsar.throughput.in` | By/s | Incoming byte throughput |
| `pulsar.throughput.out` | By/s | Outgoing byte throughput |
| `pulsar.storage.size` | By | Broker-level storage used for ledger data |
| `pulsar.message.backlog` | {message} | Total message backlog across all topics |
| `pulsar.producers.count` | {producer} | Connected producers |
| `pulsar.consumers.count` | {consumer} | Connected consumers |

## Operational notes

- The Pulsar admin port (default 8080) and metrics endpoint are on the same port.
- For TLS-secured brokers, use `https://` as the endpoint prefix.
- The probe scrapes broker-level aggregates from `/metrics` (Prometheus text); per-topic and per-namespace metrics require additional per-endpoint scrapes and are not covered in this tier.

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
| `senhub.pulsar.up` | `senhub.pulsar.up` | Pulsar {endpoint} Up | # | 1 when the broker answered /admin/v2/brokers/ready with HTTP 200 |
| `pulsar.topics.count` | `pulsar.topics.count` | Pulsar Topics | # | Number of topics on the broker |
| `pulsar.producers.count` | `pulsar.producers.count` | Pulsar Producers | # | Number of producers connected to the broker |
| `pulsar.consumers.count` | `pulsar.consumers.count` | Pulsar Consumers | # | Number of consumers connected to the broker |
| `pulsar.rate.messages.in` | `pulsar.rate.messages.in` | Pulsar Publish Rate | msgs/s | Messages published per second to the broker |
| `pulsar.rate.messages.out` | `pulsar.rate.messages.out` | Pulsar Dispatch Rate | msgs/s | Messages dispatched per second from the broker |
| `pulsar.throughput.in` | `pulsar.throughput.in` | Pulsar Publish Throughput | Bytes/s | Bytes published per second to the broker |
| `pulsar.throughput.out` | `pulsar.throughput.out` | Pulsar Dispatch Throughput | Bytes/s | Bytes dispatched per second from the broker |
| `pulsar.storage.size` | `pulsar.storage.size` | Pulsar Storage Size | B | Total storage used by the broker for message data |
| `pulsar.message.backlog` | `pulsar.message.backlog` | Pulsar Message Backlog | # | Number of messages in the backlog (not yet consumed) |
| `pulsar.storage.read.rate` | `pulsar.storage.read.rate` | Pulsar Storage Read Rate | entries/s | Storage ledger entries read per second |
| `pulsar.storage.write.rate` | `pulsar.storage.write.rate` | Pulsar Storage Write Rate | entries/s | Storage ledger entries written per second |

<!-- schema:metrics:end -->
