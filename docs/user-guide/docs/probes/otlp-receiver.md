<img src="https://cdn.simpleicons.org/opentelemetry" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `protocol` | `grpc` | `grpc` (OTLP/gRPC) or `http` (OTLP/HTTP protobuf) |
| `address` | `127.0.0.1:4317` (grpc), `127.0.0.1:4318` (http) | Listen address. Loopback by default — accepting remote OTLP requires an explicit address (e.g. `"0.0.0.0:4317"`); pair it with the protections below |
| `port` | from `address` | Convenience override: replaces only the port part of the address |
| `http_path` | `/v1/metrics` | Route served by the HTTP receiver (ignored for gRPC) |
| `bearer_token` | none | When set, senders must present `Authorization: Bearer <token>` (HTTP header or gRPC metadata). Reference a stored secret via `${secret:<name>.bearer_token}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |
| `allowed_cidrs` | none | Source IP allow-list (CIDR notation, IPv4/IPv6). Checked against the transport peer address — proxy headers are not trusted |
| `rate_limit_rps` | `0` (off) | Accepted requests per second (token bucket). Excess requests get HTTP 429 / gRPC `ResourceExhausted` |
| `rate_limit_burst` | `2 × rps` | Bucket burst capacity |

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
  win on key collisions.
- **All metric types.** Gauges and Sums map to one value each.
  Histograms and summaries are ingested as their component series —
  `<name>_count`, `<name>_sum`, cumulative `<name>_bucket{le="…"}`
  (Prometheus style), `<name>{quantile="…"}` for summaries, and
  `<name>_min` / `<name>_max` when present. Exponential histograms
  contribute their `_count` / `_sum` / `_min` / `_max` aggregates
  (the base-2 buckets are not expanded yet). Only a metric with an
  unrecognized or unset data type is dropped, reported in the OTLP
  partial-success response.
- **Pass-through naming.** Ingested metric names are forwarded
  unchanged; nothing is renamed or prefixed.
- **Limits.** gRPC accepts payloads up to 4 MiB (the OTel SDK
  default); the HTTP server applies a 30-second read timeout.

## Operational notes

- A bind failure (port already taken) surfaces at probe start, not
  silently at runtime.
- The listener accepts plaintext OTLP. Keep it on localhost or a
  trusted network segment; for cross-network ingestion put a TLS
  terminator or an OTel collector in front.
- Shutdown is graceful on both protocols: in-flight requests finish
  before the agent exits.
