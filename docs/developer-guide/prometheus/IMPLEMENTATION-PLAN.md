# Prometheus Export — Implementation Plan

**Status:** delivered. Kept as the design record for the OTel-first mapper architecture it introduced; the five phases shipped, so the plan itself is historical.
**Spec:** [SPEC-prometheus-integration.md](./SPEC-prometheus-integration.md)
**Author:** Matthieu Noirbusson
**Date:** 2026-04-18

## 0. Executive summary

**The long-term ambition: an OTel-first SenHub Agent.** The internal semantic model becomes OpenTelemetry; every output (PRTG, Nagios, Prometheus, Zabbix tomorrow, OTLP) becomes a **mapper** translating OTel into the target format. PRTG/Nagios backwards compatibility is carried by the YAML, which maps the OTel name onto the existing channel names.

**The deliverable here (the Prometheus endpoint)**: the first OTel → Prometheus mapper — a pragmatic path that introduces the mapper architecture without breaking the existing probes.

The Prometheus output slots in as an **endpoint** of the existing HTTP strategy, not as a new strategy. The handler reads the shared cache (`MetricCache`) and serialises text exposition on the fly, with no intermediate cache.

Audit findings that make the work smaller:

| Item | Current state |
|---|---|
| Route `/api/{key}/prometheus/metrics` | **Already registered**, stub handler → 501 |
| The `prometheus` endpoint in `validEndpoints` | **Already there** (`http_config.go:154-162`) |
| Data point cache | **Complete** (`CachedMetric.Tags` thread-safe, dynamic TTL) |
| Per-probe transformers | **One YAML per probe** with `name`, `channel`, `unit`, `multi_instance_labels` — extensible |

→ No deep refactor and no new strategy. The main work is the serialiser and the naming tables.

## 1. Target architecture

```
┌─────────────────────┐
│ DataStore           │
└──────────┬──────────┘
           │ data points
           ▼
┌─────────────────────┐     ┌─────────────────────┐
│ HTTPSyncStrategy    │────▶│ MetricCache (TTL)   │
└──────────┬──────────┘     └──────────┬──────────┘
           │ route                     │ read
           ▼                           │
┌─────────────────────┐                │
│ HTTPHandlers        │                │
│  /prtg/metrics      │────────────────┤
│  /nagios/metrics    │────────────────┤
│  /prometheus/metrics│◀───────────────┘  (new)
│  /web/*             │
└─────────────────────┘
           │
           ▼
┌─────────────────────┐
│ PromSerializer      │   (new)
│  cache → text       │
└─────────────────────┘
```

**Principles:**
- No Prometheus-specific cache: serialisation happens on the fly from `MetricCache`.
- No `prometheus` strategy: it is an endpoint of the `http` strategy.
- Internal key → Prometheus name mapping lives in `transformers/definitions/<probe>.yaml`, in an additional `prometheus:` section per metric.

## 2. Routes

Dual route, same handler:

| Route | Auth | Usage |
|---|---|---|
| `GET /api/{agentkey}/prometheus/metrics` | AgentKey (the SenHub pattern) | Consistency with PRTG/Nagios |
| `GET /metrics` | Bearer `{agentkey}` *(header)* or `?token={agentkey}` *(query param)* | Standard Prom/vmagent |

The `/metrics` route **without** `/api/{key}/` honours the Prometheus convention. The token is validated in constant time against the existing `authentication_key`.

**Enabling it:** only enabling the `prometheus` endpoint in the config mounts the two routes.

## 3. Config

An extension of the existing v2 format, **strictly additive**:

```yaml
storage:
  - name: http
    params:
      endpoints: [prtg, web, nagios, prometheus]   # ← "prometheus" alone enables it
      prometheus:                                  # optional block, sensible defaults
        include_probe_tags: true                   # default: true (custom_tags → labels)
        expose_host_metrics: true                  # default: true (cpu/memory host via probes)
```

> Note: the `senhub_` prefix is fixed and not configurable. See §12, point 5.

Validation is handled by `ConfigurationManager.ValidateConfigParams()`, already in place for the `endpoints` list.

## 4. YAML transformers — the OTel-first model

**Architectural decision**: the `internal/agent/services/data_store/transformers/definitions/<probe>.yaml` files carry an **`otel:`** block per metric as the semantic source of truth. The outputs are mappers, derived or explicit.

**Target shape:**

