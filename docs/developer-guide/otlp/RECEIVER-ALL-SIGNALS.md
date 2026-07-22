# OTLP receiver — full signal coverage (metrics, logs, traces)

Status: design / in progress. Epic: senhub-io/senhub-agent#655.

## Goal

Turn the `otlp_receiver` probe into a complete OTLP receiver that ingests
**all three OTLP signal types** — metrics, logs and traces — over the
standard OTLP transports (gRPC on 4317, HTTP/protobuf on 4318 with the
`/v1/{metrics,logs,traces}` paths). Today it ingests **metrics only**, and
only the scalar metric families (Gauge, Sum); histograms and summaries are
dropped via OTLP partial-success.

## Why this is not one change

The agent's internal fan-out is `[]datapoint.DataPoint` shaped — a single
float `Value` plus tags. That model fits **metrics** naturally. Logs and
traces have no scalar value and cannot flow through the `DataPoint`
callback that every probe (including this receiver) uses today. The
signal types therefore split into three increments of very different size
and risk, tracked as separate issues under the epic.

The receiver is only useful for logs/traces when paired with an **OTLP
export strategy** (`strategies/otlp/`): the pull sinks (Prometheus,
PRTG, Nagios, Zabbix) are metrics-only, so ingested logs/traces have
nowhere to go except back out over OTLP (an OTLP-in → OTLP-out relay).
This is stated in config validation, not silently no-op'd.

## Architecture

### Transport (shared by all signals)

One listener per probe instance, transport chosen by `protocol` (`grpc`
or `http`), exactly as today. The change is that the listener serves
**every enabled signal** rather than only metrics:

- gRPC: register `MetricsService`, `LogsService`, `TracesService` on the
  one `grpc.Server`, all behind the existing `guardInterceptor`
  (bearer / CIDR / rate-limit).
- HTTP: route `/v1/metrics`, `/v1/logs`, `/v1/traces` on the one mux,
  all behind the existing ingress guard, `application/x-protobuf` only.

Which signals are accepted is config-driven (`signals:`), defaulting to
`[metrics]` so existing installs are unchanged.

### Metrics (Phase 1) — `DataPoint` expansion

Extend `decode.go` to map the non-scalar metric families onto scalar
component series, the Prometheus classic-histogram representation, so
they flow through the existing `DataPoint` path:

- Histogram → `<name>_count`, `<name>_sum`, and cumulative
  `<name>_bucket{le="<bound>"}` series (plus `le="+Inf"`); optional
  `<name>_min` / `<name>_max` gauges when present.
- Summary → `<name>_count`, `<name>_sum`, and `<name>{quantile="q"}`
  gauges.
- ExponentialHistogram → `<name>_count`, `<name>_sum`, `<name>_min`,
  `<name>_max` scalars; the base-2 bucket expansion is a follow-up
  (exponential buckets do not map to a fixed `le` set cleanly).

Limitation (tracked): scalar expansion means an OTLP-in histogram is
re-exported as its **component scalar series**, not as a native OTLP
histogram. Native histogram pass-through needs the `DataPoint` /
`OtelRecord` model to carry a histogram payload end-to-end (mapper + both
serializers) — a separate, larger change, tracked in the epic.

### Logs (Phase 2) — relay through the agent log channel

The OTLP export strategy already drains an internal
`agentstate.LogRecord` channel (`strategies/otlp/logs.go`, `logsPump`).
Reuse it: `LogsService.Export` converts each OTLP `LogRecord` into an
`agentstate.LogRecord` and publishes it to that channel, so ingested logs
ride the existing export pipeline (batching, dead-letter queue, replay)
out over OTLP. No new export machinery.

Gating: logs ingest is a no-op with a startup warning when no OTLP export
strategy is configured (nowhere to relay to).

### Traces (Phase 3) — raw span forwarder

Traces have no internal representation at all; the export side
(`strategies/otlp/traces.go`) uses the SDK `BatchSpanProcessor` fed by the
agent's own tracer, which does not accept externally-received proto spans.
Phase 3 adds a dedicated span forwarder: `TracesService.Export` hands the
received `ResourceSpans` to an OTLP trace exporter that forwards them
unchanged (OTLP-in → OTLP-out), with the same batching/retry contract as
the metrics/logs exporters. Largest and last increment.

## Cross-cutting decisions

- **Cache keying (no collapse)**: an ingested metric's label set is
  arbitrary and externally controlled, so a fixed `DiscriminantTagsRegistry`
  list cannot enumerate it. The HTTP `MetricCache` therefore keys
  `otlp_receiver` (and `prometheus_scrape`) on their **full tag set**
  (`fullTagKeyProbes` in `http_cache.go`), identical to the OTLP strategy's
  own store — so every distinct series (each histogram `le` bucket, each
  `quantile`, each resource-attribute split) survives on the PRTG / Nagios /
  Prometheus pull sinks, not just on the OTLP relay. The cache cardinality
  cap bounds the memory an external producer can drive. PRTG/Nagios still
  render these generically (raw humanized name, generic unit, no lookup or
  threshold) — the agent cannot infer semantics for a metric it did not
  define — but nothing is silently dropped.
- **Licensing**: `otlp_receiver` stays **free tier** across all three
  signals. It is an onboarding/wedge capability (accept OTLP from anything
  at the edge); gating signals behind Pro would blunt that. Revisit only
  if abuse/scale data says otherwise.
- **Config back-compat**: absent `signals:` ⇒ `[metrics]`. Existing
  single-path HTTP (`http_path`) keeps working for metrics; logs/traces
  use the fixed `/v1/logs` and `/v1/traces` paths.
- **Guard/limits**: all signals reuse the one `ingressGuard` and
  `maxRecvMsgBytes`. No per-signal auth.
- **Self-metrics**: add ingest counters per signal
  (`senhub.agent.otlp_receiver.ingested{signal=metrics|logs|traces}` and a
  matching `rejected`/`dropped`) so operators can see the relay working.

## Phases / tracking

| Phase | Scope | Issue |
|---|---|---|
| 1 | Complete metrics: histogram / exp-histogram / summary → scalar series | #656 |
| 2 | Multi-signal transport + logs ingest (relay via agent log channel) | #657 |
| 3 | Traces ingest (raw span forwarder) | #658 |
| — | Native OTLP histogram pass-through (model carries histogram) | #659 |

Each phase ships as its own PR with tests. Phase 1 is self-contained and
does not touch the transport; Phases 2–3 add the multi-service transport.
