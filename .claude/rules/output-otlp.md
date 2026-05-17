---
title: OTLP output (gRPC push)
paths:
  - internal/agent/services/data_store/strategies/otlp/**
---

## What this is

The `otlp` strategy pushes signals via OpenTelemetry Protocol over gRPC to a configured collector. Three signals supported:

- **metrics** — primary use case (probe data → OTLP MetricsService)
- **logs** — agent + probe logs (when enabled)
- **traces** — Phase 3 / future

Per-signal transport overrides exist (`signals.metrics.endpoint`, `signals.logs.endpoint`) so an operator can split signal targets if their collector setup demands it.

## Mapper-side conformance

The OTLP exporter consumes from the **neutral `otelmapper`** (see `data-store` rule). It does NOT do any probe-specific translation — that's the mapper's job.

Concretely: when `Export(metrics)` runs each cycle, the strategy:

1. Pulls every cached probe datapoint via `otelmapper.Resolve(...)`.
2. Maps cache metric → `OtelRecord` (name, unit, type, attributes).
3. Translates to `pmetric.Metric` (proto).
4. Adds resource attributes from the strategy config (`service.name`, `service.version`, `host.name`, `deployment.environment`, `service.instance.id`).
5. Pushes via gRPC with retry/timeout from the OTel SDK.

## Resource vs metric attributes

| Type | Source | Lives on |
|---|---|---|
| Resource attributes | Strategy config `resource:` block + probe-emitted tags (via `IncludeProbeTags`) | The whole batch (one `Resource` per process) |
| Metric attributes | Probe-emitted tags + `tag_to_attribute` mappings + YAML static `otel.attributes` | Per datapoint |

Currently `IncludeProbeTags: true` is hardcoded for OTLP (see `strategy.go`), so every probe tag flows as a metric attribute. The OTel-canonical attrs `db.system.name`, `server.address`, `server.port` are emitted as metric attributes (not resource attrs) because the agent can host multiple probe instances and resource attrs are batch-level. Document this trade-off if a user asks why their dashboard groups by `db.system.name` show as labels rather than resource fields.

## Compression, TLS, retries

- Compression: `gzip` is the default; `none` is allowed for debugging.
- TLS: defaults to insecure for `127.0.0.1:4317` style targets; production collectors should use mTLS. The strategy supports `tls.enabled`, `tls.ca_file`, `tls.cert_file`, `tls.key_file`, `tls.skip_verify`.
- Retries: handled by the OTel SDK retry config (`retry.initial_interval`, `retry.max_interval`, `retry.max_elapsed_time`). Don't reimplement.

## Signal config

```yaml
signals:
  metrics:
    enabled: true
    interval: 10s        # push cadence
    endpoint: ""         # optional override (otherwise root endpoint)
  logs:
    enabled: false
    batch_size: 100
    batch_timeout: 2s
    buffer_size: 1024
  traces:
    enabled: false
    sample_ratio: 1.0
```

The interval is independent of probe `Collect` cadence — OTLP pulls the latest cache snapshot at its own rhythm.

## Common pitfalls

- **gRPC connect failures** show as `OTLP metrics export failed: context deadline exceeded` — usually firewall or wrong port. Use a local mock receiver (Python grpcio + opentelemetry-proto) for end-to-end validation.
- **Missing `db.system.name` on metric**: probe didn't emit the canonical resource attribute as a tag. Fix in the probe's `commonTags`, not here.
- **Empty payload**: cache is empty because the probe hasn't completed its first scrape yet. OTLP runs on its own timer; the first push after agent start may be empty.
