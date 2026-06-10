# OTLP / OpenTelemetry

SenHub Agent can push metrics and logs natively over **OTLP/gRPC** to any
OpenTelemetry receiver — an OTel collector, vmagent's OTLP endpoint, a
direct VictoriaMetrics / VictoriaLogs ingest, Grafana Cloud OTLP, etc.

OTLP is a **push** path, complementary to the [Prometheus pull
endpoint](prometheus/index.md). Both signals carry the
**same metric names, types, units, and attributes** because both are
produced by the same neutral OTel mapper internally — operators
querying Prometheus today and switching to OTLP push tomorrow do not
rewrite a single PromQL query.

## Quick start

1. Add an `otlp` storage block to your config:

```yaml
storage:
  - name: otlp
    params:
      endpoint: "otel-collector.internal:4317"   # required
      tls:
        enabled: true
      signals:
        metrics:
          enabled: true
          interval: 30s
        logs:
          enabled: true
```

2. Restart the agent. The agent connects lazily — a missing collector
   does not block startup; failures appear as retried export attempts.

3. Verify the collector is receiving data:

```bash
# At the collector side
journalctl -u otel-collector -f
# or query VictoriaMetrics directly
curl -G http://vm:8428/api/v1/query \
  --data-urlencode 'query=count by(__name__) ({service_name="senhub-agent"})'
```

## Configuration

The full set of knobs (every key maps directly to an OTel SDK option):

```yaml
storage:
  - name: otlp
    params:
      # Required: gRPC endpoint of the receiver (no scheme prefix).
      endpoint: "otel-collector.internal:4317"

      # Optional headers (for example bearer auth at the gateway).
      headers:
        Authorization: "Bearer YOUR-INGEST-TOKEN"
        X-Tenant-Id: "acme"

      # TLS — defaults to enabled. Disable explicitly for plaintext
      # localhost / lab environments.
      tls:
        enabled: true                 # default true
        insecure_skip_verify: false   # opt-in for self-signed certs in test
        ca_file: /etc/ssl/private/otlp-ca.pem
        cert_file: /etc/ssl/private/agent.pem    # mTLS, optional
        key_file:  /etc/ssl/private/agent.key    # required if cert_file set

      compression: gzip               # gzip | none — default gzip
      timeout: 10s                    # per-export deadline

      retry:
        enabled: true
        initial_interval: 5s
        max_interval: 30s
        max_elapsed_time: 1m

      signals:
        metrics:
          enabled: true
          interval: 30s               # push cadence
          temporality: cumulative     # cumulative | delta — default cumulative
        logs:
          enabled: true
          batch_size: 1000            # max records per gRPC export
          batch_timeout: 5s           # flush even if batch_size not reached
          buffer_size: 10000          # bounded queue; drop-oldest beyond

      # Resource attributes attached to every emitted batch. Defaults
      # are derived from agent identity if omitted.
      resource:
        service.name: senhub-agent             # default
        service.instance.id: <agent-key prefix> # default: first 8 chars of agent.key
        deployment.environment: prod
        # Any additional keys are passed through as resource attributes.
        k8s.cluster.name: edge-01
```

### `endpoint` (required)