```yaml
probe_name: netscaler
metrics:
  # The OTel section is the semantic source of truth
  - otel:
      name: senhub.netscaler.vserver.connections.active   # the senhub.* space, for domains OTel does not cover
      unit: "{connection}"
      type: gauge
      attributes: {}                                      # static attributes (constants)

    # Transitional: the current internal names the probe emits (legacy keys).
    # Goes away once the probe is reworked to emit OTel natively.
    source_keys: ["netscaler.vserver.client.connections"]

    # Dynamic mapping: probe tags → OTel attributes.
    # Tags present on the cached data point are translated into OTel attributes.
    tag_to_attribute:
      vserver: network.vserver.name                       # existing tag → OTel attribute

    # PRTG backwards compatibility (current fields unchanged)
    prtg:
      channel: vserver.client.connections
      display_name: "vServer Client Connections"
      category: vserver
      description: "Active client connections per vServer"

    # Nagios retro-compat (TBD during the Nagios audit)
    nagios: {}

    # Prometheus: NO section at all. Derived automatically from the OTel→Prom rules
    # (§5). Here: senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}
```

**Rules:**
- `otel.name`: the unique key. It follows the OTel semconv for covered domains (`system.*`), or the `senhub.*` extension for proprietary ones (netscaler, citrix, veeam…).
- `otel.unit`: the UCUM unit (`s`, `By`, `{connection}`, `1` for a ratio, and so on).
- `otel.type`: `counter`, `gauge`, `updowncounter`, `histogram` (V1: gauge/counter).
- `otel.attributes`: constant attributes (e.g. `cpu.mode: user`).
- `source_keys`: the internal bus keys the probe currently emits that feed this OTel metric. **Transitional** — removed once the probe emits OTel natively.
- `tag_to_attribute`: probe tag → OTel attribute mapping. Tag values are propagated as attribute values.
- `prtg`, `nagios`: unchanged from the current format (strict backwards compatibility).
- When `otel:` is absent from a metric, it is **silently not emitted** in `/metrics`, with a **WARN logged once** per (probe_type, metric_name). The endpoint keeps working and the agent does not block (Q4 §12, revised 2026-04-21).

**The explicit `otel.skip` opt-out** — some metrics must NOT be exposed in Prometheus: the log/event conduit probes, which emit no semantic metrics.

```yaml
otel:
  skip: true
  reason: "Event conduit probe — relayed events, not metrics. Future: OTLP log export."
```

The Prometheus mapper ignores these metrics. The `prtg:` / `nagios:` fields stay active for backwards compatibility. The "no metric without a mapping" contract still holds: `skip` IS an explicit mapping, documented and auditable.

**The `otel.expand` enum expansion** — for "health/state" metrics whose value is a numeric enum (from a lookup) that strict OTel requires as one data point per state (`hw.status`, for example), the mapper **expands** automatically:

```yaml
otel:
  name: hw.status
  unit: "1"
  type: updowncounter
  attributes:
    hw.type: physical_disk
  expand:
    attribute: hw.state
    mapping:                  # state_name → raw lookup code
      ok: 0                   # sfs.redfish.health value 0 (OK) → hw.state=ok=1
      degraded: 1             # value 1 (Warning) → degraded=1
      failed: 2               # value 2 (Critical) → failed=1
      unknown: 3              # value 3 (Unknown) → unknown=1 (extension value)
```

**Emission behaviour:** for each cached data point (whose value is the enum code from the lookup), the mapper emits **N data points**, one per `mapping` entry:
- value = **1** when the current code matches the value associated with that state
- value = **0** otherwise
- the `<attribute>` attribute = the state name

A concrete example — a drive in the "ok" state (code 0) emits 4 series:
```
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="ok"} 1
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="degraded"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="failed"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="predicted_failure"} 0
```

**Rationale**: OTel compliance lives in the mapper. Future exports — native OTLP to VictoriaMetrics OTel, Grafana OTel — have nothing to correct, because the data is already strict OTel on the way out.

## 4bis. The SenHub OTel semantic convention

For domains the OTel semconv does not cover (netscaler, citrix, veeam, redfish, the webapp probes…), we **create an extension under the `senhub.*` namespace**, documented in `docs/developer-guide/otel/senhub-semantic-conventions.md`.

