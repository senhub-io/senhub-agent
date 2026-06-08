# OTLP Native Export — Implementation Plan

**Status:** Phase 0 (planning), pending validation
**Branch:** `feat/otlp-export` (from `feat/prometheus-otel-mapping`)
**Author:** Matthieu Noirbusson
**Date:** 2026-05-04
**Companion:**
- [Prometheus IMPLEMENTATION-PLAN](../prometheus/IMPLEMENTATION-PLAN.md) — sibling exporter
- [SenHub Semantic Conventions](../otel/senhub-semantic-conventions.md) — shared OTel naming

## 0. Executive summary

This branch adds **native OTLP/gRPC export** of metrics and logs to SenHub
Agent. It is the second of multiple OTel-first sinks (Prometheus shipped
first as a pull endpoint; OTLP ships next as a push exporter).

**Crucial property:** the metric data emitted via OTLP carries the **same**
metric names, units, types, and attributes as the Prometheus `/metrics`
exposition. Operators who scrape Prometheus today and switch to OTLP
push tomorrow do **not** rewrite a single PromQL query — the names match
because both paths consume the same `OtelRecord` stream produced by the
neutral `otelmapper` package — see
`internal/agent/services/data_store/otelmapper/`.

**Scope:**
- OTLP/gRPC over `go.opentelemetry.io/otel/exporters/otlp/otlpmetricgrpc`
  and `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc`
- Metrics signal: cache-driven periodic push (default 30 s)
- Logs signal: live tap on `syslog` and `event` probe data streams,
  shipped as OTel `log.Record` per the OTel log data model + RFC 5424
  attributes mapping
- Strict adherence to the OTel specification — no SenHub-flavored
  deviations beyond what the spec already allows (e.g. extension
  attribute values that pre-existed under the `senhub.*` namespace)

**Out of scope (future work):**
- OS log file consumer probes (Linux journald, /var/log files;
  Windows Event Log, ETW) — these are *sources*, separate from this
  *sink* work
- OTLP/HTTP fallback (we go gRPC-only as decided)
- OTLP traces signal (no tracing data in the agent today)

## 1. Architecture

```
                                 ┌──────────────────────┐
                                 │ MetricCache          │
                                 │ (existing, unchanged)│
                                 └──────────┬───────────┘
   ┌──────┐    DataPoint                    │
   │probes│──────────────────────────► DataStore router ──┐
   └──────┘                                                │
                                                           ▼
                  ┌──────────┐    ┌──────┐   ┌────────┐   ┌──────────┐
                  │ senhub   │    │ prtg │   │ http   │   │  otlp    │   ◀── NEW
                  │ strategy │    │ strat│   │ strat  │   │ strategy │
                  └──────────┘    └──────┘   └────────┘   └────┬─────┘
                                                                │
                                                                ▼
                                                        ┌──────────────┐
                                                        │ OTLP/gRPC    │
                                                        │ exporter     │───►  collector / vmagent
                                                        └──────────────┘
                                                                ▲
                                                                │
                            log channel (new, lightweight pub/sub)
                                                                │
                                                          ┌─────┴──────┐
                                                          │ syslog +   │
                                                          │ event probes│
                                                          └─────────────┘
```

The **otlp strategy** is a parallel storage strategy alongside `senhub`,
`prtg`, `http`, etc. It does **not** replace any existing path — it adds a
new push sink that an operator opts into via `storage:`.

### 1.1 Metrics path

Re-uses the neutral `otelmapper` package — the same mapper that feeds
the Prometheus exposition. No sink-to-sink dependency: both
`prometheus/` and the new `otlp/` strategy import `otelmapper`, never
each other.

1. Periodically (`interval`, default 30 s), the otlp strategy reads all
   entries from `MetricCache.GetAllMetrics()`
2. Calls `otelmapper.Resolve(probeDef, cacheMetric, otelmapper.DefaultResolveOptions())`
   — identical resolution to the Prometheus path
3. Converts the resulting `[]otelmapper.OtelRecord` into the OTel SDK's
   `metricdata.ResourceMetrics` struct
