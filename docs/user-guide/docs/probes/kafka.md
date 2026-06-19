!!! info
    **License: Free** — part of the universal collection tier.

# Apache Kafka

The `kafka` probe monitors Kafka brokers, topics, partitions and consumer
groups via the Admin API, covering broker count, topic and partition metadata,
current and oldest offsets, ISR replica counts, and consumer group lag.
Metric parity with the OpenTelemetry Collector contrib `kafkametricsreceiver`.

## Quick start

```yaml
probes:
  - name: kafka
    type: kafka
    params:
      brokers:
        - localhost:9092
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `brokers` | `[localhost:9092]` | Bootstrap broker list (`host:port` entries) |
| `tls` | `false` | Enable TLS for the broker connection |
| `sasl_mechanism` | — | SASL authentication: `PLAIN`, `SCRAM-SHA-256` or `SCRAM-SHA-512` |
| `sasl_username` | — | SASL username |
| `sasl_password` | — | SASL password |
| `protocol_version` | `2.0.0` | Kafka protocol version to negotiate |
| `topic_filter` | all | Glob patterns to restrict which topics are monitored (internal topics starting with `__` are always excluded) |
| `group_filter` | all | Glob patterns to restrict which consumer groups are monitored |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.kafka.up` | 1 | 1 when the last collection cycle reached the Kafka cluster |
| `kafka.brokers` | {broker} | Number of brokers in the cluster |
| `kafka.topic.partitions` | {partition} | Partition count per topic, tagged with `topic` |
| `kafka.partition.current_offset` | {offset} | Current (high-water mark) offset, tagged with `topic`/`partition` |
| `kafka.partition.oldest_offset` | {offset} | Oldest available offset per partition |
| `kafka.partition.replicas` | {replica} | Total replicas per partition |
| `kafka.partition.replicas.in_sync` | {replica} | In-sync replicas per partition |
| `kafka.consumer_group.lag` | {message} | Lag per group/topic/partition, tagged with `group`/`topic`/`partition` |
| `kafka.consumer_group.lag_sum` | {message} | Total lag summed across partitions per group/topic |

## Operational notes

- Internal topics (prefixed `__`) are excluded by default and cannot be included via `topic_filter`.
- For SASL/SCRAM authentication, use `sasl_mechanism: SCRAM-SHA-256` or `SCRAM-SHA-512` along with `sasl_username` and `sasl_password`.