Example target names:
- `system.cpu.time`, `system.memory.usage`, `system.network.io` (native OTel)
- `senhub.netscaler.vserver.connections.active`, `senhub.netscaler.system.cpu.utilization`
- `senhub.citrix.session.count`, `senhub.citrix.delivery_group.machines.registered`
- `senhub.veeam.job.status`, `senhub.veeam.repository.capacity.bytes`
- `senhub.redfish.drive.temperature.celsius`, `senhub.redfish.psu.power.watts`

**Attributes**: aligned with OTel where possible (`network.interface.name`, `system.device`), extended under `senhub.*` otherwise (`senhub.vserver.name`, `senhub.citrix.delivery_group.name`).

That document was written alongside Phase 0.5, as the reference.

## 5. OTel → Prometheus conversion rules

Per the [OTel compatibility spec](https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/):

1. **A `senhub_` prefix** prepended to the OTel name, whatever the OTel namespace (`system.` or `senhub.`).
2. **Dots → underscores** in the name and the attributes (`system.cpu.time` → `system_cpu_time`, `cpu.mode` → `cpu_mode`).
3. **Disallowed characters** (the Prometheus regex `[a-zA-Z_:][a-zA-Z0-9_:]*`) replaced by `_`, with consecutive underscores collapsed.
4. **Unit suffix**:
   - `s` → `_seconds`
   - `By` → `_bytes`
   - `Hz` → `_hertz`
   - `1` (ratio) → `_ratio`
   - `{connection}`, `{packet}` and other braced units → dropped
   - `foo/bar` → `_foo_per_bar`
5. **Counter suffix**: counters get `_total` when they do not already end in it (`system_cpu_time_seconds_total`).
6. **OTel attributes → Prometheus labels**: every data point attribute, with `cpu.mode` → `cpu_mode` and so on.

Deterministic examples:
| OTel | Prometheus |
|---|---|
| `system.cpu.time` (counter, `s`, `cpu.mode=user`) | `senhub_system_cpu_time_seconds_total{cpu_mode="user"}` |
| `system.memory.usage` (updowncounter, `By`, `system.memory.state=used`) | `senhub_system_memory_usage_bytes{system_memory_state="used"}` |
| `senhub.netscaler.vserver.connections.active` (gauge, `{connection}`, `network.vserver.name=lb_app1`) | `senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}` |
| `system.cpu.utilization` (gauge, `1`, `cpu.mode=user, cpu.logical_number=0`) | `senhub_system_cpu_utilization_ratio{cpu_mode="user",cpu_logical_number="0"}` |

## 5bis. Labels present on every probe metric

On every probe metric we **add**, on top of the OTel attributes:

| Label | Source | Example |
|---|---|---|
| `probe_name` | instance name (config) | `citrix-prod-paris` |
| `probe_type` | the registry type | `citrix` |
| *custom_tags labels* | the probe's `custom_tags`, when `include_probe_tags: true` | `env=prod, site=paris` |

Prometheus's reserved `instance` label is never emitted by the agent — it would clash with the scrape target's own.

## 6. The source of truth is OTel

See §4 and §4bis. No more generic `group`/`subgroup` labels: the OTel attributes carry the semantic information (`cpu.mode`, `network.vserver.name`, and so on).

## 7. Serialisation

**Choice:** manual serialisation, not `client_golang/prometheus`.
- Avoids a dependency that was not already in `go.mod`.
- Text exposition v0.0.4 is trivial to write correctly.
- Full control over ordering, grouping and HELP/TYPE.
- Automated test: round-trip parsing via `github.com/prometheus/common/expfmt` *(test-only, not a runtime dependency)*.

**Target package:** `internal/agent/services/data_store/strategies/http/prometheus/`
- `serializer.go` — converts `CachedMetric` into text-exposition lines
- `names.go` — name resolution from the YAML transformer, plus the fallback
- `handler.go` — HTTP handler (dual route)
- `auth.go` — validation Bearer + query param
- `serializer_test.go`, `names_test.go`, `handler_test.go`

## 8. Handling textual metrics

Spec §5.5 sets out 3 strategies. The decision:

| Source | Handling |
|---|---|
| A numeric value (float, int, bool) | Emitted directly (bool → 0/1) |
| A string value identified as a state (`Up/Down`, `Running/Stopped`…) | Converted through the YAML's `lookup:` → `senhub_*_state{state="up"} 1` |
| A version/firmware string | An info metric `senhub_probe_info{version="..."} 1` when declared as `prometheus.type: info` |
| Any other non-convertible string | Silently ignored, with a debug log — never a scrape error |

