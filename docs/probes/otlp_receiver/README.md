# OTLP Receiver Probe

The `otlp_receiver` probe turns the agent into a small **edge collector**:
it runs an embedded OTLP receiver (gRPC or HTTP) that accepts incoming
OTLP **metric** streams from other instrumented devices/applications,
decodes them as if they were collected by an internal probe, and routes
them to every configured sink (OTLP re-export, Prometheus pull, PRTG,
SenHub cloud, …). Part of the **Free tier** (universal collection).

## Configuration

```yaml
# probes.d/20-otlp_receiver.yaml — each file under probes.d/ is a YAML array of probes
- type: otlp_receiver
  name: edge_in
  params:
    protocol: grpc                 # grpc (default) | http
    address: "0.0.0.0:4317"        # listen address; default 4317 (grpc) / 4318 (http)
    # port: 5317                   # optional: override only the port, keep the host
    # http_path: "/v1/metrics"     # http only: route the receiver serves
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `protocol` | string | `grpc` | `grpc` (OTLP/gRPC) or `http` (OTLP/HTTP protobuf). |
| `address` | string | `0.0.0.0:4317` (grpc), `0.0.0.0:4318` (http) | Listen `host:port`. |
| `port` | int | — | Convenience: replace just the port of the default/derived address. |
| `http_path` | string | `/v1/metrics` | HTTP only: the route metrics are POSTed to. |

Run multiple instances (different names + ports) to receive on several
endpoints.

## What it ingests

- **Gauge** and **Sum** number datapoints → one internal datapoint each,
  keyed by the incoming OTel metric name (kept verbatim).
- Resource attributes and datapoint attributes are folded onto each
  datapoint as tags (datapoint attributes win on key collisions).
- **Histogram / ExponentialHistogram / Summary** carry no single scalar
  value and are **not** ingested; their count is reported back to the
  sender as an OTLP `PartialSuccess` (`rejected_data_points`) and logged.

Every ingested datapoint is tagged `metric_type=otlp_ingest` and carries
`probe_name` / `probe_type=otlp_receiver`. Because these metrics arrive
already OTel-shaped, the shared mapper passes them through to every sink
**without** needing a per-probe transformer definition (their names are
arbitrary external identifiers).

## Output

The ingested metrics flow to all configured storages. Typical edge-collector
setup — receive OTLP and forward it on (OTLP re-export):

```yaml
# strategies.d/40-otlp.yaml
otlp:
  endpoint: "central-collector:4317"
  signals: { metrics: { enabled: true } }
```

```yaml
# probes.d/20-otlp_receiver.yaml
- type: otlp_receiver
  name: edge_in
  params: { protocol: grpc, address: "0.0.0.0:4317" }
```

They also surface on the Prometheus pull endpoint (`/api/<key>/prometheus/metrics`)
and the other HTTP sub-formats.

## Status

Validated end-to-end on macOS/Linux: a sender exporting OTLP metrics to
the receiver has its series routed through the data store to the
configured sinks — confirmed both as OTLP re-export (the sender's
`system.cpu.*` series reach a downstream collector, tagged
`probe_type=otlp_receiver`) and into the HTTP cache. Decoding, scalar
flattening, partial-success for non-scalar types, and the mapper
pass-through are unit-tested.

> **Metric types**: every OTLP metric family is ingested. Gauge/Sum map
> to one value each (the inbound instrument type and unit are preserved
> via control tags). Histograms and summaries are expanded into their
> Prometheus-style component series (`_count`, `_sum`, `_bucket{le}`,
> `{quantile}`, `_min`/`_max`); exponential histograms contribute their
> aggregates only (buckets deferred, #659). Only an unrecognized/unset
> data type is dropped and counted for partial-success.