4. Hands it to `otlpmetricgrpc.Exporter.Export()`

**Crucial detail:** counter cumulative semantics. The OTel data model
requires counters to carry a `start_time_unix_nano` (process start) and
report the cumulative value at `time_unix_nano`. We track the
process start time once and emit it on every counter point. Reset
detection on probe restart is left to the consumer (standard OTel
behavior).

### 1.2 Logs path

`syslog` and `event` probes today emit `DataPoint` instances that
`otelmapper.Resolve` currently `skip`s for both Prometheus output and
the OTLP metrics sink. We add a separate **log channel**:

```go
// internal/agent/services/agentstate/log_channel.go
type LogRecord struct {
    Timestamp time.Time
    Severity  log.Severity     // OTel severity per spec
    Body      string
    Attributes []attribute.KeyValue
}

func PublishLog(rec LogRecord)   // called by syslog/event probes
func SubscribeLogs(buf int) <-chan LogRecord  // consumed by otlp strategy
```

Probes call `agentstate.PublishLog(rec)` whenever they receive a syslog
message or HTTP event. The otlp strategy subscribes (with a bounded
buffered channel — drop-oldest on backpressure) and ships records via
`otlploggrpc.Exporter`.

OTel log mapping for syslog (per the OTel collector-contrib syslog
parser, which is the de-facto standard):
- `Body` = the syslog `MSG` field
- `SeverityNumber` = mapped from RFC 5424 PRI (0..7) to OTel Severity
  (TRACE..FATAL4) per the standard table
- `SeverityText` = OTel severity name
- Attributes: `syslog.facility`, `syslog.facility.name`,
  `syslog.hostname`, `syslog.appname`, `syslog.proc_id`, `syslog.msg_id`

For the `event` probe (custom HTTP-pushed events): each field of the
JSON payload becomes a log attribute under namespace `senhub.event.*`
(no other OTel convention applies — `event` is by definition custom).

Both probes will keep their PRTG/Nagios/senhub strategy outputs unchanged.

## 2. Configuration

```yaml
storage:
  - name: http
    params:
      endpoints: [prtg, web, prometheus]   # unchanged

  - name: otlp                              # NEW
    params:
      endpoint: "otlp.victoria.local:4317"  # gRPC default port
      headers:
        Authorization: "Bearer ${OTLP_TOKEN}"   # env-var substitution supported
      tls:
        enabled: true                       # default true; set false for plaintext localhost
        insecure_skip_verify: false
        ca_file: /etc/ssl/private/otlp-ca.pem
        cert_file: /etc/ssl/private/agent.pem
        key_file: /etc/ssl/private/agent.key
      compression: gzip                     # gzip | none — default gzip
      timeout: 10s
      retry:
        enabled: true                       # default true
        initial_interval: 5s
        max_interval: 30s
        max_elapsed_time: 1m
      signals:
        metrics:
          enabled: true                     # default true
          interval: 30s
          temporality: cumulative           # cumulative | delta — default cumulative
        logs:
          enabled: true                     # default true
          batch_size: 1000
          batch_timeout: 5s
          buffer_size: 10000                # bounded queue; drop-oldest beyond this
      resource:
        # Resource attributes per OTel spec — attached to every record.
        # Defaults derived from agent identity if not set.
        service.name: senhub-agent          # default
        service.instance.id: <agent-key prefix>   # default
        deployment.environment: prod        # operator override
```

All knobs map directly to OTel SDK options. **No SenHub-specific
behavior** is hidden in here — every key corresponds to an upstream OTel
configuration field.

## 3. Dependencies

New direct dependencies (added to go.mod):

| Package | Purpose |
|---|---|
| `go.opentelemetry.io/otel` | core API |
| `go.opentelemetry.io/otel/sdk` | SDK runtime |
| `go.opentelemetry.io/otel/sdk/metric` | metric SDK |
| `go.opentelemetry.io/otel/sdk/log` | log SDK (recently stabilized) |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | metrics OTLP/gRPC exporter |
| `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc` | logs OTLP/gRPC exporter |
| `google.golang.org/grpc` | gRPC transport (transitive of OTel exporters) |