## 9. Agent metrics (host-level)

Beyond the probe metrics, the agent exposes its own operational metrics:

| Name | Type | Description |
|---|---|---|
| `senhub_agent_uptime_seconds` | gauge | Process uptime |
| `senhub_agent_probes_total` | gauge | Number of configured probe instances |
| `senhub_agent_probes_healthy` | gauge | Number of instances in a healthy state |
| `senhub_agent_collect_errors_total` | counter | Total collection errors |
| `senhub_agent_http_requests_total{endpoint=…}` | counter | HTTP requests served, per endpoint |
| `senhub_agent_cache_entries` | gauge | Number of entries in the cache |
| `senhub_agent_build_info{version=…, branch=…}` | gauge (value=1) | Build info |

No `probe_*` label on these. They are **always** emitted when `prometheus` is enabled.

## 10. Implementation phases

### Phase 0 — Plan approved
- [x] Audit of the cache structure, transformers and routes
- [x] Target architecture, config schema, group/subgroup vocabulary
- [ ] **Complete naming table for the 15 probes** *(deliverable 0.5)*
- [ ] User validation

### Phase 0.5 — OTel tables + retro-compat mapping *(blocking before Phase 1)*

**Step 0.5.a — survey the OTel community** *(a mandatory prerequisite for each probe)*

