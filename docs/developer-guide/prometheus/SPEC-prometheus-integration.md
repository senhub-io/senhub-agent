# SenHub Agent — Native Prometheus / VictoriaMetrics integration

**Spec version:** 1.1
**Target:** Prometheus 2.x / VictoriaMetrics (vmagent, single-node, cluster)
**Status:** ready for audit + implementation
**Spec author:** Matthieu Noirbusson

---

## 0. Integration principles (identical to the Zabbix output)

This spec describes the **expected result**: a `/metrics` endpoint in Prometheus
exposition format, served by the agent's HTTP server. It describes what must come
out, not how it is plumbed internally.

**Mandatory integration rules:**

1. The Prometheus output is a **new consumer of the internal bus**, on the same
   footing as the cloud and Zabbix outputs.
2. Reuse the repo's **existing conventions**.
3. Transformation: **same pattern** as the other outputs.
4. **Strictly additive**: no change to the probes or to the existing outputs.
5. Where the spec contradicts the code, the code wins.

→ If the Phase 0 audit was already done for the Zabbix output, it serves as the
foundation.

## 1. Terminology

| Term | Definition |
|---|---|
| **Probe** | A collector type (netscaler, citrix_cvad, vmware…) |
| **Probe instance** | A probe instantiated in the config under a unique name (e.g. `lb-prod-paris` is an instance of the `netscaler` probe) |
| **Agent** | The SenHub Agent process running N probe instances |

## 2. Context and motivation

The Prometheus/VictoriaMetrics ecosystem is the de-facto standard for
observability, and it is the basis of the SenHub Observability Platform stack
(VictoriaMetrics + Grafana OSS).

Exposing the agent's metrics in Prometheus format allows:

- direct ingestion into VictoriaMetrics via vmagent or Prometheus
- use in Grafana with no intermediate layer
- compatibility with the whole ecosystem (alerting, recording rules, federation)
- dogfooding: monitoring the SenHub Agent with SenHub's own stack

**Goal**: add a `/metrics` route to the agent's HTTP server, exposing every
metric (agent + probes) in the standard Prometheus exposition format.

## 3. Shared HTTP server

The `/metrics` route is added to the **same HTTP server** as the Zabbix,
healthcheck and any other existing route. One port, one TLS/auth config.

Enabled by `prometheus_export.enabled: true`.

## 4. Non-goals (V1)

- Remote write push to VictoriaMetrics — V2
- A separate `/metrics` endpoint per probe (a Prometheus anti-pattern)
- Native Prometheus histograms (not relevant here)
- OpenTelemetry export (OTLP) — V3
- HTTP_SD service discovery — V2

## 5. Output format

### 5.1 Standard

Prometheus text exposition format (`text/plain; version=0.0.4; charset=utf-8`),
OpenMetrics-compliant.

Global prefix: `senhub_`.

### 5.2 Naming convention

Strict Prometheus rules:

- only `[a-zA-Z_:][a-zA-Z0-9_:]*`
- snake_case
- `senhub_` prefix
- unit suffix: `_seconds`, `_bytes`, `_total`, `_percent`, `_ratio`
- no `.` → replaced by `_`

**Internal key mapping:**

| Internal bus key | Prometheus metric | Type |
|---|---|---|
| `agent.uptime_seconds` | `senhub_agent_uptime_seconds` | gauge |
| `agent.probes.total` | `senhub_agent_probes_total` | gauge |
| `agent.probes.healthy` | `senhub_agent_probes_healthy` | gauge |
| `agent.collect.errors_total` | `senhub_agent_collect_errors_total` | counter |
| `agent.http.requests_total` | `senhub_agent_http_requests_total` | counter |
| `host.cpu.usage_percent` | `senhub_host_cpu_usage_percent` | gauge |
| (probe) `vserver.lb_app1.health` | `senhub_probe_health` with labels | gauge |
| (probe) `vserver.lb_app1.connections_active` | `senhub_probe_connections_active` with labels | gauge |

### 5.3 Labels

Labels carry the dimensions. No LLD, no host prototypes: labels only.

