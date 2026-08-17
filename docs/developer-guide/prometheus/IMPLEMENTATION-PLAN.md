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

| Élément | État actuel |
|---|---|
| Route `/api/{key}/prometheus/metrics` | **Déjà enregistrée**, handler stub → 501 |
| The `prometheus` endpoint in `validEndpoints` | **Already there** (`http_config.go:154-162`) |
| Data point cache | **Complete** (`CachedMetric.Tags` thread-safe, dynamic TTL) |
| Per-probe transformers | **One YAML per probe** with `name`, `channel`, `unit`, `multi_instance_labels` — extensible |

→ No deep refactor and no new strategy. The main work is the serialiser and the naming tables.

## 1. Architecture cible

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
│  /prometheus/metrics│◀───────────────┘  (nouveau)
│  /web/*             │
└─────────────────────┘
           │
           ▼
┌─────────────────────┐
│ PromSerializer      │   (nouveau)
│  cache → text       │
└─────────────────────┘
```

**Principes :**
- No Prometheus-specific cache: serialisation happens on the fly from `MetricCache`.
- No `prometheus` strategy: it is an endpoint of the `http` strategy.
- Internal key → Prometheus name mapping lives in `transformers/definitions/<probe>.yaml`, in an additional `prometheus:` section per metric.

## 2. Routes

Dual-route, même handler :

| Route | Auth | Usage |
|---|---|---|
| `GET /api/{agentkey}/prometheus/metrics` | AgentKey (the SenHub pattern) | Consistency with PRTG/Nagios |
| `GET /metrics` | Bearer `{agentkey}` *(header)* ou `?token={agentkey}` *(query param)* | Standard Prom/vmagent |

The `/metrics` route **without** `/api/{key}/` honours the Prometheus convention. The token is validated in constant time against the existing `authentication_key`.

**Enabling it:** only enabling the `prometheus` endpoint in the config mounts the two routes.

## 3. Config

Extension du format v2 existant, **strictement additive** :

```yaml
storage:
  - name: http
    params:
      endpoints: [prtg, web, nagios, prometheus]   # ← "prometheus" alone enables it
      prometheus:                                  # bloc optionnel, défauts raisonnables
        include_probe_tags: true                   # default: true (custom_tags → labels)
        expose_host_metrics: true                  # default: true (cpu/memory host via probes)
```

> Note: the `senhub_` prefix is fixed and not configurable. See §12, point 5.

Validation is handled by `ConfigurationManager.ValidateConfigParams()`, already in place for the `endpoints` list.

## 4. YAML transformers — modèle OTel-first

**Architectural decision**: the `internal/agent/services/data_store/transformers/definitions/<probe>.yaml` files carry an **`otel:`** block per metric as the semantic source of truth. The outputs are mappers, derived or explicit.

**Shape cible :**

```yaml
probe_name: netscaler
metrics:
  # Section OTel = source de vérité sémantique
  - otel:
      name: senhub.netscaler.vserver.connections.active   # the senhub.* space, for domains OTel does not cover
      unit: "{connection}"
      type: gauge
      attributes: {}                                      # attributs statiques (constants)

    # Transitional: the current internal names the probe emits (legacy keys).
    # Goes away once the probe is reworked to emit OTel natively.
    source_keys: ["netscaler.vserver.client.connections"]

    # Dynamic mapping: probe tags → OTel attributes.
    # Tags present on the cached data point are translated into OTel attributes.
    tag_to_attribute:
      vserver: network.vserver.name                       # tag existant → attribut OTel

    # Retro-compat PRTG (champs actuels inchangés)
    prtg:
      channel: vserver.client.connections
      display_name: "vServer Client Connections"
      category: vserver
      description: "Active client connections per vServer"

    # Retro-compat Nagios (TBD lors de l'audit Nagios)
    nagios: {}

    # Prometheus: NO section at all. Derived automatically from the OTel→Prom rules
    # (§5). Ex. ici : senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}
```

**Règles :**
- `otel.name`: the unique key. It follows the OTel semconv for covered domains (`system.*`), or the `senhub.*` extension for proprietary ones (netscaler, citrix, veeam…).
- `otel.unit`: the UCUM unit (`s`, `By`, `{connection}`, `1` for a ratio, and so on).
- `otel.type` : `counter`, `gauge`, `updowncounter`, `histogram` (V1: gauge/counter).
- `otel.attributes` : attributs constants (ex: `cpu.mode: user`).
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
- value = **0** sinon
- attribut `<attribute>` = nom du state

Exemple concret — un drive en état "ok" (code 0) émet 4 séries:
```
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="ok"} 1
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="degraded"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="failed"} 0
senhub_hw_status{hw_id="disk1",hw_type="physical_disk",hw_state="predicted_failure"} 0
```

**Rationale**: OTel compliance lives in the mapper. Future exports — native OTLP to VictoriaMetrics OTel, Grafana OTel — have nothing to correct, because the data is already strict OTel on the way out.

## 4bis. Convention OTel sémantique SenHub

For domains the OTel semconv does not cover (netscaler, citrix, veeam, redfish, the webapp probes…), we **create an extension under the `senhub.*` namespace**, documented in `docs/developer-guide/otel/senhub-semantic-conventions.md`.

Exemples de noms cibles :
- `system.cpu.time`, `system.memory.usage`, `system.network.io` (OTel natif)
- `senhub.netscaler.vserver.connections.active`, `senhub.netscaler.system.cpu.utilization`
- `senhub.citrix.session.count`, `senhub.citrix.delivery_group.machines.registered`
- `senhub.veeam.job.status`, `senhub.veeam.repository.capacity.bytes`
- `senhub.redfish.drive.temperature.celsius`, `senhub.redfish.psu.power.watts`

**Attributes**: aligned with OTel where possible (`network.interface.name`, `system.device`), extended under `senhub.*` otherwise (`senhub.vserver.name`, `senhub.citrix.delivery_group.name`).

That document was written alongside Phase 0.5, as the reference.

## 5. Règles de conversion OTel → Prometheus

Per the [OTel compatibility spec](https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/):

1. **Préfixe `senhub_`** préfixé au nom OTel (indépendant du namespace OTel `system.` ou `senhub.`).
2. **Dots → underscores** in the name and the attributes (`system.cpu.time` → `system_cpu_time`, `cpu.mode` → `cpu_mode`).
3. **Disallowed characters** (the Prometheus regex `[a-zA-Z_:][a-zA-Z0-9_:]*`) replaced by `_`, with consecutive underscores collapsed.
4. **Suffixe d'unité** :
   - `s` → `_seconds`
   - `By` → `_bytes`
   - `Hz` → `_hertz`
   - `1` (ratio) → `_ratio`
   - `{connection}`, `{packet}` et unités entre accolades → supprimées
   - `foo/bar` → `_foo_per_bar`
5. **Counter suffix**: counters get `_total` when they do not already end in it (`system_cpu_time_seconds_total`).
6. **OTel attributes → Prometheus labels**: every data point attribute, with `cpu.mode` → `cpu_mode` and so on.

Exemples déterministes :
| OTel | Prometheus |
|---|---|
| `system.cpu.time` (counter, `s`, `cpu.mode=user`) | `senhub_system_cpu_time_seconds_total{cpu_mode="user"}` |
| `system.memory.usage` (updowncounter, `By`, `system.memory.state=used`) | `senhub_system_memory_usage_bytes{system_memory_state="used"}` |
| `senhub.netscaler.vserver.connections.active` (gauge, `{connection}`, `network.vserver.name=lb_app1`) | `senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}` |
| `system.cpu.utilization` (gauge, `1`, `cpu.mode=user, cpu.logical_number=0`) | `senhub_system_cpu_utilization_ratio{cpu_mode="user",cpu_logical_number="0"}` |

## 5bis. Labels systématiques de probe

On every probe metric we **add**, on top of the OTel attributes:

| Label | Source | Exemple |
|---|---|---|
| `probe_name` | nom d'instance (config) | `citrix-prod-paris` |
| `probe_type` | the registry type | `citrix` |
| *custom_tags labels* | the probe's `custom_tags`, when `include_probe_tags: true` | `env=prod, site=paris` |

Prometheus's reserved `instance` label is never emitted by the agent — it would clash with the scrape target's own.

## 6. Source de vérité = OTel

See §4 and §4bis. No more generic `group`/`subgroup` labels: the OTel attributes carry the semantic information (`cpu.mode`, `network.vserver.name`, and so on).

## 7. Sérialisation

**Choice:** manual serialisation, not `client_golang/prometheus`.
- Avoids a dependency that was not already in `go.mod`.
- Text exposition v0.0.4 trivial à écrire correctement.
- Full control over ordering, grouping and HELP/TYPE.
- Automated test: round-trip parsing via `github.com/prometheus/common/expfmt` *(test-only, not a runtime dependency)*.

**Package cible :** `internal/agent/services/data_store/strategies/http/prometheus/`
- `serializer.go` — conversion `CachedMetric` → lignes text exposition
- `names.go` — résolution nom depuis transformer YAML + fallback
- `handler.go` — HTTP handler (dual route)
- `auth.go` — validation Bearer + query param
- `serializer_test.go`, `names_test.go`, `handler_test.go`

## 8. Handling textual metrics

La spec §5.5 précise 3 stratégies. Décision :

| Source | Traitement |
|---|---|
| Valeur numérique (float, int, bool) | Émise directement (bool → 0/1) |
| A string value identified as a state (`Up/Down`, `Running/Stopped`…) | Converted through the YAML's `lookup:` → `senhub_*_state{state="up"} 1` |
| Valeur string version/firmware | Info metric `senhub_probe_info{version="..."} 1` si déclaré `prometheus.type: info` |
| Any other non-convertible string | Silently ignored, with a debug log — never a scrape error |

## 9. Métriques d'agent (host-level)

Beyond the probe metrics, the agent exposes its own operational metrics:

| Nom | Type | Description |
|---|---|---|
| `senhub_agent_uptime_seconds` | gauge | Uptime du processus |
| `senhub_agent_probes_total` | gauge | Nombre d'instances de probe configurées |
| `senhub_agent_probes_healthy` | gauge | Nombre d'instances en état sain |
| `senhub_agent_collect_errors_total` | counter | Total erreurs de collecte |
| `senhub_agent_http_requests_total{endpoint=…}` | counter | HTTP requests served, per endpoint |
| `senhub_agent_cache_entries` | gauge | Number of entries in the cache |
| `senhub_agent_build_info{version=…, branch=…}` | gauge (valeur=1) | Info build |

No `probe_*` label on these. They are **always** emitted when `prometheus` is enabled.

## 10. Phases d'implémentation

### Phase 0 — Plan approved
- [x] Audit structure cache + transformers + routes
- [x] Architecture cible, config schema, vocabulaire group/subgroup
- [ ] **Complete naming table for the 15 probes** *(deliverable 0.5)*
- [ ] Validation utilisateur

### Phase 0.5 — Tables OTel + mapping retro-compat *(bloquant avant Phase 1)*

**Étape 0.5.a — Veille OTel communautaire** *(préalable obligatoire à chaque probe)*

Before defining a `senhub.*` extension, always check whether a convention already exists:
- **OTel semconv officiel** : [specs/semconv](https://github.com/open-telemetry/semantic-conventions) (système, HTTP, database, RPC, messaging, faas, etc.)
- **OTel Collector contrib** : [receivers](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver) (citrix : aucun à ce jour ; netscaler : aucun ; veeam : aucun ; redfish : existe → **aligner dessus**)
- **Conventions de facto vendeurs** : documentation Grafana Labs, VictoriaMetrics, ObservIQ, DataDog integrations
- **Official Prometheus exporters**: [prometheus/community](https://github.com/prometheus-community) — long-standing exporters (redfish_exporter and others) whose conventions can inform our namespace

If a convention exists: adopt it as it is — attributes, units, types. If it is partial: extend it, honouring the existing prefixes. If none exists: create one under `senhub.*`, in the style of the official OTel conventions.

Every choice is traced in `senhub-semantic-conventions.md`, with its justification and the links consulted.

**Step 0.5.b — filling the YAML, batch by batch**

Pour **chaque** métrique de **chaque** probe (15), rédiger :
1. Le bloc `otel:` (name, unit, type, attributes statiques)
2. `source_keys` (the mapping onto the current internal keys)
3. Le `tag_to_attribute` (translation tags existants → attributs OTel)
4. Les blocs `prtg:`/`nagios:` inchangés (retro-compat)

No fallback — an unmapped metric is silently not emitted in `/metrics`, with a WARN logged once per (probe_type, metric_name). The endpoint keeps working (Q4 revised — see §12).

Review batches (the recommended order, reviewed probe by probe):
1. **Light system** (4 probes): `cpu`, `memory`, `network`, `logicaldisk` — covered by the official OTel semconv
2. **Network** (4 probes): `ping_gateway`, `ping_webapp`, `load_webapp`, `wifi_signal_strength` — partially covered by the HTTP semconv
3. **Events** (2 probes) : `syslog`, `event` — voir [log semconv](https://opentelemetry.io/docs/specs/semconv/logs/)
4. **Heavy domain** (4 probes): `netscaler`, `citrix`, `redfish`, `veeam` — lead with the community survey (redfish has a well-known Prometheus exporter)

À chaque lot : veille OTel → PR YAML + mise à jour `senhub-semantic-conventions.md` → revue nommage → validation user → merge → lot suivant.

### Phase 1 — Règles OTel→Prom + sérialiseur
- `otel_to_prom.go`: deterministic application of the §5 rules (dots→underscores, unit suffixes, `_total`, the `senhub_` prefix, and so on)
- `resolver.go`: resolving cached data points into an OTel metric via the YAML's `source_keys` and `tag_to_attribute`
- `serializer.go` : sérialisation text exposition (HELP/TYPE/metric)
- Injecting the labels present on everything (`probe_name`, `probe_type`, custom_tags)
- Filtrage métriques textuelles non convertibles
- Tests: `expfmt.TextParser` round-trip, the OTel→Prom rules over 50+ cases, cardinality, edge cases

### Phase 2 — Handler HTTP + routes
- Implement `handlePrometheusMetricsGET()`, replacing the 501 stub
- Add the `/metrics` route (without `/api/{key}/`), with Bearer auth
- Tests d'intégration : bus → cache → GET → body parsable

### Phase 3 — Métriques d'agent + config
- An `AgentMetrics` collector for §9
- Parse `storage[].params.prometheus` (défauts)
- Wiring au démarrage, non-régression PRTG/Nagios
- Test end-to-end : config activée → curl `/metrics` → grep `senhub_`

### Phase 4 — Validation réelle vmagent/Grafana
- Scrape vmagent réel vers VictoriaMetrics
- Vérif dashboards Grafana + PromQL
- Alerting rules de démonstration

### Phase 5 — Documentation + revue de code + CHANGELOG *(bloquant avant merge)*
- **Complete user documentation**: `docs/user-guide/content/docs/prometheus/_index.md` (the integration guide), `metrics-reference.md` (the complete table of the 15 probes with name, type, group, labels and description), and scrape config examples
- **Revue de code exhaustive** du package `prometheus/` (agent `pr-review-toolkit:code-reviewer` + revue user)
- **Non-regression**: PRTG/Nagios unchanged (automated test plus manual validation on production deployments)
- CHANGELOG + release notes 0.1.88 (feat majeur)

## 11. Critères de done

- [ ] `GET /metrics` returns a body parsable by `expfmt.TextParser` (automated test)
- [ ] Métriques `senhub_agent_*` présentes
- [ ] Probe metrics carry names and attributes conforming to the §5 OTel→Prom rules
- [ ] Every `senhub.*` extension documented in `senhub-semantic-conventions.md`
- [ ] Labels `probe_name`/`probe_type` systématiques
- [ ] `custom_tags` propagés comme labels si `include_probe_tags: true`
- [ ] Non-convertible textual metrics ignored, with no scrape error
- [ ] PRTG et Nagios inchangés (non-régression auto + validation manuelle)
- [ ] Couverture package `prometheus/` ≥ 80%
- [ ] Successfully scraped by vmagent and visible in Grafana
- [ ] Docs + changelog livrés

## 12. Décisions (questions tranchées)

1. **A `/metrics` route without the agentkey in the URL** → **yes**, a dual route is implemented. Bearer auth (header) or `?token=` (query parameter), validated in constant time against `authentication_key`. UI impact: Sensor Builder needs a Prometheus tab (PromQL plus a copy-paste scrape config) — added to the web-ui refactoring roadmap.

2. **`expose_host_metrics`** → **`true` by default, configurable**. An operator running node_exporter alongside, who wants to avoid the duplication, can set it to `false`.

3. **Info metrics (version/firmware)** → declared **explicitly** with `prometheus.type: info` in the YAML. No auto-detection.

4. **A naming fallback** → **none**, but **not blocking** *(Q4 revised 2026-04-21)*.
   - A metric with no `otel:` block is **not emitted** in `/metrics`
   - A **WARN is logged** with `probe_name`, `probe_type`, `metric_name` and an actionable message ("Add an `otel:` block or `otel.skip: true`")
   - **De-duplicated** per (probe_type, metric_name) for the agent's lifetime — no spam on every scrape
   - The `/metrics` endpoint **keeps serving** the other metrics normally
   - L'agent **ne refuse jamais de démarrer** à cause d'un mapping manquant
   - Expected delivery (a quality target, not a gate):
     - Documentation utilisateur complète (`/docs/prometheus/_index.md` + `metrics-reference.md`)
     - Revue de code complète du package `prometheus/`
     - Non-régression PRTG/Nagios validée
   - **Rationale**: never block production over a forgotten YAML entry. The emitted names stay contractual — no auto-generated fallback polluting the namespace — and the warning records the omission without breaking the endpoint.

5. **A configurable prefix** → **fixed at `senhub_`**. There is no `metric_prefix` option in the config: changing the prefix would break user dashboards every time.

## Annexe — Liens de référence

- Cache : `internal/agent/services/data_store/strategies/http/http_cache.go:97-618`
- Handlers HTTP : `internal/agent/services/data_store/strategies/http/http_handlers.go:26-104`
- Transformers : `internal/agent/services/data_store/transformers/`
- Config endpoints : `internal/agent/services/data_store/strategies/http/http_config.go:50-174`
- Registry probes : `internal/agent/probes/registry.go:47-63`
