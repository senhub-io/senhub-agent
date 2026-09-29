# Prometheus / VictoriaMetrics

SenHub Agent exposes its metrics in **Prometheus text exposition format**
(version 0.0.4) on a `/metrics` endpoint, ready to be scraped by Prometheus,
VictoriaMetrics (`vmagent` or `victoria-metrics` direct scrape), Grafana
Agent / Alloy, or any compatible scraper.

The exposition is **OpenTelemetry-aligned**: metric names follow OTel
semantic conventions where they apply (`system.cpu.*`, `system.memory.*`,
`system.network.*`, `system.filesystem.*`, `hw.*`), and use `senhub.*`
extensions for vendor-specific domains (NetScaler, Citrix, Veeam, Redfish).
The native [OTLP push exporter](../otlp.md) produces the **same** names
without any query rewrite.

## Quick start

1. Enable the `prometheus` endpoint of the HTTP output, in
   `strategies.d/00-http.yaml`:

```yaml
http:
  bind_address: "0.0.0.0"
  port: 8080
  endpoints: [prtg, web, prometheus]   # add "prometheus" to the list
```

2. The agent reloads its configuration when the file changes; no
   restart is needed. Two routes are now exposed:

| Route | Auth |
|---|---|
| `GET /metrics` | `Authorization: Bearer <agent-key>` header **or** `?token=<agent-key>` query parameter |
| `GET /api/{agent-key}/prometheus/metrics` | Agent key embedded in URL (SenHub pattern) |

3. Verify the output:

```bash
curl -H "Authorization: Bearer YOUR-AGENT-KEY" \
     http://your-agent:8080/metrics | head -30
```

You should see metric families like `senhub_agent_uptime_seconds`,
`senhub_system_cpu_utilization_ratio`, etc. with `# HELP` / `# TYPE`
headers.

## Configuration block

Beyond enabling the endpoint, an optional `prometheus:` sub-block tunes
the behavior:

```yaml
http:
  endpoints: [prtg, web, prometheus]
  prometheus:
    include_probe_tags: true        # default — propagate custom_tags as labels
    expose_host_metrics: true       # default — expose cpu/memory/network/filesystem
```

### `include_probe_tags` (default: `true`)

When `true`, every `custom_tags` entry declared on a probe is propagated as a
Prometheus label on that probe's metrics:

```yaml
# probes.d/20-netscaler.yaml, a YAML array of probes
- name: netscaler-prod-paris
  type: netscaler
  params:
    base_url: "https://netscaler.example.com"
    username: "monitoring-user"
    password: ${secret:netscaler-prod-paris.password}
  custom_tags:
    env: prod
    site: paris
```

All series for this probe then carry `env="prod"` and `site="paris"`. Set to
`false` if you'd rather keep the label set minimal and inject those
dimensions via `metric_relabel_configs` on the scraper side.

### `expose_host_metrics` (default: `true`)

When `false`, metrics from probes that observe the **local agent host**
(`cpu`, `memory`, `network`, `logicaldisk`) are filtered out of `/metrics`.
Use this when you already run a `node_exporter` on the same host and want
to avoid duplicate series. Probes that observe **remote** targets
(NetScaler, Citrix, Redfish, Veeam, ping_*) are unaffected.

## Authentication

The `/metrics` route accepts the agent key via two mechanisms — both are
constant-time validated:

**Bearer header** *(preferred — used by Prometheus, vmagent, Grafana Agent
out of the box):*

```
GET /metrics HTTP/1.1
Authorization: Bearer 9f4e2c8a-7b3d-...
```

**Query parameter** *(fallback for older scrapers without header support):*

```
GET /metrics?token=9f4e2c8a-7b3d-...
```

Without a valid token, the server replies `HTTP 401` with a
`WWW-Authenticate: Bearer realm="senhub-agent"` response header.

The `/api/{agent-key}/prometheus/metrics` route uses the standard SenHub
URL-embedded key auth — convenient when scripting with the same key as
PRTG / Nagios endpoints.

## Naming conventions

The exposition follows the **official OTel → Prometheus compatibility
specification**:

