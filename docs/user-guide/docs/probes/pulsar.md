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

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.pulsar.up` | `pulsar_up` | # | 1 when the broker answered /admin/v2/brokers/ready with HTTP 200 |
| `pulsar.topics.count` | `pulsar_topics_count` | # | Number of topics on the broker |
| `pulsar.producers.count` | `pulsar_producers_count` | # | Number of producers connected to the broker |
| `pulsar.consumers.count` | `pulsar_consumers_count` | # | Number of consumers connected to the broker |
| `pulsar.rate.messages.in` | `pulsar_rate_in` | msgs/s | Messages published per second to the broker |
| `pulsar.rate.messages.out` | `pulsar_rate_out` | msgs/s | Messages dispatched per second from the broker |
| `pulsar.throughput.in` | `pulsar_throughput_in` | Bytes/s | Bytes published per second to the broker |
| `pulsar.throughput.out` | `pulsar_throughput_out` | Bytes/s | Bytes dispatched per second from the broker |
| `pulsar.storage.size` | `pulsar_storage_size` | B | Total storage used by the broker for message data |
| `pulsar.message.backlog` | `pulsar_msg_backlog` | # | Number of messages in the backlog (not yet consumed) |
| `pulsar.storage.read.rate` | `pulsar_storage_read_rate` | entries/s | Storage ledger entries read per second |
| `pulsar.storage.write.rate` | `pulsar_storage_write_rate` | entries/s | Storage ledger entries written per second |

<!-- schema:metrics:end -->
