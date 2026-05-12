# SenHub Agent — Intégration Prometheus / VictoriaMetrics native

**Version spec :** 1.1
**Cible :** Prometheus 2.x / VictoriaMetrics (vmagent, single-node, cluster)
**Statut :** prêt pour audit + implémentation
**Auteur spec :** Matthieu Noirbusson

---

## 0. Principes d'intégration (identiques à la sortie Zabbix)

Cette spec décrit le **résultat attendu** : un endpoint `/metrics` au format Prometheus exposition, exposé par le serveur HTTP de l'agent. Elle décrit ce qui doit sortir, pas comment c'est plombé en interne.

**Règles d'intégration impératives :**

1. La sortie Prometheus est un **nouveau consommateur du bus interne**, au même titre que les sorties cloud et Zabbix.
2. Réutilise les **conventions existantes** du repo.
3. Transformation : **même pattern** que les autres sorties.
4. **Strictement additive** : aucune modification des probes ni des sorties existantes.
5. Si la spec contredit le code, le code gagne.

→ Si l'audit Phase 0 a déjà été fait pour la sortie Zabbix, il sert de socle.

## 1. Terminologie

| Terme | Définition |
|---|---|
| **Probe** | Un type de collecteur (netscaler, citrix_cvad, vmware…) |
| **Instance de probe** | Une probe instanciée dans la config avec un nom unique (ex: `lb-prod-paris` est une instance de la probe `netscaler`) |
| **Agent** | Le processus SenHub Agent qui fait tourner N instances de probes |

## 2. Contexte et motivation

L'écosystème Prometheus/VictoriaMetrics est le standard de facto pour l'observabilité, et c'est la base du stack SenHub Observability Platform (VictoriaMetrics + Grafana OSS).

Exposer les métriques de l'agent au format Prometheus permet :
- L'ingestion directe dans VictoriaMetrics via vmagent ou Prometheus
- L'utilisation dans Grafana sans couche intermédiaire
- La compatibilité avec tout l'écosystème (alerting, recording rules, federation)
- Le dogfooding : monitorer le SenHub Agent avec le propre stack SenHub

**Objectif** : ajouter une route `/metrics` au serveur HTTP de l'agent, exposant toutes les métriques (agent + probes) au format Prometheus exposition standard.

## 3. Serveur HTTP partagé

La route `/metrics` s'ajoute au **même serveur HTTP** que les routes Zabbix, healthcheck et toute autre route existante. Un seul port, une seule config TLS/auth.

Activée par `prometheus_export.enabled: true`.

## 4. Non-objectifs (V1)

- Remote write push vers VictoriaMetrics — V2
- Endpoint `/metrics` séparé par probe (anti-pattern Prometheus)
- Histogrammes natifs Prometheus (non pertinent)
- OpenTelemetry export (OTLP) — V3
- Service discovery HTTP_SD — V2

## 5. Format de sortie

### 5.1 Standard

Format Prometheus exposition text (`text/plain; version=0.0.4; charset=utf-8`), conforme OpenMetrics.

Préfixe global : `senhub_`.

### 5.2 Convention de nommage

Règles Prometheus strictes :
- Uniquement `[a-zA-Z_:][a-zA-Z0-9_:]*`
- Snake_case
- Préfixe `senhub_`
- Suffixe par unité : `_seconds`, `_bytes`, `_total`, `_percent`, `_ratio`
- Pas de `.` → remplacé par `_`

**Mapping des clés internes :**

| Clé bus interne | Métrique Prometheus | Type |
|---|---|---|
| `agent.uptime_seconds` | `senhub_agent_uptime_seconds` | gauge |
| `agent.probes.total` | `senhub_agent_probes_total` | gauge |
| `agent.probes.healthy` | `senhub_agent_probes_healthy` | gauge |
| `agent.collect.errors_total` | `senhub_agent_collect_errors_total` | counter |
| `agent.http.requests_total` | `senhub_agent_http_requests_total` | counter |
| `host.cpu.usage_percent` | `senhub_host_cpu_usage_percent` | gauge |
| (probe) `vserver.lb_app1.health` | `senhub_probe_health` avec labels | gauge |
| (probe) `vserver.lb_app1.connections_active` | `senhub_probe_connections_active` avec labels | gauge |

### 5.3 Labels

Les labels portent les dimensions. Pas de LLD, pas de host prototypes : juste des labels.

**Labels systématiques sur les métriques de probe :**

| Label | Source | Exemple |
|---|---|---|
| `probe_name` | nom de l'instance de probe | `lb-prod-paris` |
| `probe_type` | type de probe | `netscaler` |
| `group` | groupe fonctionnel de la métrique | `vserver` |
| `subgroup` | sous-groupe (objet spécifique) | `lb_app1` |

**Labels optionnels (tags de la config) :**

| Label | Source | Exemple |
|---|---|---|
| `env` | tag configuré sur la probe | `prod` |
| `site` | tag configuré sur la probe | `paris` |
| `client` | tag configuré sur la probe | `acme` |

