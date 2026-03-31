# Netscaler - Analyse des Axes de Corrélation

**Date**: 2025-12-11
**Status**: 📊 Évaluation des capacités de corrélation actuelles

---

## Axes de Corrélation Proposés

Tu as identifié 4 axes principaux :

| Axe | Intérêt | Status Actuel | Notes |
|-----|---------|---------------|-------|
| Par vServer | Santé applicative par service exposé | ✅ **Excellent** | Tag `vserver` présent |
| Par backend pool | Détection membres défaillants | ✅ **Bon** | Tags `service`, `servicegroup` |
| Temporel (trends) | Capacity planning, anomalies | ✅ **OK** | Timestamps présents, analyse côté backend |
| Par type de trafic | SSL vs non-SSL, HTTP vs TCP | ⚠️ **Partiel** | Manque tags protocol/ssl |

---

## 1. Corrélation Par vServer ✅

### Ce Qui Fonctionne

**Tag disponible** : `vserver` (nom du virtual server)

**Métriques corrélables** :
```
netscaler.lbvserver.state {vserver="VS_PROD_HTTPS"}
netscaler.lbvserver.connections.current {vserver="VS_PROD_HTTPS"}
netscaler.lbvserver.requests.rate {vserver="VS_PROD_HTTPS"}
netscaler.lbvserver.throughput.rx {vserver="VS_PROD_HTTPS"}
netscaler.lbvserver.throughput.tx {vserver="VS_PROD_HTTPS"}
```

**Cas d'usage** :
```
Scénario : "VS_PROD_HTTPS est DOWN"
→ Filtrer toutes les métriques avec vserver="VS_PROD_HTTPS"
→ Voir : connexions qui chutent, requests à zéro, throughput à zéro
→ Diagnostic : panne du vServer, pas des backends
```

### Ce Qui Manque

**Tags enrichissants** :

| Tag | Valeur | Intérêt | Difficulté |
|-----|--------|---------|-----------|
| `vserver_type` | HTTP, SSL, TCP, UDP, SSL_BRIDGE | Filtrer par protocole | Facile |
| `vserver_port` | 80, 443, 8080 | Distinguer services sur même IP | Facile |
| `vserver_ip` | 10.0.0.10 | Géolocalisation, réseau | Facile |
| `vserver_lb_method` | ROUNDROBIN, LEASTCONNECTION | Corrélation perfs/algo | Moyenne |
| `business_service` | "Portail RH", "App Compta" | **Mapping business** | Manuel |

**Recommandation** : Ajouter `vserver_type` et `vserver_port` en priorité (facile, très utile).

---

## 2. Corrélation Par Backend Pool ✅

### Ce Qui Fonctionne

**Tags disponibles** :
- `service` : backend individuel (ex: `SVC_WEB01_443`)
- `servicegroup` : pool de backends (ex: `SG_WEB_PROD`)

**Métriques corrélables** :
```
# Par service individuel
netscaler.service.state {service="SVC_WEB01_443"}
netscaler.service.throughput {service="SVC_WEB01_443"}
netscaler.service.transactions.active {service="SVC_WEB01_443"}

# Par service group
netscaler.servicegroup.state {servicegroup="SG_WEB_PROD"}
netscaler.servicegroup.requests.rate {servicegroup="SG_WEB_PROD"}
netscaler.servicegroup.members.active {servicegroup="SG_WEB_PROD"}
```

**Cas d'usage** :
```
Scénario : "SG_WEB_PROD a 2 membres DOWN sur 4"
→ Filtrer servicegroup="SG_WEB_PROD"
→ members.active = 2 (alert!)
→ Creuser : filtrer services individuels du pool
→ Voir : SVC_WEB03 et SVC_WEB04 à state=0
→ Diagnostic : 2 backends défaillants, charge sur 2 survivants
```

### Ce Qui Manque

**Relation vServer → ServiceGroup** :

Aujourd'hui : **aucune trace** de quelle ServiceGroup est liée à quel vServer !

```
# On ne peut pas faire :
"Quels backends servent VS_PROD_HTTPS ?"
"Quel vServer est impacté si SG_WEB_PROD tombe ?"
```

**Solution** : Ajouter tags de liaison

| Tag | Où | Valeur | Intérêt |
|-----|-----|--------|---------|
| `vserver` | Sur metrics `servicegroup` | Nom du vServer lié | Corrélation bottom-up |
| `servicegroup` | Sur metrics `lbvserver` | Nom du SG lié | Corrélation top-down |
| `backend_host` | Sur metrics `service` | IP ou hostname | Troubleshooting infra |
| `backend_port` | Sur metrics `service` | Port backend | Distinguer services |

