# Prometheus Export — Implementation Plan

**Statut :** Phase 0 (audit + plan), en attente de validation
**Spec :** [SPEC-prometheus-integration.md](./SPEC-prometheus-integration.md)
**Auteur :** Matthieu Noirbusson
**Date :** 2026-04-18

## 0. Résumé exécutif

**Ambition long terme : SenHub Agent OTel-first**. Le modèle sémantique interne devient OpenTelemetry ; toutes les sorties (PRTG, Nagios, Prometheus, demain Zabbix, OTLP) sont des **mappers** qui traduisent OTel → format cible. Retro-compat PRTG/Nagios assurée par le YAML qui mappe le nom OTel vers les noms de channels existants.

**Livrable présent (Prometheus endpoint)** : premier mapper OTel → Prometheus, path pragmatique qui introduit l'architecture mappers sans casser les probes existantes.

La sortie Prometheus s'intègre comme **endpoint** de la strategy HTTP existante (pas une nouvelle strategy). Le handler lit le cache partagé (`MetricCache`) et sérialise en text exposition à la volée, sans cache intermédiaire.

Découvertes d'audit qui simplifient le travail :

| Élément | État actuel |
|---|---|
| Route `/api/{key}/prometheus/metrics` | **Déjà enregistrée**, handler stub → 501 |
| Endpoint `prometheus` dans `validEndpoints` | **Déjà présent** (`http_config.go:154-162`) |
| Cache des data points | **Complet** (`CachedMetric.Tags` thread-safe, TTL dynamique) |
| Transformers par probe | **YAML par probe** avec `name`, `channel`, `unit`, `multi_instance_labels` — extensible |

→ Pas de refacto profond, pas de nouvelle strategy. Travail principal : sérialiseur + tables de nommage.

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
- Zéro cache spécifique Prometheus : sérialisation à la volée depuis `MetricCache`.
- Pas de strategy `prometheus` : c'est un endpoint de la strategy `http`.
- Mapping clé interne → Prom name : dans les YAML `transformers/definitions/<probe>.yaml`, via une section additionnelle `prometheus:` par métrique.

## 2. Routes

Dual-route, même handler :

| Route | Auth | Usage |
|---|---|---|
| `GET /api/{agentkey}/prometheus/metrics` | AgentKey (pattern SenHub) | Cohérence avec PRTG/Nagios |
| `GET /metrics` | Bearer `{agentkey}` *(header)* ou `?token={agentkey}` *(query param)* | Standard Prom/vmagent |

La route `/metrics` **sans** `/api/{key}/` respecte la convention Prom. Token validé constant-time, comparaison au `authentication_key` existant.

**Activation :** seule l'activation de l'endpoint `prometheus` dans la config monte les deux routes.

## 3. Config

Extension du format v2 existant, **strictement additive** :

```yaml
storage:
  - name: http
    params:
      endpoints: [prtg, web, nagios, prometheus]   # ← "prometheus" suffit pour activer
      prometheus:                                  # bloc optionnel, défauts raisonnables
        include_probe_tags: true                   # default: true (custom_tags → labels)
        expose_host_metrics: true                  # default: true (cpu/memory host via probes)
```

> Note : le préfixe `senhub_` est figé (non configurable). Voir §12 point 5.

Validation gérée par `ConfigurationManager.ValidateConfigParams()` (déjà en place pour la liste `endpoints`).

## 4. YAML transformers — modèle OTel-first

**Décision architecturale** : les fichiers `internal/agent/services/data_store/transformers/definitions/<probe>.yaml` portent un bloc **`otel:`** par métrique comme source de vérité sémantique. Les sorties sont des mappers dérivés ou explicites.

**Shape cible :**