Before defining a `senhub.*` extension, always check whether a convention already exists:
- **The official OTel semconv**: [specs/semconv](https://github.com/open-telemetry/semantic-conventions) (system, HTTP, database, RPC, messaging, faas, and so on)
- **OTel Collector contrib**: [receivers](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver) (citrix: none to date; netscaler: none; veeam: none; redfish: exists → **align with it**)
- **De-facto vendor conventions**: the Grafana Labs, VictoriaMetrics, ObservIQ and DataDog integration documentation
- **Official Prometheus exporters**: [prometheus/community](https://github.com/prometheus-community) — long-standing exporters (redfish_exporter and others) whose conventions can inform our namespace

If a convention exists: adopt it as it is — attributes, units, types. If it is partial: extend it, honouring the existing prefixes. If none exists: create one under `senhub.*`, in the style of the official OTel conventions.

Every choice is traced in `senhub-semantic-conventions.md`, with its justification and the links consulted.

**Step 0.5.b — filling the YAML, batch by batch**

For **every** metric of **every** probe (15), write:
1. The `otel:` block (name, unit, type, static attributes)
2. `source_keys` (the mapping onto the current internal keys)
3. The `tag_to_attribute` block (existing tags → OTel attributes)
4. The `prtg:`/`nagios:` blocks, unchanged (backwards compatibility)

No fallback — an unmapped metric is silently not emitted in `/metrics`, with a WARN logged once per (probe_type, metric_name). The endpoint keeps working (Q4 revised — see §12).

Review batches (the recommended order, reviewed probe by probe):
1. **Light system** (4 probes): `cpu`, `memory`, `network`, `logicaldisk` — covered by the official OTel semconv
2. **Network** (4 probes): `ping_gateway`, `ping_webapp`, `load_webapp`, `wifi_signal_strength` — partially covered by the HTTP semconv
3. **Events** (2 probes): `syslog`, `event` — see the [log semconv](https://opentelemetry.io/docs/specs/semconv/logs/)
4. **Heavy domain** (4 probes): `netscaler`, `citrix`, `redfish`, `veeam` — lead with the community survey (redfish has a well-known Prometheus exporter)

For each batch: survey OTel → a YAML PR plus an update to `senhub-semantic-conventions.md` → naming review → user approval → merge → next batch.

### Phase 1 — OTel→Prom rules and the serialiser
- `otel_to_prom.go`: deterministic application of the §5 rules (dots→underscores, unit suffixes, `_total`, the `senhub_` prefix, and so on)
- `resolver.go`: resolving cached data points into an OTel metric via the YAML's `source_keys` and `tag_to_attribute`
- `serializer.go`: text exposition serialisation (HELP/TYPE/metric)
- Injecting the labels present on everything (`probe_name`, `probe_type`, custom_tags)
- Filtering out non-convertible textual metrics
- Tests: `expfmt.TextParser` round-trip, the OTel→Prom rules over 50+ cases, cardinality, edge cases

### Phase 2 — Handler HTTP + routes
- Implement `handlePrometheusMetricsGET()`, replacing the 501 stub
- Add the `/metrics` route (without `/api/{key}/`), with Bearer auth
- Integration tests: bus → cache → GET → parsable body

### Phase 3 — agent metrics and config
- An `AgentMetrics` collector for §9
- Parse `storage[].params.prometheus` (with defaults)
- Wiring at startup; PRTG/Nagios non-regression
- End-to-end test: config enabled → curl `/metrics` → grep `senhub_`

### Phase 4 — real validation with vmagent/Grafana
- A real vmagent scrape into VictoriaMetrics
- Verify the Grafana dashboards and PromQL
- Demonstration alerting rules

### Phase 5 — Documentation + code review + CHANGELOG *(blocking before merge)*
- **Complete user documentation**: `docs/user-guide/content/docs/prometheus/_index.md` (the integration guide), `metrics-reference.md` (the complete table of the 15 probes with name, type, group, labels and description), and scrape config examples
- **Exhaustive code review** of the `prometheus/` package (the `pr-review-toolkit:code-reviewer` agent plus a user review)
- **Non-regression**: PRTG/Nagios unchanged (automated test plus manual validation on production deployments)
- CHANGELOG + 0.1.88 release notes (major feature)

## 11. Definition of done

- [ ] `GET /metrics` returns a body parsable by `expfmt.TextParser` (automated test)
- [ ] `senhub_agent_*` metrics present
- [ ] Probe metrics carry names and attributes conforming to the §5 OTel→Prom rules
- [ ] Every `senhub.*` extension documented in `senhub-semantic-conventions.md`
- [ ] `probe_name`/`probe_type` labels on everything
- [ ] `custom_tags` propagated as labels when `include_probe_tags: true`
- [ ] Non-convertible textual metrics ignored, with no scrape error
- [ ] PRTG and Nagios unchanged (automated non-regression plus manual validation)
- [ ] `prometheus/` package coverage ≥ 80%
- [ ] Successfully scraped by vmagent and visible in Grafana
- [ ] Docs and changelog delivered

## 12. Decisions (questions settled)

1. **A `/metrics` route without the agentkey in the URL** → **yes**, a dual route is implemented. Bearer auth (header) or `?token=` (query parameter), validated in constant time against `authentication_key`. UI impact: Sensor Builder needs a Prometheus tab (PromQL plus a copy-paste scrape config) — added to the web-ui refactoring roadmap.

2. **`expose_host_metrics`** → **`true` by default, configurable**. An operator running node_exporter alongside, who wants to avoid the duplication, can set it to `false`.

3. **Info metrics (version/firmware)** → declared **explicitly** with `prometheus.type: info` in the YAML. No auto-detection.

4. **A naming fallback** → **none**, but **not blocking** *(Q4 revised 2026-04-21)*.
   - A metric with no `otel:` block is **not emitted** in `/metrics`
   - A **WARN is logged** with `probe_name`, `probe_type`, `metric_name` and an actionable message ("Add an `otel:` block or `otel.skip: true`")
   - **De-duplicated** per (probe_type, metric_name) for the agent's lifetime — no spam on every scrape
   - The `/metrics` endpoint **keeps serving** the other metrics normally
   - The agent **never refuses to start** because of a missing mapping
   - Expected delivery (a quality target, not a gate):
     - Complete user documentation (`/docs/prometheus/_index.md` plus `metrics-reference.md`)
     - A full code review of the `prometheus/` package
     - PRTG/Nagios non-regression validated
   - **Rationale**: never block production over a forgotten YAML entry. The emitted names stay contractual — no auto-generated fallback polluting the namespace — and the warning records the omission without breaking the endpoint.

5. **A configurable prefix** → **fixed at `senhub_`**. There is no `metric_prefix` option in the config: changing the prefix would break user dashboards every time.

## Appendix — reference links

- Cache : `internal/agent/services/data_store/strategies/http/http_cache.go:97-618`
- Handlers HTTP : `internal/agent/services/data_store/strategies/http/http_handlers.go:26-104`
- Transformers : `internal/agent/services/data_store/transformers/`
- Config endpoints : `internal/agent/services/data_store/strategies/http/http_config.go:50-174`
- Registry probes : `internal/agent/probes/registry.go:47-63`
