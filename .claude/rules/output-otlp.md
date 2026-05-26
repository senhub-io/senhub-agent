---
title: OTLP output (gRPC or HTTP push)
paths:
  - internal/agent/services/data_store/strategies/otlp/**
---

## What this is

The `otlp` strategy pushes signals via OpenTelemetry Protocol to a configured collector or a compatible backend. Three signals supported:

- **metrics** — primary use case (probe data → OTLP MetricsService)
- **logs** — agent + probe logs (when enabled)
- **traces** — Phase 3 / future

## Transport: `protocol: grpc | http`

The `protocol` config key selects the OTLP transport for all three signals:

- `grpc` (default) — OTLP/gRPC. Talk to an `otelcol` or any gRPC OTLP receiver.
- `http` — OTLP/HTTP protobuf. This is what VictoriaMetrics / VictoriaLogs / VictoriaTraces accept on their **native** `/opentelemetry/...` ingestion endpoints, so `http` is the transport to use when pushing straight to a Victoria backend (no otelcol hop).

`protocol` is strategy-wide — there is no per-signal transport override (mixing gRPC and HTTP against one endpoint is almost always a config mistake, not an intent). The exporter wiring lives in `client.go`: `buildMetricExporter` / `buildLogExporter` / `buildTraceExporter` each branch to a `*GRPC` or `*HTTP` builder. The `exporters` struct holds interface types (`sdkmetric.Exporter`, `sdklog.Exporter`) so the same struct carries either transport.

With `http`, the OTLP/HTTP exporters use the standard signal paths (`/v1/metrics`, `/v1/logs`, `/v1/traces`) appended to `endpoint`. When fronting a Victoria backend behind nginx, nginx rewrites those standard paths to the Victoria-native ones.

Per-signal transport overrides for endpoint/headers/TLS still exist (`signals.metrics.endpoint`, `signals.logs.endpoint`) so an operator can split signal targets — useful with `http` when each signal lands in a different Victoria service.

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
protocol: grpc          # grpc (default) | http
endpoint: "otlp.example.com:4317"
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