`host:port` of the OTLP/gRPC receiver. No scheme prefix (it's gRPC).
Default OTLP/gRPC port is **4317**. There is intentionally **no
default** for this field — silently shipping data to localhost when
the operator forgets to set it would be a worse failure mode than
refusing to start.

### `tls`

Default is `enabled: true`. For **production push to a remote
collector you should always keep TLS enabled**. The plaintext path is
provided only for localhost / lab tests where the gRPC traffic never
leaves the loopback interface.

mTLS is supported by setting both `cert_file` and `key_file`. Setting
only one is rejected at config validation.

### `signals.metrics` and `signals.logs`

Each signal can be enabled/disabled independently. Disabling one stops
the corresponding pipeline and keeps the gRPC connection idle.

`temporality` defaults to **cumulative** (the OTel default since
OTel v1.0). Cumulative means counter values are reported as absolute
totals since the agent start; consumers that prefer deltas (some
Datadog setups, vmagent ingest) can set `temporality: delta`.

### `resource`

Resource attributes are attached **once per batch** by the SDK
(per-batch in OTLP wire format) and are how observability backends
group series and logs into entities. Defaults:

- `service.name` = `"senhub-agent"`
- `service.instance.id` = first 8 chars of the agent authentication
  key (avoids leaking the full secret to backends while keeping
  per-agent disambiguation)
- `service.version` = the agent build version (when known)

Any additional key/value pair under `resource:` is passed through
verbatim — useful for `k8s.*`, `cloud.*`, `host.*`, or your own
custom labels.

## Naming alignment with Prometheus

Metrics emitted via OTLP carry the **same internal names** as the
Prometheus exposition. The path is:

```
probe data → otelmapper.Resolve → []OtelRecord → { Prometheus serializer
                                                  OTLP exporter }
```

Both consumers read from the same `OtelRecord` stream. Prometheus
applies its sink-specific conventions (`senhub_` prefix, `_seconds`
suffix, dots → underscores), and OTLP keeps the dotted OTel names
intact. The OTel collector / VictoriaMetrics / Grafana that ingests
the OTLP push automatically applies the same transforms when storing
into a Prometheus-compatible TSDB, so the **observed metric names are
identical** at the query layer.

| OTel name (OTLP wire) | Prometheus exposition | After OTLP→PromQL ingest in VM |
|---|---|---|
| `system.cpu.utilization` (gauge, `1`, `cpu.mode=user`) | `senhub_system_cpu_utilization_ratio` | `system_cpu_utilization_ratio` |
| `system.memory.usage` (updowncounter, `By`) | `senhub_system_memory_usage_bytes` | `system_memory_usage_bytes` |
| `system.network.io` (gauge, `By/s`, `network.io.direction=receive`) | `senhub_system_network_io_bytes_per_second` | `system_network_io_bytes_per_second` |
| `senhub.netscaler.lbvserver.connections.active` | `senhub_netscaler_lbvserver_connections_active` | `senhub_netscaler_lbvserver_connections_active` |

The collector applies the standard `senhub_` prefix loss when going
through `prometheusremotewrite`, but the **dimensions** (probe_name,
probe_type, semantic-convention attributes like `cpu.mode`,
`system.memory.state`, `hw.state`) flow through identically. Aliasing
both paths under one PromQL query is straightforward.

## Logs signal

The OTLP strategy ships logs from these probe sources:

- **`syslog` probe** — every received syslog message becomes an OTel
  log record with severity mapped per the standard RFC 5424 → OTel
  table (info → INFO, error → ERROR, emergency → FATAL4, etc.) and
  attributes `syslog.facility`, `syslog.hostname`, `syslog.appname`,
  …
- **`event` probe** — every accepted HTTP event becomes a log record
  with severity from the event's `severity` field (EMERG..DEBUG)
  and the payload fields under the `senhub.event.*` namespace.
- **`linux_logs` probe** — reads the local systemd journal via
  `journalctl --output=json --follow` and emits a log per entry
  with `host.name`, `systemd.unit`, `syslog.appname`,
  `process.pid`, … See [linux_logs probe](probes/linux-logs.md).

Probes still emit DataPoints to the existing PRTG/Nagios/event
strategies; the logs path is **additive**.

## Severity mapping

| Source value | OTel SeverityNumber | OTel SeverityText |
|---|---|---|
| Syslog 0 — emergency | 24 | FATAL4 |
| Syslog 1 — alert     | 23 | FATAL3 |
| Syslog 2 — critical  | 22 | FATAL2 |
| Syslog 3 — error     | 17 | ERROR |
| Syslog 4 — warning   | 13 | WARN |
| Syslog 5 — notice    | 10 | INFO2 |
| Syslog 6 — info      | 9  | INFO |
| Syslog 7 — debug     | 5  | DEBUG |

Same table is used by `linux_logs` (PRIORITY field of the journal
entry) and the `event` probe (when the operator-supplied severity
string maps to one of the eight standard names).

## Routing probes to OTLP

Every metric probe routes to OTLP automatically when an `otlp` storage
is configured — the strategy is included in the default target list
alongside `senhub`, `prtg`, `http`. There's nothing to wire per probe.

The data_store router silently skips strategies that aren't
configured, so probes that target `[senhub, prtg, http, otlp]` continue
to work in agents where the operator has not enabled OTLP.

## Receiver setups

### OpenTelemetry Collector (recommended)

A minimal collector that accepts OTLP/gRPC and forwards to
VictoriaMetrics + VictoriaLogs:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "0.0.0.0:4317"

exporters:
  prometheusremotewrite:
    endpoint: "http://vm.internal:8428/api/v1/write"
    resource_to_telemetry_conversion:
      enabled: true

  otlphttp/logs:
    logs_endpoint: "http://victorialogs.internal:9428/insert/opentelemetry/v1/logs"

service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      exporters: [otlphttp/logs]
```

`resource_to_telemetry_conversion: enabled: true` ensures the OTel
resource attributes (service.name, service.instance.id, etc.) become
Prometheus labels rather than being discarded — usually what operators
want.

### Single public intake for a fleet (collector-fronted)

The recommended pattern for a fleet of agents across sites: every agent —
local or remote — pushes its whole OTLP stream (metrics, logs, entities) to
**one** public HTTPS endpoint, fronted by a reverse proxy that terminates TLS
and forwards to a central collector. The collector authenticates the bearer
token and fans out to the storage backends.

Agent-side, the entire client configuration is one strategy fragment:

```yaml
# strategies.d/10-otlp.yaml
otlp:
  endpoint: "ingest.example.com:443"
  tls:
    enabled: true
  headers:
    Authorization: "Bearer ${env:OTLP_BEARER_TOKEN}"
  compression: gzip
  timeout: 60s
  signals:
    metrics:
      enabled: true
      interval: 30s
    logs:
      enabled: true
    entities:
      enabled: true
      interval: 5m
  resource:
    service.name: my-host-prod        # one value per agent
    deployment.environment: prod
```

Notes:

- **One URL, one token, three signals.** Entity events ride the logs
  transport — enabling `signals.entities` requires no extra endpoint.
- The bearer token comes from the environment (`${env:}`) or a root-only
  file (`${file:/etc/senhub-agent/bearer.token}`); never inline it.
- `host.id`, `host.name` and `os.*` resource attributes are auto-detected
  and attached to every signal — don't set them manually.
- gRPC works through standard reverse proxies. nginx needs one location for
  OTLP/gRPC and one for OTLP/HTTP (the agent uses the standard `/v1/*`
  paths):

```nginx
server {
    listen 443 ssl;
    server_name ingest.example.com;

    # OTLP/gRPC -> collector
    location /opentelemetry.proto.collector. {
        grpc_pass grpcs://127.0.0.1:4317;
    }
    # OTLP/HTTP -> collector
    location /v1/ {
        proxy_pass http://127.0.0.1:4318;
    }
}
```

Authentication belongs on the collector (for example the `bearertokenauth`
extension), keeping the proxy a pure transport. To feed a second consumer
(another OTLP backend, an entity-graph store, ...), fan out **at the
collector** — add a second exporter there — rather than configuring a
second OTLP strategy on every agent.

### vmagent's native OTLP endpoint

vmagent v1.95+ ingests OTLP/gRPC directly. Configure the agent to
push at `vmagent.internal:4317` — no collector needed.

### Grafana Cloud OTLP

Grafana Cloud accepts OTLP push at a public gRPC endpoint. Get the
endpoint URL and ingest token from the Grafana Cloud console:

```yaml
storage:
  - name: otlp
    params:
      endpoint: "otlp-gateway-prod-eu-west-2.grafana.net:443"
      headers:
        Authorization: "Basic eW91...="   # from Grafana Cloud console
      tls:
        enabled: true
```

## Agent self-observability

The agent's existing `senhub_agent_*` metrics (uptime, probe counts,
collect errors, HTTP requests, build info) are emitted on the
**Prometheus path only** — the OTLP path does not push agent self-
metrics, to avoid feedback loops where a failing OTLP export would
itself emit metrics that need to go through OTLP.

The collector's own self-metrics (`otelcol_exporter_sent_*`,
`otelcol_receiver_accepted_*`) cover the OTLP transport health from
the receiver side.

## Monitoring the OTLP pipeline

The agent maintains its own snapshot of OTLP pipeline counters that
you can read without scraping Prometheus, via three surfaces:

- **CLI** — `senhub-agent status --otlp` appends a four-section block
  (Pipeline / Store & Export / Checkpoint / Parallel) to the standard
  status view. See the [CLI reference](cli.md#status).
- **Web dashboard** — when the HTTP strategy is enabled the dashboard
  renders an **OTLP Pipeline** card with the same four sections,
  refreshed every 30 seconds. See [Web Interface](web-interface.md#otlp-pipeline-card).
- **JSON endpoint** — `GET /api/{agentkey}/info/otlp` returns the same
  data as a single JSON snapshot. Useful for custom dashboards or
  external alerting. The field reference lives in
  [OTLP observability](https://github.com/senhub-io/senhub-agent/blob/dev/docs/admin-guide/OTLP-OBSERVABILITY.md) (admin guide).

### What to look at

| Symptom | What to check |
|---|---|
| Operator dashboards lag the agent by minutes | `Export last` and `Export mean` durations — sustained values close to the configured `timeout` (default 60 s) mean the sink is the bottleneck. |
| Series are missing without an explicit error | `Dropped` total and its `by_reason` breakdown. `probe_cardinality` = the per-probe budget was hit; `store_cap` = the global cap; `memory_soft_limit`/`memory_hard_limit` = the agent shed load to stay under its memory budget. |
| Recovery after a restart looks slow | `Checkpoint.Restored entries` — if 0 with persistence enabled, the checkpoint wasn't found or failed to parse (check `Errors` by stage). |
| Throughput plateaus despite headroom on the collector | `Parallel.Sub-batches` — value of 1 means the single-batch path; > 1 means the per-probe fan-out is firing. |

### Cardinality controls

If `Dropped by probe_cardinality` keeps climbing, lift the per-probe
budget in your strategy YAML:

```yaml
otlp:
  endpoint: "otel-collector.internal:4317"
  max_active_series_per_probe: 20000   # default 10000
  max_store_size: 100000               # default 50000 (global cap)
```

Both knobs are gauges of last resort — they protect the agent's memory
under runaway-cardinality conditions. Raise them only when you know
what's generating the series, otherwise the agent will happily eat
RAM tracking churning timeseries.

## Backward compatibility

- **Existing storages** (PRTG, Nagios, SenHub, HTTP/Prometheus) are
  unchanged. Adding an `otlp` block is purely additive.
- **Probe configurations** (`type: cpu`, `type: redfish`, etc.) work
  identically. The OTel mapping comes from the bundled YAML
  transformer definitions.
- **No new probe types are required to push to OTLP** — every existing
  probe routes through the OTLP strategy when it's configured.

## Troubleshooting

### Agent starts but nothing reaches the collector

Check the agent log for:

```
strategy.otlp WRN OTLP metrics export failed   error=...
```

Common causes:

- **Wrong endpoint or unreachable host**. The agent uses lazy gRPC
  connect — the failure surfaces only on the first push attempt
  (after `metrics.interval`).
- **TLS handshake failure**. Set `insecure_skip_verify: true`
  temporarily to confirm certificate verification is the cause.
- **Authentication rejected by gateway**. Check the collector / vmagent
  / Grafana Cloud auth log; verify the `headers:` block.

### Metrics appear with unexpected names in VictoriaMetrics

The default VictoriaMetrics OTLP-to-Prometheus translation strips
the `senhub_` prefix you might see in the Prometheus exposition.
That's intentional — the OTLP path uses the canonical OTel names
(`system.cpu.utilization`, not `senhub_system_cpu_utilization`).
Add `resource_to_telemetry_conversion: enabled: true` to ensure
resource attributes (service.name, deployment.environment) are
preserved as labels.

### Some metrics are missing

Check the agent log for:

```
strategy.otlp WRN Metric has no OTel mapping — not exported via OTLP.
```

Same policy as the Prometheus path: a probe metric without an
`otel:` block in its YAML definition is silently skipped, and the
agent warns once per `(probe_type, metric_name)`. Add the mapping or
`otel.skip: true` directive in `definitions/<probe>.yaml` and
restart.

### Logs aren't arriving

Verify:

1. `signals.logs.enabled: true` in the storage params.
2. The collector pipeline includes the `otlp` receiver in the `logs:`
   pipeline (not just `metrics:`).
3. The probes you expect to produce logs are configured: `syslog`,
   `event`, or `linux_logs`. Other probes do not produce log records.
4. The receiver tolerates the OTel log data model; older versions of
   vmagent or Loki collectors may need a recent build.

### Reverse the agent → push direction

If you want a pull-based scrape model instead of push, use the
[Prometheus endpoint](prometheus/index.md) — same data,
same names, different transport.