**Labels present on every probe metric:**

| Label | Source | Example |
|---|---|---|
| `probe_name` | probe instance name | `lb-prod-paris` |
| `probe_type` | probe type | `netscaler` |
| `group` | functional group of the metric | `vserver` |
| `subgroup` | subgroup (the specific object) | `lb_app1` |

**Optional labels (config tags):**

| Label | Source | Example |
|---|---|---|
| `env` | tag configured on the probe | `prod` |
| `site` | tag configured on the probe | `paris` |
| `client` | tag configured on the probe | `acme` |

Tags configured in the probe section (`tags: { env: prod, site: paris }`) are
propagated as Prometheus labels.

Note: `probe_name` and `probe_type` are used (rather than `instance_name`) to
avoid colliding with Prometheus's reserved `instance` label, which carries the
`host:port` of the scrape target.

### 5.4 Full output example

```
# HELP senhub_agent_uptime_seconds Agent uptime in seconds
# TYPE senhub_agent_uptime_seconds gauge
senhub_agent_uptime_seconds 84231

# HELP senhub_agent_probes_total Number of configured probe instances
# TYPE senhub_agent_probes_total gauge
senhub_agent_probes_total 3

# HELP senhub_agent_probes_healthy Number of healthy probe instances
# TYPE senhub_agent_probes_healthy gauge
senhub_agent_probes_healthy 3

# HELP senhub_agent_collect_errors_total Total number of collection errors
# TYPE senhub_agent_collect_errors_total counter
senhub_agent_collect_errors_total 0

# HELP senhub_agent_http_requests_total Total HTTP requests served
# TYPE senhub_agent_http_requests_total counter
senhub_agent_http_requests_total 1820

# HELP senhub_host_cpu_usage_percent Host CPU usage percentage
# TYPE senhub_host_cpu_usage_percent gauge
senhub_host_cpu_usage_percent 14.2

# HELP senhub_host_memory_used_percent Host memory usage percentage
# TYPE senhub_host_memory_used_percent gauge
senhub_host_memory_used_percent 38.5

# HELP senhub_probe_up Probe instance health (1=up, 0=down)
# TYPE senhub_probe_up gauge
senhub_probe_up{probe_name="lb-prod-paris",probe_type="netscaler",env="prod",site="paris",client="acme"} 1
senhub_probe_up{probe_name="lb-prod-lyon",probe_type="netscaler",env="prod",site="lyon",client="acme"} 1
senhub_probe_up{probe_name="cvad-axplora",probe_type="citrix_cvad",env="prod",client="axplora"} 1

# HELP senhub_probe_health Metric value (per vserver/service/object)
# TYPE senhub_probe_health gauge
senhub_probe_health{probe_name="lb-prod-paris",probe_type="netscaler",group="vserver",subgroup="lb_app1",env="prod",site="paris"} 1
senhub_probe_health{probe_name="lb-prod-paris",probe_type="netscaler",group="vserver",subgroup="lb_app2",env="prod",site="paris"} 1

# HELP senhub_probe_connections_active Active connections
# TYPE senhub_probe_connections_active gauge
senhub_probe_connections_active{probe_name="lb-prod-paris",probe_type="netscaler",group="vserver",subgroup="lb_app1",env="prod",site="paris"} 1247

# HELP senhub_probe_throughput_bytes Throughput in bytes per second
# TYPE senhub_probe_throughput_bytes gauge
senhub_probe_throughput_bytes{probe_name="lb-prod-paris",probe_type="netscaler",group="vserver",subgroup="lb_app1",env="prod",site="paris"} 12834000

# HELP senhub_probe_cpu_usage_percent CPU usage of monitored target
# TYPE senhub_probe_cpu_usage_percent gauge
senhub_probe_cpu_usage_percent{probe_name="lb-prod-paris",probe_type="netscaler",group="system",env="prod",site="paris"} 22.4
```

### 5.5 Textual metrics

Prometheus does not support textual metrics. Strategies:

1. **State metrics** (up/down) → numeric gauge (0/1) with a label:
   `senhub_probe_state{..., state="running"} 1`