```yaml
probe_name: netscaler
metrics:
  # Section OTel = source de vérité sémantique
  - otel:
      name: senhub.netscaler.vserver.connections.active   # espace senhub.* pour domaines hors OTel
      unit: "{connection}"
      type: gauge
      attributes: {}                                      # attributs statiques (constants)

    # Transition: noms internes actuels émis par la probe (legacy keys).
    # Disparaît quand la probe est refactorée pour émettre OTel nativement.
    source_keys: ["netscaler.vserver.client.connections"]

    # Mapping dynamique: tags de la probe → attributs OTel.
    # Les tags présents sur le data point en cache sont traduits en attributs OTel.
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

    # Prometheus : AUCUNE section. Dérivé automatiquement des règles OTel→Prom
    # (§5). Ex. ici : senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}
```

**Règles :**
- `otel.name` : clé unique, suit semconv OTel pour domaines couverts (`system.*`) ou extension `senhub.*` pour domaines propriétaires (netscaler, citrix, veeam…).
- `otel.unit` : unité UCUM (`s`, `By`, `{connection}`, `1` pour ratio, etc.).
- `otel.type` : `counter`, `gauge`, `updowncounter`, `histogram` (V1: gauge/counter).
- `otel.attributes` : attributs constants (ex: `cpu.mode: user`).
- `source_keys` : liste des clés bus internes actuellement émises par la probe qui alimentent cette métrique OTel. **Transitoire** — supprimé quand la probe émet OTel nativement.
- `tag_to_attribute` : mapping tag probe → attribut OTel. Les valeurs de tags sont propagées comme valeurs d'attributs.
- `prtg`, `nagios` : champs inchangés par rapport au format actuel (retro-compat stricte).
- Si `otel:` est absent sur une métrique → **error au démarrage, refus de monter l'endpoint Prometheus** (décision Q4, §12).

## 4bis. Convention OTel sémantique SenHub

Pour les domaines non couverts par OTel semconv (netscaler, citrix, veeam, redfish, webapp probes…), on **crée une extension sous namespace `senhub.*`** documentée dans `docs/developer-guide/otel/senhub-semantic-conventions.md`.

Exemples de noms cibles :
- `system.cpu.time`, `system.memory.usage`, `system.network.io` (OTel natif)
- `senhub.netscaler.vserver.connections.active`, `senhub.netscaler.system.cpu.utilization`
- `senhub.citrix.session.count`, `senhub.citrix.delivery_group.machines.registered`
- `senhub.veeam.job.status`, `senhub.veeam.repository.capacity.bytes`
- `senhub.redfish.drive.temperature.celsius`, `senhub.redfish.psu.power.watts`

**Attributs** : on aligne sur OTel quand possible (`network.interface.name`, `system.device`) et on étend en `senhub.*` sinon (`senhub.vserver.name`, `senhub.citrix.delivery_group.name`).

Ce doc sera rédigé en parallèle de la Phase 0.5 comme référence.

## 5. Règles de conversion OTel → Prometheus

Conformes à la [spec OTel compatibility](https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/) :

1. **Préfixe `senhub_`** préfixé au nom OTel (indépendant du namespace OTel `system.` ou `senhub.`).
2. **Dots → underscores** dans le nom et les attributs (`system.cpu.time` → `system_cpu_time`, `cpu.mode` → `cpu_mode`).
3. **Caractères non autorisés** (regex Prom `[a-zA-Z_:][a-zA-Z0-9_:]*`) remplacés par `_`, underscores consécutifs dédupliqués.
4. **Suffixe d'unité** :
   - `s` → `_seconds`
   - `By` → `_bytes`
   - `Hz` → `_hertz`
   - `1` (ratio) → `_ratio`
   - `{connection}`, `{packet}` et unités entre accolades → supprimées
   - `foo/bar` → `_foo_per_bar`
5. **Suffixe counter** : counters reçoivent `_total` si pas déjà présent (`system_cpu_time_seconds_total`).
6. **Attributs OTel → labels Prom** : tous les attributs du data point + `cpu.mode` → `cpu_mode` etc.

