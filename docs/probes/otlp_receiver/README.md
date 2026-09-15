# OTLP Receiver Probe

The `otlp_receiver` probe turns the agent into a small **edge collector**:
it runs an embedded OTLP receiver (gRPC or HTTP) that accepts incoming
OTLP **metrics**, **logs** and **traces** from other instrumented
devices and applications, and routes them to every configured sink
(OTLP re-export, Prometheus pull, PRTG, SenHub cloud, ...). Part of the
**Free tier** (universal collection).

Two things must line up for a signal to arrive somewhere:

1. the receiver must **accept** the signal — the `signals` parameter below;
2. a configured strategy must **consume** it — see [Sinks](#sinks).

A signal that is accepted but has no sink is counted and dropped, with a
throttled warning in the agent log.

### What goes wrong with `signals`, and how it looks

Every shape below produces a running agent. The difference is what the
receiver actually registered, and a signal that was never registered
answers a sender with `UNIMPLEMENTED` — which reads like a missing
feature rather than an option that was not asked for.

| Written | Receiver starts with | How you find out |
|---|---|---|
| no `signals` line | `["metrics"]` | nothing says so; logs and traces are refused |
| `signals: [metrics, logs, traces]` | `["metrics","logs","traces"]` | this is the working form |
| `signals: logs` | `["logs"]` | accepted — a lone name is a one-item list. **Metrics stop**, because the list replaces the default |
| `signals: metrics, logs, traces` (no brackets) | refused at start | a scalar is not a list. Before 0.5.5 this silently left the receiver on metrics only |
| `signals: [metrics, log]` | probe does not start | `unknown signal "log"`, named at startup |

**The one check that answers all of it** is the line the receiver logs
when it comes up:

```
INF OTLP gRPC receiver started address=0.0.0.0:4317 signals=["metrics","logs","traces"]
```

If that list is not what you wrote, the agent did not read what you
think it read. On 0.5.4 and earlier, `agent config check` does not
validate these values — it reports the file as valid and the probe then
refuses to start; from 0.5.5 the check runs the probe's own validation.

Accepting a signal is only half of it: a strategy must also consume it,
or the records are counted as `no_sink` and dropped. See [Sinks](#sinks).

## Configuration

```yaml
# probes.d/20-otlp_receiver.yaml — each file under probes.d/ is a YAML array of probes
- type: otlp_receiver
  name: edge_in
  params:
    protocol: grpc                 # grpc (default) | http
    address: "127.0.0.1:4317"      # default is loopback-only; see "Listening remotely"
    signals: [metrics]             # default: metrics only
    # port: 5317                   # optional: override only the port, keep the host
    # http_path: "/v1/metrics"     # http only: route the metrics receiver serves
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `protocol` | string | `grpc` | `grpc` (OTLP/gRPC) or `http` (OTLP/HTTP protobuf). |
| `address` | string | `127.0.0.1:4317` (grpc), `127.0.0.1:4318` (http) | Listen `host:port`. Loopback-only by default; see [Listening remotely](#listening-remotely). |
| `port` | int | — | Convenience: replace just the port of the default/derived address. |
| `http_path` | string | `/v1/metrics` | HTTP only: the route metrics are POSTed to. Logs and traces are served on the fixed OTLP routes `/v1/logs` and `/v1/traces`. |
| `signals` | list of strings | `[metrics]` | Which OTLP services the listener registers: any of `metrics`, `logs`, `traces`. The list **replaces** the default rather than extending it. See [Signals](#signals). |
| `bearer_token` | string | — | When set, senders must present `Authorization: Bearer <token>`. Use `${env:VAR}` or `${file:/path}` rather than a literal. |
| `allowed_cidrs` | list of strings | — | When non-empty, only these peer ranges may ingest. The transport peer address is used; proxy headers are not consulted. |
| `rate_limit_rps` | float | — | Accepted requests per second (token bucket). Omitted or `0` disables rate limiting. |
| `rate_limit_burst` | int | `2 x rate_limit_rps` | Burst allowance. Requires `rate_limit_rps`. |

Run multiple instances (different names and ports) to receive on several
endpoints.

## Signals

`signals` is a **list** of signal names on the probe:

```yaml
params:
  signals: [metrics, logs, traces]
```

A configuration with no `signals` key accepts **metrics only**, so
existing installations are unchanged by upgrades. This is the single
most common surprise: a sender exporting logs to a receiver that was
never told to accept them gets

```
UNIMPLEMENTED: unknown service opentelemetry.proto.collector.logs.v1.LogsService
```

which reads like a missing feature but means the opt-in is absent. Add
`logs` to the list and restart the agent. The agent logs the enabled
signals on startup, which is the quickest way to confirm.

Note the shape difference from the OTLP **strategy**, where `signals` is
a map keyed by signal name (`signals: { logs: { enabled: true } }`). The
probe decides what is **received**; the strategy decides what is
**sent**. Enabling a signal on one side has no effect on the other.

## Listening remotely

The listen defaults are **loopback-only**. The receiver performs no
authentication unless you configure it, so accepting OTLP from other
machines is an explicit opt-in. The upstream OpenTelemetry Collector
made the same default change in v0.104.

To accept remote senders, set `address` to a routable interface and
protect the endpoint:

```yaml
params:
  address: "0.0.0.0:4317"
  signals: [metrics, logs]
  bearer_token: "${file:/etc/senhub-agent/otlp.token}"
  allowed_cidrs: ["10.42.0.0/16"]
  rate_limit_rps: 500
```

A sidecar sharing a network namespace with its application (same
Kubernetes pod, for instance) can keep the loopback default.

## What it ingests

**Metrics.** Every OTLP metric family is ingested:

- **Gauge** and **Sum** number datapoints become one internal datapoint
  each, keyed by the incoming OTel metric name (kept verbatim). The
  inbound instrument type and unit are preserved via control tags.
- **Histogram** and **Summary** are expanded into their Prometheus-style
  component series (`_count`, `_sum`, `_bucket{le}`, `{quantile}`,
  `_min`/`_max`).
- **ExponentialHistogram** contributes its aggregates only; buckets are
  deferred (#659).
- Only an unrecognized or unset data type is dropped. Its count is
  reported back to the sender as an OTLP `PartialSuccess`
  (`rejected_data_points`) and logged.

Resource attributes and datapoint attributes are folded onto each
datapoint as tags; datapoint attributes win on key collisions.

Every ingested datapoint is tagged `metric_type=otlp_ingest` and carries
`probe_name` / `probe_type=otlp_receiver`. Because these metrics arrive
already OTel-shaped, the shared mapper passes them through to every sink
without needing a per-probe transformer definition (their names are
arbitrary external identifiers).

**Logs.** Records are converted to the agent's internal log envelope and
published for relay. The sender's own resource identity is preserved.

**Traces.** Spans have no internal scalar model and are relayed verbatim
as received `ResourceSpans`; they bypass the datapoint path entirely.

## Sinks

Accepting a signal is only half the path. Each signal needs a strategy
that consumes it, otherwise the receiver counts the records as dropped
and warns:

| Signal | Consumed by |
|---|---|
| Metrics | Every configured storage, plus the Prometheus pull endpoint (`/api/<key>/prometheus/metrics`) and the other HTTP sub-formats. |
| Logs | An OTLP strategy with `signals.logs.enabled`, or the `event` strategy. |
| Traces | An OTLP strategy with `signals.traces.enabled`. |

Typical edge-collector setup — receive metrics and logs, forward both:

```yaml
# strategies.d/40-otlp.yaml
otlp:
  endpoint: "central-collector:4317"
  signals:
    metrics: { enabled: true }
    logs:    { enabled: true }
```

```yaml
# probes.d/20-otlp_receiver.yaml
- type: otlp_receiver
  name: edge_in
  params:
    protocol: grpc
    address: "127.0.0.1:4317"
    signals: [metrics, logs]
```

## Status

Validated end-to-end on macOS/Linux: a sender exporting OTLP metrics to
the receiver has its series routed through the data store to the
configured sinks — confirmed both as OTLP re-export (the sender's
`system.cpu.*` series reach a downstream collector, tagged
`probe_type=otlp_receiver`) and into the HTTP cache. Decoding, scalar
flattening, histogram and summary expansion, partial-success for
unrecognized types, the log and span relay paths, and the mapper
pass-through are unit-tested.
