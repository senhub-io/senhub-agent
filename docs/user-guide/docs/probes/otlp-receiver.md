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
probes:
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
| `bearer_token` | none | When set, senders must present `Authorization: Bearer <token>` (HTTP header or gRPC metadata). Use `${env:VAR}` or `${file:/path}` substitution, never a literal token |
| `allowed_cidrs` | none | Source IP allow-list (CIDR notation, IPv4/IPv6). Checked against the transport peer address — proxy headers are not trusted |
| `rate_limit_rps` | `0` (off) | Accepted requests per second (token bucket). Excess requests get HTTP 429 / gRPC `ResourceExhausted` |
| `rate_limit_burst` | `2 × rps` | Bucket burst capacity |

Opening the receiver to the network with all protections:

```yaml
probes:
  - name: otlp-in
    type: otlp_receiver
    params:
      protocol: grpc
      address: "0.0.0.0:4317"
      bearer_token: "${env:OTLP_INGEST_TOKEN}"
      allowed_cidrs: ["10.0.0.0/8"]
      rate_limit_rps: 100
```

Run two instances to serve both protocols at once:

```yaml
probes:
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
- **Scalar metrics only.** Gauges and Sums are ingested. Histograms,
  exponential histograms and summaries are dropped and reported in
  the OTLP partial-success response — the sender's SDK logs it.
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