Exemples déterministes :
| OTel | Prometheus |
|---|---|
| `system.cpu.time` (counter, `s`, `cpu.mode=user`) | `senhub_system_cpu_time_seconds_total{cpu_mode="user"}` |
| `system.memory.usage` (updowncounter, `By`, `system.memory.state=used`) | `senhub_system_memory_usage_bytes{system_memory_state="used"}` |
| `senhub.netscaler.vserver.connections.active` (gauge, `{connection}`, `network.vserver.name=lb_app1`) | `senhub_netscaler_vserver_connections_active{network_vserver_name="lb_app1"}` |
| `system.cpu.utilization` (gauge, `1`, `cpu.mode=user, cpu.logical_number=0`) | `senhub_system_cpu_utilization_ratio{cpu_mode="user",cpu_logical_number="0"}` |

## 5bis. Labels systématiques de probe

Sur toute métrique de probe, on **ajoute** en plus des attributs OTel :

| Label | Source | Exemple |
|---|---|---|
| `probe_name` | nom d'instance (config) | `citrix-prod-paris` |
| `probe_type` | type dans la registry | `citrix` |
| *labels custom_tags* | `custom_tags` de la probe si `include_probe_tags: true` | `env=prod, site=paris` |

Le label réservé `instance` de Prometheus est jamais émis par l'agent (conflit avec le label de scrape target).

## 6. Source de vérité = OTel

Voir §4 et §4bis. Plus de label `group`/`subgroup` génériques : les attributs OTel portent l'information sémantique (`cpu.mode`, `network.vserver.name`, etc.).

## 7. Sérialisation

**Choix :** sérialisation manuelle (pas de `client_golang/prometheus`).
- Dépendance évitée (pas déjà dans `go.mod`).
- Text exposition v0.0.4 trivial à écrire correctement.
- Contrôle total sur ordre, grouping, HELP/TYPE.
- Test automatique : round-trip parsing via `github.com/prometheus/common/expfmt` *(test-only, pas de dep runtime)*.

**Package cible :** `internal/agent/services/data_store/strategies/http/prometheus/`
- `serializer.go` — conversion `CachedMetric` → lignes text exposition
- `names.go` — résolution nom depuis transformer YAML + fallback
- `handler.go` — HTTP handler (dual route)
- `auth.go` — validation Bearer + query param
- `serializer_test.go`, `names_test.go`, `handler_test.go`

## 8. Gestion des métriques textuelles

La spec §5.5 précise 3 stratégies. Décision :

