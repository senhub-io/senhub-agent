<img src="../../assets/probe-logos/otlp-receiver.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# OTLP Receiver Probe

The `otlp_receiver` probe turns the agent into an edge OTLP
collector: applications and SDKs push OTLP metrics to it (gRPC or
HTTP), and the ingested datapoints flow through the agent exactly
like locally collected metrics — out to PRTG, Nagios, Prometheus,
OTLP or the SenHub cloud, whichever storages are configured.

Use it when instrumented applications run next to the agent and you
want one egress point per host instead of a separate collector
deployment.

## Quick start

```yaml
# probes.d/10-otlp-receiver.yaml — each file under probes.d/ is a YAML array of probes
- name: otlp-in
  type: otlp_receiver
  params:
    protocol: grpc          # listens on 127.0.0.1:4317
    # address: "0.0.0.0:4317"  # required to accept remote senders
```

Point any OTel SDK or collector at the agent:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `protocol` | In practice | `grpc` | Listener transport: OTLP/gRPC or OTLP/HTTP protobuf. One of `grpc`, `http` |
| `address` | In practice | - | Listen address (host:port); 127.0.0.1:4317 for grpc and 127.0.0.1:4318 for http when empty, so remote senders need an explicit address. Example: `0.0.0.0:4317` |
| `port` | No | - | Replaces only the port part of the address |
| `http_path` | No | `/v1/metrics` | Route the HTTP receiver serves metrics on; logs and traces keep /v1/logs and /v1/traces; ignored for grpc |
| `signals` | In practice | `[metrics]` | Signals the listener accepts; empty means metrics only. One of `metrics`, `logs`, `traces` |
| `bearer_token` | No | - | Token senders must present as Authorization: Bearer; empty accepts unauthenticated senders. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `allowed_cidrs` | No | - | Source ranges (CIDR) allowed to send, checked on the transport peer address; empty allows any. Example: `10.0.0.0/8` |
| `rate_limit_rps` | No | `0` | Accepted requests per second; 0 turns rate limiting off |
| `rate_limit_burst` | No | - | Token bucket burst; twice rate_limit_rps when empty, needs rate_limit_rps |

<!-- schema:params:end -->

- The bearer token is read from the HTTP `Authorization` header or from the gRPC `authorization` metadata.
- `allowed_cidrs` accepts IPv4 and IPv6 ranges. Proxy headers such as `X-Forwarded-For` are not trusted: a sender behind a proxy is checked on the proxy's address.
- A request above the rate limit is refused with HTTP 429 or the gRPC `ResourceExhausted` status.
- Logs and traces are relayed onward through a configured OTLP export strategy (see Behavior).

Opening the receiver to the network with all protections:

```yaml
# probes.d/10-otlp-receiver.yaml
- name: otlp-in
  type: otlp_receiver
  params:
    protocol: grpc
    address: "0.0.0.0:4317"
    bearer_token: ${secret:otlp-in.bearer_token}   # OS secret store; inline plaintext is auto-sealed on install
    allowed_cidrs: ["10.0.0.0/8"]
    rate_limit_rps: 100
```

Run two instances to serve both protocols at once:

```yaml
# probes.d/10-otlp-receiver.yaml
- name: otlp-grpc
  type: otlp_receiver
  params:
    protocol: grpc
- name: otlp-http
  type: otlp_receiver
  params:
    protocol: http
```

## Behavior

- **Resource attributes become tags.** `host.name`, `service.name`
  and every other resource attribute is folded onto each datapoint,
  so downstream sinks can group by origin. Per-datapoint attributes
  win on key collisions. This is what PRTG, Nagios, Prometheus, the web
  UI and the cloud sink read, and it is unchanged.
- **The OTLP output relays the original batch.** Those same points are
  also forwarded verbatim on the OTLP export, under the **emitting
  application's** resource rather than re-encoded under the agent's.
  Without it a reserved identity key such as `service.name` would carry
  two different values in one export — the agent's on the resource, the
  application's on the datapoint — and the backend would silently keep
  one. Agent context is added on top, never substituted, exactly as for
  logs and traces. Only the OTLP output is affected; every other sink
  keeps reading the tags above.
- **All metric types.** Gauges and Sums map to one value each.
  Explicit-bucket histograms are ingested **natively**: re-exported over
  OTLP as a genuine histogram (buckets, sum, count, min/max preserved)
  and on the Prometheus endpoint as a classic histogram — cumulative
  `<name>_bucket{le="…"}`, `<name>_sum`, `<name>_count`. Sinks without
  histogram rendering (PRTG, Nagios, cloud) show the observation count.
  Summaries are ingested as their component series — `<name>_count`,
  `<name>_sum`, `<name>{quantile="…"}` — and exponential histograms
  contribute their `_count` / `_sum` / `_min` / `_max` aggregates
  (the base-2 buckets are not expanded yet). Only a metric with an
  unrecognized or unset data type is dropped, reported in the OTLP
  partial-success response.