Total transitive footprint: ~30 new modules. Acceptable cost for the
push capability.

## 4. Phasing

### Phase 0 — Planning *(this document)*
- Audit + plan + spec
- User validation of approach

### Phase 1 — Strategy skeleton + lifecycle
- `internal/agent/services/data_store/strategies/otlp/`
- Implement `data_store.Strategy` interface
- Configuration parsing (mirror http strategy's ConfigurationManager
  pattern)
- gRPC client setup (TLS, headers, compression, retry, timeout)
- DataStore registration in `data_store.go:registerStrategies`
- Graceful shutdown (Drain queues, Shutdown exporters with timeout)
- Tests: lifecycle, config validation, shutdown drain

### Phase 2 — Metrics export
- `metrics_exporter.go`: convert `[]otelmapper.OtelRecord` into
  `metricdata.ResourceMetrics`
- Periodic push goroutine (driven by `signals.metrics.interval`)
- Counter cumulative semantics (process start time, last value tracking)
- Consume `otelmapper.Resolve` directly — neutral package with no
  Prometheus or OTLP-specific code
- Tests: round-trip via SDK's `metricdatatest` helpers

### Phase 3 — Logs export
- `internal/agent/services/agentstate/log_channel.go` — pub/sub buffer
- `syslog` + `event` probes call `agentstate.PublishLog`
- `logs_exporter.go`: drain channel → batch → OTLP log exporter
- Drop the `otel.skip: true` directive on `syslog_event` and
  `event_event` YAML entries (they have a destination now, but only via
  the logs path — they remain absent from `/metrics`)
- Tests: probe → channel → batch → exporter mock

### Phase 4 — Live validation
- Spin up an OTLP receiver on the operations host: either vmagent's OTLP gRPC
  endpoint, or a fresh otelcol-contrib instance
- Verify metrics arrive in VictoriaMetrics with the **same names** as
  the Prometheus path — concrete proof of the "no query rewrite"
  promise
- Verify logs arrive in VictoriaLogs (already running on the operations host)
- Same SSH reverse-tunnel pattern as Prometheus Phase 4

### Phase 5 — Documentation + release notes
- `docs/user-guide/content/docs/otlp/_index.md` — integration guide
- Update `senhub-semantic-conventions.md` with a new section noting that
  the OTLP push path emits identical names to Prometheus (single
  vocabulary, two transport mechanisms)
- Release notes 0.1.89-beta

## 5. Standard compliance

**Strict adherence to OTel spec — no custom behavior unless the spec
explicitly allows it:**

- OTLP protobuf schema: pinned via SDK; we don't hand-write protos
- Metric data model: `metricdata.ResourceMetrics` from SDK
- Log data model: `log.Record` from SDK
- Resource attributes: `service.name`, `service.instance.id`,
  `service.version`, `deployment.environment` per the OTel resource
  semconv
- Severity mapping (syslog → OTel): per the OTel log data model spec
  (TRACE..FATAL4 mapping table, sourced from
  [logs/data-model.md §4.2](https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/logs/data-model.md))
- Counter monotonicity: enforced by the SDK (we push deltas in order)
- Aggregation temporality: cumulative by default (OTel default since
  v1.0); delta available for users with VictoriaMetrics's preferred
  ingest path

**SenHub-specific extensions** (carried over from the Prometheus side, all
already documented in `senhub-semantic-conventions.md`):
- `senhub.*` metric namespace for vendor-specific domains
- Extension values for OTel attributes (e.g. `cpu.mode=dpc`,
  `system.memory.state=committed`)
- The `senhub.event.*` log attribute namespace (event probe payload)

These extensions are **valid OTel** — the spec explicitly allows
extension namespaces and extension attribute values, and we use them
the way the spec recommends (clearly namespaced, documented, stable).

## 6. Lifecycle and failure modes

**Startup:**
- gRPC client connects lazily (first export attempt)
- Connection failure does NOT prevent the agent from starting — OTLP is
  optional even when configured
- Logs `WARN` once on initial connection failure, retries per backoff