| OTel | Prometheus |
|---|---|
| `system.cpu.utilization` (gauge, ratio, `cpu.mode=user`) | `senhub_system_cpu_utilization_ratio{cpu_mode="user"}` |
| `system.memory.usage` (updowncounter, bytes, `system.memory.state=used`) | `senhub_system_memory_usage_bytes{system_memory_state="used"}` |
| `system.network.io` (gauge, By/s, `network.io.direction=receive`) | `senhub_system_network_io_bytes_per_second{network_io_direction="receive"}` |
| `senhub.netscaler.lbvserver.connections.active` | `senhub_netscaler_lbvserver_connections_active` |

Rules applied:
- **Prefix** `senhub_` (frozen, non-configurable)
- Dots → underscores (in names AND labels)
- Unit suffixes: `s` → `_seconds`, `By` → `_bytes`, `1` → `_ratio` (only
  on gauges — status/state metrics emitted as updowncounter keep the
  bare name without `_ratio`), `bit/s` → `_bits_per_second`,
  `By/s` → `_bytes_per_second`
- Counters get `_total` suffix when not already present
- Annotated units in braces (`{packet}`, `{error}`) drop the brackets

See the [metrics reference](metrics-reference.md) for the metrics of the
agent itself and of the system, network, active-check, SNMP, hardware and
vendor probes.

## Strict OTel for status enums

Health and state metrics (drive health, NetScaler vServer state, Veeam
job status, etc.) are emitted in **strict OTel form**: one data point
per possible state with value `1` (active) or `0` (inactive).

For example, a healthy physical disk produces:

```
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="ok"} 1
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="degraded"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="failed"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="predicted_failure"} 0
```

This matches the OTel spec for `hw.status` and lets you alert on
`hw_state="failed"` directly without numeric-enum gymnastics.

## Agent self-observability

These metrics describe the agent's own state. Uptime, cache entries,
the three probe gauges, the transformer fallback counter and
`senhub_agent_build_info` are emitted on every scrape once the endpoint
is enabled; `senhub_agent_collect_errors_total` and
`senhub_agent_http_requests_total` carry one series per label set that
has counted something, so a probe that never failed has no
`collect_errors` series. The four `senhub_agent_otlp_receiver_*`
counters appear only when an `otlp_receiver` probe runs and has taken
traffic.

| Metric | Type | Description |
|---|---|---|
| `senhub_agent_uptime_seconds` | gauge | Process uptime since start |
| `senhub_agent_cache_entries` | gauge | Distinct time series in the shared cache |
| `senhub_agent_probes_active` | gauge | Probes that have emitted ≥1 datapoint in the cache window |
| `senhub_agent_probes_total` | gauge | Configured probes currently running |
| `senhub_agent_probes_healthy` | gauge | Probes reporting `IsHealthy() == true` |
| `senhub_agent_collect_errors_total` | counter | Probe collection errors since start, with `probe` (type) and `reason` (collect/timeout/route) labels |
| `senhub_agent_transformer_fallback_total` | counter | Datapoints processed without a transformer definition (no unit injection or corrections) |
| `senhub_agent_http_requests_total` | counter | HTTP requests served, with `endpoint` label (route template) |
| `senhub_agent_build_info` | gauge (=1) | `version` and `commit` labels |
| `senhub_agent_otlp_receiver_ingested_total` | counter | Items accepted by the OTLP receiver, per `signal` |
| `senhub_agent_otlp_receiver_received_total` | counter | Records relayed by the OTLP receiver, by `signal`, `origin` and `service_name` |
| `senhub_agent_otlp_receiver_received_without_host_id_total` | counter | Relayed records that still had no `host.id` |
| `senhub_agent_otlp_receiver_dropped_total` | counter | Items the OTLP receiver discarded, by `signal` and `reason` |

