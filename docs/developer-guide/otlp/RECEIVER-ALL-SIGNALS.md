# OTLP receiver — full signal coverage (metrics, logs, traces)

Status: phases 1–3 shipped; native histogram pass-through (#659) shipped.
Epic: senhub-io/senhub-agent#655.

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

### Metrics (Phase 1 + #659) — `DataPoint` expansion / native histograms

`decode.go` maps every metric family onto the `DataPoint` path:

- Histogram (explicit-bucket) → **native pass-through** (#659): ONE
  `DataPoint` whose `Histogram *datapoint.HistogramValue` payload carries
  count/sum/min/max/bucket_counts/explicit_bounds, threaded through the
  mapper (`CacheMetric.Histogram` / `OtelRecord.Histogram`), the OTLP
  store + checkpoint, and both serializers. The OTLP push emits a real
  `metricdata.Histogram[float64]` (cumulative); the Prometheus exposition
  emits classic `_bucket{le}`/`_sum`/`_count` lines. `Value` holds the
  observation count so scalar-only sinks (PRTG, Nagios, cloud) render a
  meaningful number; a nil payload falls back to the scalar/gauge path
  everywhere.
- Summary → `<name>_count`, `<name>_sum`, and `<name>{quantile="q"}`
  gauges (scalar expansion, unchanged).
- ExponentialHistogram → `<name>_count`, `<name>_sum`, `<name>_min`,
  `<name>_max` scalars; the base-2 bucket expansion is a follow-up
  (exponential buckets do not map to a fixed `le` set cleanly), as is a
  possible native pass-through mirroring #659.

**Temporality**: the agent's OTLP exporter (and the last-writer-wins store)
are **cumulative-only** for every signal. A delta-temporality histogram (or
sum) is re-exported as cumulative, which is lossy for a delta sender — the
same long-standing contract the Sum path already has (`sumInstrumentType`
degrades a delta sum to a gauge). Faithful delta pass-through is a separate
follow-up. **Malformed histograms** (bucket-count length ≠ explicit-bounds
+ 1) are dropped at decode and surfaced via OTLP partial-success rather than
forwarded, so a poison point cannot make a downstream collector reject the
whole export batch. On the Prometheus side, a sender-supplied `le` attribute
on a histogram is stripped (it is reserved for the bucket ladder).

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

Traces have no internal representation at all; the export side's SDK
pipeline (`strategies/otlp/traces.go`) uses the `BatchSpanProcessor` fed
by the agent's own tracer, which does not accept externally-received
proto spans. Phase 3 therefore relays spans as **raw proto end-to-end**:
`TracesService.Export` (and HTTP `/v1/traces`) publishes the received
`ResourceSpans` verbatim on a dedicated agentstate span channel
(`agentstate/span_channel.go`, the batch-shaped analogue of the log
channel), and the OTLP export strategy's `spansRelay`
(`strategies/otlp/spans_relay.go`) subscribes and forwards each batch
unchanged over a raw collector `TracesService` client — gRPC or HTTP per
the strategy `protocol`, reusing the strategy's resolved
endpoint/TLS/headers/compression. Batching follows
`signals.traces.batch_size` / `batch_timeout` / `buffer_size`; a failed
export drops the batch (bounded per-call timeout, no SDK retry — the
relay is best-effort like the receive side).

The relay is gated by the export strategy's existing
`signals.traces.enabled` flag (no new config key) and runs independently
of the SDK pipeline — both are active when traces are enabled: the SDK
pipeline exports the agent's own spans, the relay forwards received ones.
With no trace-capable strategy subscribed, the receiver discards ingested
spans with a throttled warning (same contract as logs).

## Cross-cutting decisions

- **Cache keying (no collapse)**: an ingested metric's label set is
  arbitrary and externally controlled, so a fixed `DiscriminantTagsRegistry`
  list cannot enumerate it. The HTTP `MetricCache` therefore keys
  `otlp_receiver` (and `prometheus_scrape`) on their **full tag set**
  (`fullTagKeyProbes` in `http_cache.go`), identical to the OTLP strategy's
  own store — so every distinct series (each summary `quantile`, each
  resource-attribute split, each histogram series) survives on the PRTG / Nagios /
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

| Phase | Scope | Issue | Status |
|---|---|---|---|
| 1 | Complete metrics: histogram / exp-histogram / summary → scalar series | #656 | done |
| 2 | Multi-signal transport + logs ingest (relay via agent log channel) | #657 | done |
| 3 | Traces ingest (raw span forwarder) | #658 | done |
| — | Native OTLP histogram pass-through (model carries histogram) | #659 | done |

Each phase ships as its own PR with tests. Phase 1 is self-contained and
does not touch the transport; Phases 2–3 add the multi-service transport.