2. **Info metrics** (version, firmware) → info metric:
   `senhub_probe_info{probe_name="...", version="1.2.3"} 1`
3. **Not convertible** → silently ignored (debug log)

### 5.6 Metric types

| Type | Use |
|---|---|
| `gauge` | Instantaneous value (default) |
| `counter` | Monotonically increasing counter |

In V1, gauge by default unless the bus metadata says counter.

## 6. Name transformation rules

A **deterministic and stable** mapping from internal key to Prometheus name:

1. `senhub_` prefix
2. `.` → `_`
3. `-` → `_`
4. drop disallowed characters
5. lowercase
6. append the unit suffix when known (`s` → `_seconds`, `%` → `_percent`,
   `B` → `_bytes`, `Bps` → `_bytes_per_second`, counter → `_total`)
7. de-duplication: two keys resolving to the same name is a config error → warn
   at startup

**Critical cardinality rule**: dimensions go in the labels, not in the name. A
vserver is `senhub_probe_connections_active{subgroup="lb_app1"}`, never
`senhub_probe_vserver_lb_app1_connections_active`.

## 7. Authentication

The same auth as the Zabbix routes. Two modes are supported for scraper
compatibility:

1. **Header**: `Authorization: Bearer <token>` (Prometheus ≥ 2.27, vmagent)
2. **Query parameter**: `GET /metrics?token=<token>` (fallback for older
   scrapers)

The query parameter is only checked when the header is absent. Comparison is
constant-time.

## 8. Agent configuration

```yaml
prometheus_export:
  enabled: true
  expose_host_metrics: false
  metric_prefix: "senhub"
  include_probe_tags: true
```

No port and no auth here: those come from the agent's HTTP config.

## 9. Internal architecture — directives

- **Snapshot**: if the Zabbix output maintains a snapshot, Prometheus **reuses**
  it. Not two parallel caches.
- **Serialization**: text exposition generated on each scrape from the snapshot.
  No serialization cache in V1.
- **Library**: `client_golang/prometheus` if already in the project, otherwise
  manual serialization is acceptable (trivial, fewer dependencies). The audit
  decides.
- **Package**: next to the Zabbix output.

## 10. Tests

- Unit: serialization, naming, labels, textual filtering
- Integration: mocked bus → `GET /metrics` → body parsable by
  `expfmt.TextParser`
- Bench: 50 probes × 200 metrics < 20 ms serialization
- Non-regression: cloud and Zabbix outputs unchanged

## 11. Documentation to deliver

- `docs/prometheus-integration.md` (user guide)
- `docs/prometheus-metrics-reference.md` (metric list, types, labels)
- `CHANGELOG.md`

## 12. Acceptance criteria

- [ ] Existing outputs **unmodified**
- [ ] `GET /metrics` returns parsable text exposition
- [ ] Agent metrics `senhub_agent_*` present
- [ ] Probe metrics `senhub_probe_*` with `probe_name`, `probe_type`, `group`,
      `subgroup` labels
- [ ] Config tags propagated as labels
- [ ] Textual metrics filtered without error
- [ ] Scraped by vmagent/Prometheus → visible in VictoriaMetrics/Grafana
- [ ] Package coverage ≥ 80%
- [ ] Documentation delivered

## Appendix A — PromQL / MetricsQL queries

```promql
# Every probe of one type
senhub_probe_up{probe_type="netscaler"}

# Connections per vserver, Paris site
senhub_probe_connections_active{site="paris", group="vserver"}

# Healthy / total probe ratio
senhub_agent_probes_healthy / senhub_agent_probes_total

# Alert: probe down for 5 minutes
senhub_probe_up == 0  # for 5m

# Top 5 vservers by connections
topk(5, senhub_probe_connections_active{group="vserver"})
```

## Appendix B — Cardinality

- 1 agent, 10 probes, 50 metrics/probe = ~510 series
- 1 agent, 50 probes, 200 metrics/probe = ~10,010 series

Modest. The only risk is dynamic values in config tags (`session_id`…) —
document that anti-pattern.