- **Pass-through naming.** Ingested metric names are forwarded
  unchanged; nothing is renamed or prefixed.
- **Logs are relayed.** With `signals: [logs]`, OTLP log records are
  accepted (gRPC `LogsService`, or HTTP on `/v1/logs`) and forwarded
  verbatim by a configured OTLP export strategy (an OTLP-in → OTLP-out
  relay). Severity, body, attributes **and the emitting application's
  resource** are preserved: a record sent with `service.name=my-app`
  arrives as `my-app`, so applications stay distinguishable at the
  backend. Agent context (tenant, site, environment, the
  `telemetry.relay.*` identity) is only ever **added on top** — an
  attribute the sender already set is never replaced. The pull sinks
  (Prometheus/PRTG/Nagios) are metrics-only, so **logs need an OTLP
  export strategy** — without one, ingested logs are discarded and the
  agent logs a throttled warning.

    !!! note "Changed behaviour"
        Before 0.5.4, ingested logs were re-emitted through the
        agent's own log pipeline, which replaced the sender's resource
        with the agent's — a record sent with `service.name=my-app` was
        stored under the agent's `service.name`, making applications
        indistinguishable by that attribute. If a dashboard or query
        relies on ingested logs carrying the agent's `service.name`,
        point it at the agent's own logs or at
        `telemetry.relay.instance.id` instead.
- **Traces are relayed.** With `signals: [traces]`, OTLP trace spans are
  accepted (gRPC `TracesService`, or HTTP on `/v1/traces`) and forwarded
  as a raw pass-through: spans are relayed verbatim — trace IDs, span
  IDs, attributes, events, and resource are untouched (an OTLP-in →
  OTLP-out relay). **Traces need an OTLP export strategy with
  `signals.traces.enabled: true`** — the traces signal on the export side
  gates the relay; without it, ingested spans are discarded and the agent
  logs a throttled warning.
- **Limits.** gRPC accepts payloads up to 4 MiB (the OTel SDK
  default); the HTTP server applies a 30-second read timeout.

## Operational notes

- **Observability.** The agent exposes `senhub_agent_otlp_receiver_ingested_total`
  (items accepted, by `signal`) and `senhub_agent_otlp_receiver_dropped_total`
  (discarded, by `signal` + `reason` — `no_sink` when logs/traces arrive with
  no export strategy, `unmapped` for a metric with an unrecognized data type).
  A rising `dropped{reason="no_sink"}` means a sender is pushing logs/traces the
  agent has nowhere to relay to.
- A bind failure (port already taken) surfaces at probe start, not
  silently at runtime.
- The listener accepts plaintext OTLP. Keep it on localhost or a
  trusted network segment; for cross-network ingestion put a TLS
  terminator or an OTel collector in front.
- Shutdown is graceful on both protocols: in-flight requests finish
  before the agent exits.

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `http.server.request.duration` | `http.server.request.duration` | HTTP {service.name} Request Duration | s | Duration of the HTTP server requests an application reports, per route, method and status. Relayed as sent: the count is the number of requests, the sum their total duration |
| `http.server.active_requests` | `http.server.active_requests` | HTTP {service.name} Active Requests | # | Requests an application is handling right now |
| `jvm.memory.used` | `jvm.memory.used` | JVM {service.name} Memory Used | B | Memory a Java application holds, per pool |
| `jvm.memory.committed` | `jvm.memory.committed` | JVM {service.name} Memory Committed | B | Memory committed to a Java application by the operating system, per pool |
| `jvm.memory.limit` | `jvm.memory.limit` | JVM {service.name} Memory Limit | B | Ceiling on a Java memory pool, which the used value above is measured against |
| `jvm.thread.count` | `jvm.thread.count` | JVM {service.name} Threads | # | Live threads in a Java application |
| `jvm.class.count` | `jvm.class.count` | JVM {service.name} Loaded Classes | # | Classes currently loaded by a Java application |
| `jvm.cpu.recent_utilization` | `jvm.cpu.recent_utilization` | JVM {service.name} CPU Utilization | 1 | Share of a processor a Java application has used recently, as a ratio |
| `jvm.gc.duration` | `jvm.gc.duration` | JVM {service.name} GC Duration | s | Time a Java application spent collecting garbage, per collector. The count is the number of collections, the sum the time they took |
| `db.client.operation.duration` | `db.client.operation.duration` | DB {service.name} Operation Duration | s | Duration of the database calls an application makes, per system and operation. The count is the number of calls, the sum their total duration |
| `db.client.connection.count` | `db.client.connection.count` | DB {service.name} Connections | # | Connections in an application's database pool, idle and used |
| `db.client.connection.pending_requests` | `db.client.connection.pending_requests` | DB {service.name} Pending Connection Requests | # | Requests waiting for a connection from the pool, which is what a saturated pool looks like |

<!-- schema:metrics:end -->