The exposition also carries the counters of the OTLP output
(`senhub_agent_otlp_*`, at zero when no OTLP output is configured), the
agent process metrics (`senhub_agent_process_*`), and series that
appear only once they have something to report: per-reason drop,
rejection and export-failure counters, a failed output, a refused
licence, a configuration watch that is off. See the
[metrics reference](metrics-reference.md#agent-self-observability).

Build-info usage example (for dashboard joins):

```promql
senhub_agent_uptime_seconds
  * on(instance) group_left(version) senhub_agent_build_info
```

## Scrape configuration examples

### Prometheus

```yaml
scrape_configs:
  - job_name: 'senhub-agent'
    scrape_interval: 30s
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials: 'YOUR-AGENT-KEY'
    static_configs:
      - targets: ['agent.internal:8080']
        labels:
          datacenter: 'paris-dc1'
          client: 'acme'
```

### VictoriaMetrics (vmagent or victoria-metrics direct scrape)

100 % compatible with the Prometheus block above. For very large agents
add `stream_parse: true` to keep memory bounded:

```yaml
scrape_configs:
  - job_name: 'senhub-agent'
    scrape_interval: 30s
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/secrets/senhub-agent-key
    static_configs:
      - targets: ['agent.internal:8080']
    stream_parse: true
```

### Grafana Agent / Alloy

```river
prometheus.scrape "senhub" {
  targets         = [{"__address__" = "agent.internal:8080"}]
  forward_to      = [prometheus.remote_write.victoria.receiver]
  scrape_interval = "30s"
  bearer_token    = sys.env("SENHUB_AGENT_KEY")
}
```

### TLS

When the agent is configured with `tls.enabled: true` (see
[HTTP/HTTPS](../http-https.md)), set `scheme: https` on the scrape job
and keep the target as `host:port`, without an `https://` prefix. Add a
`tls_config` block for the certificate:

```yaml
scrape_configs:
  - job_name: 'senhub-agent'
    scheme: https
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/secrets/senhub-agent-key
    tls_config:
      ca_file: /etc/prometheus/senhub-agent-ca.pem
      # insecure_skip_verify: true   # lab only, for a self-signed certificate
    static_configs:
      - targets: ['agent.internal:8443']
```

## Sample PromQL queries

```promql
# Agent uptime in hours
senhub_agent_uptime_seconds / 3600

# Per-core CPU utilization (Linux/Unix)
senhub_system_cpu_utilization_ratio{cpu_mode!=""}

# Memory used percentage
senhub_system_memory_utilization_ratio * 100

# Network throughput per interface (instantaneous gauge in By/s)
sum by (network_interface_name, network_io_direction) (
  senhub_system_network_io_bytes_per_second
)

# Filesystems above 80 % usage
senhub_system_filesystem_utilization_ratio{system_filesystem_state="used"} > 0.8

# Hardware health alarms (any drive/PSU/controller in failed state)
senhub_hw_status{hw_state="failed"} == 1

# Veeam jobs that failed on last run
senhub_veeam_jobs_by_last_result{senhub_veeam_job_last_result="failed"} > 0

# NetScaler vServers DOWN
senhub_netscaler_lbvserver_status{senhub_netscaler_lbvserver_state="down"} == 1

# Agent collect-error rate (issues per minute, all probes/reasons)
sum(rate(senhub_agent_collect_errors_total[5m])) * 60

# Break the same rate down by probe type and failure reason
sum by (probe, reason) (rate(senhub_agent_collect_errors_total[5m])) * 60
```

## Troubleshooting

### Endpoint returns 401

Make sure the `Authorization: Bearer …` header (or `?token=…` query
parameter) carries the **exact** key printed by
`senhub-agent key show`. Comparison is constant-time and case-sensitive.

### A probe is configured but its metrics are missing

Check the agent log for messages of the form:

```
WRN Metric has no OTel mapping - not exposed in /metrics. Add an
otel: block to the probe YAML or otel.skip: true to silence.
```

This means the probe emitted a metric that has no `otel:` mapping in
its YAML transformer definition. The agent **never blocks** on this — it
silently drops the unmapped metric and warns once per
`(probe_type, metric_name)` pair. Add the mapping or skip directive in
the relevant `definitions/<probe>.yaml` and restart.

### Host-level metrics are missing

Verify `expose_host_metrics: true` (default) in the `prometheus` block of the `http` output.
When set to `false`, all `cpu`, `memory`, `network` and `logicaldisk` probe
metrics are filtered out — confirm this is what you want.

### Verify the format

The fastest way to confirm the exposition is well-formed:

```bash
curl -sf -H "Authorization: Bearer KEY" http://agent:8080/metrics \
  | promtool check metrics
```

Any parse error will be reported. Our test suite also runs every emitted
record through `expfmt.TextParser` (the canonical Prometheus parser) on
every CI build — so a malformed body in production almost certainly
indicates a bug worth opening an issue for.