**Steady state:**
- Metrics: every `interval`, snapshot the cache → encode → push. If push
  fails, retry per `retry:` config; drop the snapshot if max_elapsed_time
  is exceeded (next interval gets a fresh snapshot — cumulative counter
  semantics guarantee no gap from the consumer's POV).
- Logs: drain buffered records into batches of `batch_size` (or after
  `batch_timeout`). On push failure, retry; on retry exhaustion, drop
  the batch with WARN log + a `senhub_agent_otlp_dropped_log_records_total`
  counter (new self-metric, exposed in Prometheus exposition).

**Shutdown:**
- Drain the log channel (wait up to `timeout`, then drop remainder)
- Final metrics push on shutdown
- Close the gRPC connection cleanly

## 7. Self-observability of the OTLP strategy itself

The agent's existing `senhub_agent_*` self-metrics gain three siblings
(emitted via the Prometheus exposition; the OTLP strategy does not push
metrics about itself to avoid feedback loops):

| Metric | Type | Description |
|---|---|---|
| `senhub_agent_otlp_metrics_pushed_total` | counter | OTLP metric data points successfully exported |
| `senhub_agent_otlp_logs_pushed_total` | counter | OTLP log records successfully exported |
| `senhub_agent_otlp_export_errors_total` | counter | Failed export attempts (after retries exhausted) |
| `senhub_agent_otlp_dropped_log_records_total` | counter | Log records dropped due to backpressure |
| `senhub_agent_otlp_buffer_fill_ratio` | gauge | Current log buffer occupancy (0..1) |

## 8. Acceptance criteria

- [ ] OTLP/gRPC metrics exported with **identical names** to the
      Prometheus exposition (verified by side-by-side query:
      `senhub_system_cpu_utilization_ratio` queryable in both paths)
- [ ] OTLP/gRPC logs exported from `syslog` + `event` probes; appear
      in VictoriaLogs with proper severity, body, attributes
- [ ] No regression: PRTG, Nagios, senhub, http (Prometheus) strategies
      all unchanged — full test suite green with `-race`
- [ ] Graceful shutdown: pending data drained or dropped explicitly
      with WARN; no leaked goroutines (verified via `goleak` in tests)
- [ ] Strict OTel spec compliance: any deviation requires user-explicit
      sign-off (none expected)
- [ ] User docs + release notes 0.1.89-beta complete

## 9. Open questions (for future review rounds)

1. **Default port**: 4317 (gRPC plaintext) vs 4318 (HTTP)? OTel default
   for gRPC is 4317 — using that.
2. **TLS default**: opt-in or opt-out? Default `tls.enabled: true` is
   safer (push to remote = always encrypt). Plaintext for localhost
   testing requires explicit `tls.enabled: false`.
3. **mTLS configuration**: support client certs from day 1, or follow-up?
   Spec includes them in `tls:` block but the SDK exporter wires them
   easily — including in v1.
4. **Header substitution**: `${OTLP_TOKEN}` env-var resolution — pre-existing
   pattern? We need to verify how the agent's existing config handles
   env vars; if not supported, this becomes a follow-up.
5. **Resource auto-detection**: OTel SDK has `Detector`s (host, OS,
   process) that auto-populate resource attributes. Use them
   (`sdkresource.Default()` + custom merger) or stick to fully
   user-configured `resource:` block?

## 10. Future scope notes

- **OS log file consumer probes** (Linux journald + `/var/log/*`;
  Windows Event Log + ETW). These are **sources**, parallel to syslog
  and event. They feed the same `agentstate.PublishLog` channel, so
  Phase 3 of this branch lays the foundation. A future
  `feat/log-file-probes` branch will add them as new probe types
  (`linux_journald`, `linux_logfile`, `windows_eventlog`).
- **OTLP traces signal**: not exported here, but the strategy's gRPC
  client could host a third exporter when the agent gains tracing
  surface area. Trivial future addition.
- **OTLP/HTTP fallback**: Some operators forbid gRPC at the firewall.
  Adding HTTP/protobuf is a 1-day addition (different exporter package,
  same data model). Not in scope now.
