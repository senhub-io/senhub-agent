<img src="https://cdn.simpleicons.org/apachekafka" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Apache Kafka

The `kafka` probe monitors Kafka brokers, topics, partitions and consumer
groups via the Admin API, covering broker count, topic and partition metadata,
current and oldest offsets, ISR replica counts, and consumer group lag.
Metric parity with the OpenTelemetry Collector contrib `kafkametricsreceiver`.

## Quick start

```yaml
# probes.d/10-kafka.yaml — each file under probes.d/ is a YAML array of probes
- name: kafka
  type: kafka
  params:
    brokers:
      - localhost:9092
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `brokers` | No | `[localhost:9092]` | Bootstrap brokers as host:port entries. Example: `kafka-1.example.com:9092` |
| `protocol_version` | No | `2.0.0` | Kafka protocol version negotiated with the brokers |
| `tls` | No | `false` | Connect to the brokers over TLS |
| `sasl_mechanism` | No | - | SASL mechanism; empty connects without authentication. One of `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` |
| `sasl_username` | No | - | SASL user, required with sasl_mechanism |
| `sasl_password` | No | - | SASL password, required with sasl_mechanism. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `topic_filter` | No | - | Glob patterns of the topics to monitor; empty monitors every non-internal topic. Example: `orders-*` |
| `group_filter` | No | - | Glob patterns of the consumer groups to monitor; empty monitors every group |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Broker request timeout in seconds |
| `instance_name` | No | - | Stable identity of this cluster instead of the cluster id it reports |

<!-- schema:params:end -->

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