Les tags configurés dans la section probe (`tags: { env: prod, site: paris }`) sont propagés comme labels Prometheus.

Note : on utilise `probe_name` et `probe_type` (pas `instance_name`) pour éviter la collision avec le label réservé `instance` de Prometheus (qui vaut `host:port` du scrape target).

### 5.4 Exemple de sortie complète

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

### 5.5 Métriques textuelles

Prometheus ne supporte pas les métriques textuelles. Stratégies :

1. **Métriques d'état** (up/down) → gauge numérique (0/1) avec label : `senhub_probe_state{..., state="running"} 1`
2. **Métriques info** (version, firmware) → info metric : `senhub_probe_info{probe_name="...", version="1.2.3"} 1`
3. **Non convertibles** → ignorées silencieusement (log debug)

### 5.6 Type de métriques

| Type | Usage |
|---|---|
| `gauge` | Valeur instantanée (défaut) |
| `counter` | Compteur monotone croissant |

En V1, gauge par défaut sauf si les métadonnées du bus indiquent un compteur.

## 6. Règles de transformation des noms

Mapping **déterministe et stable** clé interne → nom Prometheus :

1. Préfixe `senhub_`
2. `.` → `_`
3. `-` → `_`
4. Suppression caractères non autorisés
5. Lowercase
6. Ajout suffixe d'unité si connue (`s` → `_seconds`, `%` → `_percent`, `B` → `_bytes`, `Bps` → `_bytes_per_second`, compteur → `_total`)
7. Dédoublonnage : deux clés → même nom = erreur de config → log warning au démarrage

**Règle critique de cardinalité** : les dimensions vont dans les labels, pas dans le nom. Un vserver s'appelle `senhub_probe_connections_active{subgroup="lb_app1"}`, pas `senhub_probe_vserver_lb_app1_connections_active`.

## 7. Authentification

Même auth que les routes Zabbix. Deux modes supportés pour la compat scraper :

1. **Header** : `Authorization: Bearer <token>` (Prometheus ≥ 2.27, vmagent)
2. **Query param** : `GET /metrics?token=<token>` (fallback scrapers anciens)

Query param vérifié uniquement si le header est absent. Comparaison constant-time.

## 8. Configuration agent

```yaml
prometheus_export:
  enabled: true
  expose_host_metrics: false
  metric_prefix: "senhub"
  include_probe_tags: true
```

Pas de port ni d'auth ici : c'est la config HTTP de l'agent.

## 9. Architecture interne — directives

- **Snapshot** : si la sortie Zabbix maintient un snapshot, Prometheus le **réutilise**. Pas deux caches parallèles.
- **Sérialisation** : text exposition générée à chaque scrape depuis le snapshot. Pas de cache de sérialisation en V1.
- **Librairie** : `client_golang/prometheus` si déjà dans le projet, sinon sérialisation manuelle acceptable (trivial, moins de dépendances). L'audit tranche.
- **Package** : voisinage de la sortie Zabbix.

## 10. Tests

- Unitaires : sérialisation, nommage, labels, filtrage textuelles
- Intégration : bus mocké → `GET /metrics` → body parsable par `expfmt.TextParser`
- Bench : 50 probes × 200 métriques < 20ms sérialisation
- Non-régression : sorties cloud et Zabbix inchangées

## 11. Documentation à livrer

- `docs/prometheus-integration.md` (guide utilisateur)
- `docs/prometheus-metrics-reference.md` (liste métriques, types, labels)
- `CHANGELOG.md`

## 12. Critères d'acceptation

- [ ] Sorties existantes **non modifiées**
- [ ] `GET /metrics` renvoie du text exposition parsable
- [ ] Métriques agent `senhub_agent_*` présentes
- [ ] Métriques probes `senhub_probe_*` avec labels `probe_name`, `probe_type`, `group`, `subgroup`
- [ ] Tags config propagés comme labels
- [ ] Métriques textuelles filtrées sans erreur
- [ ] Scrape par vmagent/Prometheus → visibles dans VictoriaMetrics/Grafana
- [ ] Couverture package ≥ 80%
- [ ] Documentation livrée

## Annexe A — Requêtes PromQL / MetricsQL

```promql
# Toutes les probes d'un type
senhub_probe_up{probe_type="netscaler"}

# Connexions par vserver, site Paris
senhub_probe_connections_active{site="paris", group="vserver"}

# Ratio probes healthy / total
senhub_agent_probes_healthy / senhub_agent_probes_total

# Alerte : probe down depuis 5 min
senhub_probe_up == 0  # pendant 5m

# Top 5 vservers par connexions
topk(5, senhub_probe_connections_active{group="vserver"})
```

## Annexe B — Cardinalité

- 1 agent, 10 probes, 50 métriques/probe = ~510 séries
- 1 agent, 50 probes, 200 métriques/probe = ~10 010 séries

Modeste. Seul risque : valeurs dynamiques dans les tags config (`session_id`…) → documenter l'anti-pattern.
