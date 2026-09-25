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