**⚠️ Limitation API Citrix** :
Les stats ne contiennent **pas** les bindings (relations vServer↔ServiceGroup).
Il faut faire des appels config en plus :
```bash
GET /nitro/v1/config/lbvserver_servicegroup_binding/{vservername}
GET /nitro/v1/config/servicegroup_servicegroupmember_binding/{servicegroupname}
```

**Recommandation** : Implémenter un **cache des relations** au démarrage de la probe.

---

## 3. Corrélation Temporelle (Trends) ✅

### Ce Qui Fonctionne

**Timestamps** : Tous les datapoints ont un timestamp précis.

**Backend PRTG/SenHub** : Stocke l'historique, calcule :
- Tendances (augmentation/diminution)
- Anomalies (valeurs hors norm)
- Prédictions (capacity planning)

**Cas d'usage** :
```
Scénario : "Connexions actives augmentent de 20%/semaine"
→ Backend détecte la tendance
→ Alert : "Saturation prévue dans 3 semaines"
→ Action : Scale-out préventif
```

### Ce Qui Manque (côté agent)

**Métriques dérivées** calculées localement :

| Métrique | Calcul | Intérêt | Complexité |
|----------|--------|---------|------------|
| Delta connexions | `current - previous` | Surge détection | Facile |
| Rate of change | `(current - prev) / interval` | Tendance immédiate | Facile |
| Percentiles | P50, P95, P99 | Latence backend | Difficile |
| Anomaly score | Écart vs moyenne mobile | Alert prédictive | Difficile |

**Recommandation** : Laisser au **backend** (SenHub/PRTG), ils font ça mieux.

L'agent doit juste fournir des **métriques brutes de qualité** avec **timestamps précis**.

---

## 4. Corrélation Par Type de Trafic ⚠️

### Ce Qui Manque

**Aucun tag de protocole/chiffrement actuellement !**

```
# Impossible aujourd'hui :
"Combien de trafic SSL vs non-SSL ?"
"HTTP/2 vs HTTP/1.1 ratio ?"
"TCP pur vs HTTP ?"
```

**Tags nécessaires** :

| Tag | Source | Valeurs | Intérêt |
|-----|--------|---------|---------|
| `protocol` | vServer config | HTTP, SSL, TCP, UDP, SSL_BRIDGE, DNS | Filtrage par protocole |
| `ssl_enabled` | vServer config | true, false | Trafic chiffré vs clair |
| `http_version` | Stats HTTP | 1.0, 1.1, 2.0 | Optimisation protocole |
| `persistence_type` | vServer config | SOURCEIP, COOKIEINSERT, SSLSESSION | Troubleshooting sessions |

**Implémentation** :

```go
// Dans collectLBVServerStats(), enrichir les tags :
func (p *netscalerProbe) collectLBVServerStats(...) {
    // Pour chaque vServer, récupérer sa config :
    vserverConfig, _ := p.client.FindResource("lbvserver", vserverName)

    vserverTags := append(baseTags,
        tags.Tag{Key: "vserver", Value: vserverName},
        tags.Tag{Key: "vserver_type", Value: vserverConfig["servicetype"]},
        tags.Tag{Key: "vserver_port", Value: vserverConfig["port"]},
        tags.Tag{Key: "vserver_ip", Value: vserverConfig["ipv46"]},
    )
}
```

**⚠️ Impact Performance** :
- Stats API : 1 appel pour tous les vServers
- Config API : 1 appel **par vServer** (N+1 queries)

**Solution** :
1. **Cache des configs** au démarrage (refresh toutes les 5 min)
2. **Join en mémoire** stats + config

**Recommandation** : Implémenter le cache de config en priorité.

---

## 5. Axes de Corrélation Additionnels

Tu n'as pas mentionné, mais très utiles :

### A. Corrélation Géographique (si GSLB)

**Tags** :
- `datacenter` : Paris, London, NewYork
- `region` : EU, US, APAC
- `site_role` : primary, secondary, dr

**Cas d'usage** :
```
"Site Paris DOWN → Trafic bascule sur London"
→ Voir augmentation connexions sur London vServers
→ Voir diminution Paris vServers
```

### B. Corrélation Business (criticité)

**Tags manuels** (dans config agent) :
- `environment` : prod, staging, dev
- `business_service` : "Portail RH", "E-commerce"
- `criticality` : critical, high, medium, low
- `team_owner` : "Team Infra", "Team App"

**Configuration** :
```yaml
probes:
  - name: netscaler-prod
    type: netscaler
    params:
      base_url: "https://..."
      # Tags business personnalisés
      custom_tags:
        environment: "production"
        datacenter: "paris"
        criticality: "critical"
```

**Cas d'usage** :
```
"Alertes criticality=critical uniquement pour l'astreinte"
"Dashboard par team_owner pour chaque équipe"
"SLA tracking par business_service"
```

