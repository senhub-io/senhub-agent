!!! info
    **License: Free** — part of the universal collection tier.

# Apache Pulsar

The `pulsar` probe monitors an Apache Pulsar broker via its Admin REST API
(`/admin/v2/brokers/ready`) and Prometheus metrics endpoint (`/metrics`),
reporting broker health, throughput, storage and backlog at the broker level.

## Quick start

```yaml
probes:
  - name: pulsar
    type: pulsar
    params:
      endpoint: http://localhost:8080
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8080` | Pulsar broker admin/metrics base URL |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.pulsar.up` | 1 | 1 when the broker answered `/admin/v2/brokers/ready` with HTTP 200 |
| `pulsar.topics.count` | {topic} | Number of topics on the broker |
| `pulsar.messages.in_rate` | {message}/s | Incoming message rate (broker-level aggregate) |
| `pulsar.messages.out_rate` | {message}/s | Outgoing message rate |
| `pulsar.bytes.in_rate` | By/s | Incoming byte throughput |
| `pulsar.bytes.out_rate` | By/s | Outgoing byte throughput |
| `pulsar.storage.size` | By | Broker-level storage used for ledger data |
| `pulsar.backlog.size` | {message} | Total message backlog across all topics |
| `pulsar.producers.count` | {producer} | Connected producers |
| `pulsar.consumers.count` | {consumer} | Connected consumers |

## Operational notes

- The Pulsar admin port (default 8080) and metrics endpoint are on the same port.
- For TLS-secured brokers, use `https://` as the endpoint prefix.
- The probe scrapes broker-level aggregates from `/metrics` (Prometheus text); per-topic and per-namespace metrics require additional per-endpoint scrapes and are not covered in this tier.