| Source | Traitement |
|---|---|
| Valeur numérique (float, int, bool) | Émise directement (bool → 0/1) |
| Valeur string identifiée comme état (`Up/Down`, `Running/Stopped`…) | Convertie via `lookup:` de la YAML → `senhub_*_state{state="up"} 1` |
| Valeur string version/firmware | Info metric `senhub_probe_info{version="..."} 1` si déclaré `prometheus.type: info` |
| Autre string non convertible | Ignorée silencieusement + log debug (pas d'erreur de scrape) |

## 9. Métriques d'agent (host-level)

En plus des métriques des probes, l'agent expose ses propres métriques opérationnelles :

| Nom | Type | Description |
|---|---|---|
| `senhub_agent_uptime_seconds` | gauge | Uptime du processus |
| `senhub_agent_probes_total` | gauge | Nombre d'instances de probe configurées |
| `senhub_agent_probes_healthy` | gauge | Nombre d'instances en état sain |
| `senhub_agent_collect_errors_total` | counter | Total erreurs de collecte |
| `senhub_agent_http_requests_total{endpoint=…}` | counter | Requêtes HTTP servies par endpoint |
| `senhub_agent_cache_entries` | gauge | Nombre d'entrées dans le cache |
| `senhub_agent_build_info{version=…, branch=…}` | gauge (valeur=1) | Info build |

Pas de label `probe_*` dessus. Ces métriques sont **toujours** émises si `prometheus` actif.

## 10. Phases d'implémentation

### Phase 0 — Plan validé *(on est ici)*
- [x] Audit structure cache + transformers + routes
- [x] Architecture cible, config schema, vocabulaire group/subgroup
- [ ] **Table de nommage complète pour les 15 probes** *(livrable 0.5)*
- [ ] Validation utilisateur

### Phase 0.5 — Tables OTel + mapping retro-compat *(bloquant avant Phase 1)*

**Étape 0.5.a — Veille OTel communautaire** *(préalable obligatoire à chaque probe)*

Avant de définir une extension `senhub.*`, vérifier systématiquement si une convention existe déjà :
- **OTel semconv officiel** : [specs/semconv](https://github.com/open-telemetry/semantic-conventions) (système, HTTP, database, RPC, messaging, faas, etc.)
- **OTel Collector contrib** : [receivers](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver) (citrix : aucun à ce jour ; netscaler : aucun ; veeam : aucun ; redfish : existe → **aligner dessus**)
- **Conventions de facto vendeurs** : documentation Grafana Labs, VictoriaMetrics, ObservIQ, DataDog integrations
- **Prometheus exporters officiels** : [prometheus/community](https://github.com/prometheus-community) — exporters historiques (redfish_exporter, etc.) dont les conventions peuvent inspirer notre namespace

Si convention existe : adopter telle quelle (attributs, unités, types). Si partielle : étendre en respectant les préfixes existants. Si inexistante : créer sous `senhub.*` en s'inspirant du style OTel officiel.

Chaque choix est tracé dans `senhub-semantic-conventions.md` avec la justification et le(s) lien(s) consulté(s).

**Étape 0.5.b — Remplissage YAML par lot**

Pour **chaque** métrique de **chaque** probe (15), rédiger :
1. Le bloc `otel:` (name, unit, type, attributes statiques)
2. Le `source_keys` (mapping vers les clés internes actuelles)
3. Le `tag_to_attribute` (translation tags existants → attributs OTel)
4. Les blocs `prtg:`/`nagios:` inchangés (retro-compat)

Aucun fallback — toute métrique non mappée fait échouer le démarrage de l'endpoint Prometheus.

Lots de revue (ordre recommandé, revue user probe par probe) :
1. **Système léger** (4 probes) : `cpu`, `memory`, `network`, `logicaldisk` — couverts par OTel semconv officiel
2. **Réseau** (4 probes) : `ping_gateway`, `ping_webapp`, `load_webapp`, `wifi_signal_strength` — partiellement couverts par HTTP semconv
3. **Events** (3 probes) : `syslog`, `event`, `otel` — voir [log semconv](https://opentelemetry.io/docs/specs/semconv/logs/)
4. **Métier lourd** (4 probes) : `netscaler`, `citrix`, `redfish`, `veeam` — prioriser la veille communautaire (redfish a un exporter Prom reconnu)

À chaque lot : veille OTel → PR YAML + mise à jour `senhub-semantic-conventions.md` → revue nommage → validation user → merge → lot suivant.

### Phase 1 — Règles OTel→Prom + sérialiseur
- `otel_to_prom.go` : application déterministe des règles §5 (dots→underscores, suffixes unités, `_total`, préfixe `senhub_`, etc.)
- `resolver.go` : résolution des data points du cache → OTel metric via `source_keys` + `tag_to_attribute` des YAML
- `serializer.go` : sérialisation text exposition (HELP/TYPE/metric)
- Injection des labels systématiques (`probe_name`, `probe_type`, custom_tags)
- Filtrage métriques textuelles non convertibles
- Tests : round-trip `expfmt.TextParser`, règles OTel→Prom sur 50+ cas, cardinalité, cas limites

### Phase 2 — Handler HTTP + routes
- Implémentation `handlePrometheusMetricsGET()` (remplace le stub 501)
- Ajout route `/metrics` (sans `/api/{key}/`) avec auth Bearer
- Tests d'intégration : bus → cache → GET → body parsable

### Phase 3 — Métriques d'agent + config
- Collecteur `AgentMetrics` pour §9
- Parse `storage[].params.prometheus` (défauts)
- Wiring au démarrage, non-régression PRTG/Nagios
- Test end-to-end : config activée → curl `/metrics` → grep `senhub_`

### Phase 4 — Validation réelle vmagent/Grafana
- Scrape vmagent réel vers VictoriaMetrics
- Vérif dashboards Grafana + PromQL
- Alerting rules de démonstration

### Phase 5 — Documentation + revue de code + CHANGELOG *(bloquant avant merge)*
- **Doc utilisateur complète** : `docs/user-guide/content/docs/prometheus/_index.md` (guide intégration), `metrics-reference.md` (table complète des 15 probes avec nom, type, group, labels, description), scrape config examples
- **Revue de code exhaustive** du package `prometheus/` (agent `pr-review-toolkit:code-reviewer` + revue user)
- **Non-régression** : PRTG/Nagios inchangés (test auto + validation manuelle sur Noble Age/SIEP-BCK)
- CHANGELOG + release notes 0.1.88 (feat majeur)

## 11. Critères de done

- [ ] `GET /metrics` renvoie body parsable par `expfmt.TextParser` (test auto)
- [ ] Métriques `senhub_agent_*` présentes
- [ ] Métriques probes avec noms et attributs conformes aux règles OTel→Prom §5
- [ ] Toute extension `senhub.*` documentée dans `senhub-semantic-conventions.md`
- [ ] Labels `probe_name`/`probe_type` systématiques
- [ ] `custom_tags` propagés comme labels si `include_probe_tags: true`
- [ ] Métriques textuelles non convertibles ignorées (pas d'erreur scrape)
- [ ] PRTG et Nagios inchangés (non-régression auto + validation manuelle)
- [ ] Couverture package `prometheus/` ≥ 80%
- [ ] Scrape réussi par vmagent, visible dans Grafana
- [ ] Docs + changelog livrés

## 12. Décisions (questions tranchées)

1. **Route `/metrics` sans agentkey dans l'URL** → **OUI**, dual route implémentée. Auth Bearer (header) ou `?token=` (query param), validation constant-time contre `authentication_key`. Impact UI: Sensor Builder doit gagner un onglet Prometheus (PromQL + config scrape copy-paste) — ajouté à la roadmap web-ui refactoring.

2. **`expose_host_metrics`** → **`true` par défaut, configurable**. Si l'utilisateur a un node_exporter en parallèle et veut éviter le doublon, il peut passer à `false`.

3. **Métriques info (version/firmware)** → déclaration **explicite** via `prometheus.type: info` dans le YAML. Pas de détection auto.

4. **Fallback nommage** → **AUCUN. Big bang.** Les 15 probes doivent avoir leur mapping Prometheus complet dans le YAML **avant merge**. Livraison accompagnée de :
   - Documentation utilisateur complète (`/docs/prometheus/_index.md` + `metrics-reference.md`)
   - Revue de code complète du package `prometheus/`
   - Non-régression PRTG/Nagios validée

5. **Préfixe configurable** → **figé à `senhub_`**. Pas d'option `metric_prefix` dans la config. Changer le préfixe casserait systématiquement les dashboards utilisateurs.

## Annexe — Liens de référence

- Cache : `internal/agent/services/data_store/strategies/http/http_cache.go:97-618`
- Handlers HTTP : `internal/agent/services/data_store/strategies/http/http_handlers.go:26-104`
- Transformers : `internal/agent/services/data_store/transformers/`
- Config endpoints : `internal/agent/services/data_store/strategies/http/http_config.go:50-174`
- Registry probes : `internal/agent/probes/registry.go:47-63`