### C. Corrélation Infra (réseau/compute)

**Tags** :
- `rack` : R01, R02 (si physique)
- `hypervisor` : ESX01, ESX02 (si VM)
- `network_zone` : DMZ, LAN, WAN
- `vlan_id` : 100, 200

**Cas d'usage** :
```
"Tous les vServers sur vlan_id=100 lents"
→ Problème réseau VLAN, pas applicatif
```

---

## Matrice de Corrélation - Vue d'Ensemble

```
┌─────────────────────────────────────────────────────────────┐
│                    AXES DE CORRÉLATION                      │
├─────────────┬──────────────┬──────────────┬─────────────────┤
│ Axe         │ Status       │ Tags Actuels │ Tags Manquants  │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Par vServer │ ✅ Excellent │ vserver      │ type, port, ip  │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Par Backend │ ✅ Bon       │ service,     │ vserver (bind), │
│             │              │ servicegroup │ host, port      │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Temporel    │ ✅ OK        │ timestamp    │ (backend calc)  │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Type Trafic │ ⚠️ Partiel   │ (aucun)      │ protocol,       │
│             │              │              │ ssl_enabled     │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Géographique│ ❌ Absent    │ (aucun)      │ datacenter,     │
│ (GSLB)      │              │              │ region, site    │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Business    │ ❌ Absent    │ probe_name   │ environment,    │
│             │              │              │ criticality     │
├─────────────┼──────────────┼──────────────┼─────────────────┤
│ Infra       │ ❌ Absent    │ (aucun)      │ rack, vlan,     │
│             │              │              │ hypervisor      │
└─────────────┴──────────────┴──────────────┴─────────────────┘
```

---

## Recommandations Priorisées

### Phase 1 - Enrichissement Essentiel (avec les 3 métriques critiques)

**1. Cache des configurations vServer/Service** (prérequis)
```go
type netscalerProbe struct {
    // ...
    configCache map[string]map[string]interface{} // Cache des configs
    cacheExpiry time.Time
}

func (p *netscalerProbe) refreshConfigCache() {
    // Toutes les 5 minutes
    p.configCache["lbvserver"] = p.client.FindAllResources("lbvserver")
    p.configCache["service"] = p.client.FindAllResources("service")
    p.configCache["servicegroup"] = p.client.FindAllResources("servicegroup")
}
```

**2. Tags vServer enrichis**
- ✅ `vserver` (déjà présent)
- ➕ `vserver_type` (HTTP, SSL, TCP)
- ➕ `vserver_port` (80, 443, 8080)
- ➕ `vserver_ip` (adresse IP)

**3. Tags Service enrichis**
- ✅ `service` (déjà présent)
- ➕ `backend_host` (IP backend)
- ➕ `backend_port` (port backend)
- ➕ `vserver` (vServer associé via binding)

**4. Tags Business (config manuelle)**
```yaml
probes:
  - name: netscaler-prod-paris
    type: netscaler
    custom_tags:
      environment: "production"
      datacenter: "paris"
      criticality: "critical"
```

### Phase 2 - Corrélations Avancées (Q1 2025)

**5. Bindings vServer ↔ ServiceGroup**
```go
// Cache des relations
func (p *netscalerProbe) cacheBindings() {
    for _, vserver := range vservers {
        bindings := p.client.FindResource("lbvserver_servicegroup_binding", vserver)
        // Store: vserver → [servicegroups]
    }
}
```

**6. Tags GSLB** (si multi-sites)
- `site` : paris, london
- `site_role` : primary, secondary

**7. Tags Infra** (si besoin)
- `rack`, `vlan_id`, `network_zone`

---

## Exemple Concret : Troubleshooting avec Corrélations

### Scénario : "Application e-commerce lente"

**1. Point d'entrée : vServer**
```
Filter: vserver="VS_ECOMMERCE_443"
Metrics:
  - state = 1 (UP) ✅
  - connections.current = 5000 (normal: 3000) ⚠️
  - requests.rate = 800/s (normal: 500/s) ⚠️
  - throughput.tx = 50 Mbps (normal) ✅
```
→ **vServer OK mais surchargé**

**2. Descendre : ServiceGroup**
```
Filter: servicegroup="SG_ECOMMERCE_BACKEND" (via binding)
Metrics:
  - members.active = 3/5 ⚠️
  - requests.rate = 800/s (distribué sur 3 au lieu de 5)
```
→ **2 backends DOWN !**

**3. Identifier les backends DOWN**
```
Filter: servicegroup="SG_ECOMMERCE_BACKEND" + service (members)
Metrics:
  - SVC_BACKEND_01: state=1, transactions=200 ✅
  - SVC_BACKEND_02: state=1, transactions=250 ✅
  - SVC_BACKEND_03: state=1, transactions=350 ⚠️ (surchargé)
  - SVC_BACKEND_04: state=0 ❌
  - SVC_BACKEND_05: state=0 ❌
```
→ **BACKEND_04 et BACKEND_05 DOWN**

**4. Corrélation Infra**
```
Filter: backend_host IN (BACKEND_04_IP, BACKEND_05_IP)
Tags: vlan_id=200, rack=R02
```
→ **Tous dans le même rack/VLAN** → problème réseau ?

**5. Corrélation Temporelle**
```
Timeline:
  - 14h00: 5 backends UP
  - 14h15: BACKEND_04 DOWN
  - 14h16: BACKEND_05 DOWN
  - 14h20: Charge sur 3 backends augmente
  - 14h25: Latence application +300%
```
→ **Root cause : Panne rack R02 à 14h15**

**Diagnostic : 2 backends DOWN (panne rack) → charge sur 3 survivants → latence applicative**

---

## Implémentation Technique Recommandée

### Structure de Tags Optimale

```go
// Base tags (tous les datapoints)
baseTags := []tags.Tag{
    {Key: "probe_name", Value: p.GetName()},
    {Key: "probe_type", Value: "netscaler"},
    {Key: "netscaler_host", Value: p.baseURL},
    {Key: "netscaler_name", Value: p.deviceName}, // hostname de l'appliance
}

// vServer tags (métriques lbvserver)
vserverTags := append(baseTags,
    {Key: "vserver", Value: vserverName},
    {Key: "vserver_type", Value: config["servicetype"]},
    {Key: "vserver_port", Value: config["port"]},
    {Key: "vserver_ip", Value: config["ipv46"]},
    {Key: "servicegroup", Value: boundServiceGroup}, // via binding
)

// Service tags (métriques service)
serviceTags := append(baseTags,
    {Key: "service", Value: serviceName},
    {Key: "backend_host", Value: config["ipaddress"]},
    {Key: "backend_port", Value: config["port"]},
    {Key: "servicegroup", Value: parentServiceGroup},
    {Key: "vserver", Value: boundVServer}, // via binding
)

// ServiceGroup tags
sgTags := append(baseTags,
    {Key: "servicegroup", Value: sgName},
    {Key: "vserver", Value: boundVServer}, // via binding
)

// Custom tags (de la config utilisateur)
if customTags := p.config["custom_tags"]; customTags != nil {
    for k, v := range customTags.(map[string]string) {
        baseTags = append(baseTags, tags.Tag{Key: k, Value: v})
    }
}
```

### Cache Pattern

```go
type ConfigCache struct {
    vservers     map[string]map[string]interface{}
    services     map[string]map[string]interface{}
    servicegroups map[string]map[string]interface{}
    bindings     map[string][]string // vserver → servicegroups

    lastRefresh  time.Time
    refreshInterval time.Duration
    mu           sync.RWMutex
}

func (c *ConfigCache) Refresh(client *service.NitroClient) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Refresh configs
    c.vservers = fetchAllVServers(client)
    c.services = fetchAllServices(client)
    c.servicegroups = fetchAllServiceGroups(client)

    // Refresh bindings
    c.bindings = fetchAllBindings(client)

    c.lastRefresh = time.Now()
}

func (c *ConfigCache) Get(resourceType, name string) map[string]interface{} {
    c.mu.RLock()
    defer c.mu.RUnlock()

    // Auto-refresh si expiré
    if time.Since(c.lastRefresh) > c.refreshInterval {
        go c.Refresh(client) // async
    }

    return c.vservers[name]
}
```

---

## Conclusion

### Points Forts Actuels
✅ Axes **vServer** et **Backend** bien couverts (tags discriminants)
✅ Temporel OK (timestamps précis)
✅ Base solide pour enrichissement

### Lacunes Principales
⚠️ **Pas de tags protocole/SSL** → impossible de filtrer par type de trafic
⚠️ **Pas de bindings vServer↔ServiceGroup** → corrélation manuelle
❌ **Pas de tags business** → pas de vue métier
❌ **Pas de tags infra** → troubleshooting réseau difficile

### Roadmap Recommandée

**Maintenant** (avec Phase 1 métriques critiques) :
1. Cache des configurations
2. Tags vServer enrichis (type, port, ip)
3. Tags Service enrichis (backend_host, backend_port)
4. Support custom_tags (business)

**Q1 2025** :
5. Cache des bindings (relations)
6. Tags GSLB (si multi-sites)

**Q2 2025** :
7. Tags infra (si besoin)

---

**Verdict** : Tes axes de corrélation sont **pertinents et bien pensés**. On a une base solide (vServer, Backend) mais il faut enrichir pour le **type de trafic** et les **relations** entre objets.

---

**Dernière mise à jour**: 2025-12-11
