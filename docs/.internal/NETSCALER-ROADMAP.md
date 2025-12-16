# Netscaler Probe - Roadmap & Status

**Date**: 2025-12-11
**Current Version**: Phase 1 Complete

---

## ✅ Phase 1 - TERMINÉ (2025-12-11)

### Objectifs
- Métriques critiques de production
- Infrastructure de corrélation (cache + tags enrichis)
- Fork SDK pour fix singleton bug

### Réalisations

**Métriques critiques** (3):
- ✅ SSL Certificate expiration (30/15/7 jours alerts)
- ✅ HA State (PRIMARY/SECONDARY/UNKNOWN)
- ✅ Disk usage (/var/log, /, /flash)

**Infrastructure**:
- ✅ Configuration cache (5 min refresh)
- ✅ Bindings cache (vServer ↔ ServiceGroup)
- ✅ Custom tags support (business/infra tagging)

**Tags enrichis**:
- ✅ vServer: type, port, IP, servicegroup
- ✅ Service: backend_host, backend_port
- ✅ ServiceGroup: vserver

**SDK Fix**:
- ✅ Fork senhub-io/adc-nitro-go
- ✅ Fix FindAllStats() pour singleton
- ✅ Fix FindStat() pour singleton

**Métriques totales**: 33 métriques
**Documentation**: 4 docs complètes

---

## 🔄 Phase 2 - Métriques Avancées (Q1 2025)

### Priorité Haute

#### 1. Spillover Count (vServer saturation)
**Pourquoi**: Détecte quand un vServer déborde vers son backup
**API**: `totspillovers` dans lbvserver stats
**Difficulté**: Facile
**Impact**: Alerte précoce saturation

```go
datapoints = append(datapoints, datapoint.DataPoint{
    Name:  "netscaler.lbvserver.spillovers.total",
    Value: float32(getFloat(vserver, "totspillovers")),
    Tags:  vserverTags,
})
```

#### 2. Surge Queue Length (backend saturation)
**Pourquoi**: File d'attente quand backends saturés
**API**: `surgecount` dans service/servicegroup stats
**Difficulté**: Facile
**Impact**: Détection saturation backend

```go
datapoints = append(datapoints, datapoint.DataPoint{
    Name:  "netscaler.service.surge_queue_length",
    Value: float32(getFloat(service, "surgecount")),
    Tags:  serviceTags,
})
```

#### 3. Packets Per Second (PPS)
**Pourquoi**: Capacity planning, détection attaques
**API**: Dérivé de `totalpktsrecvd` dans system stats
**Difficulté**: Moyenne (calcul de rate)
**Impact**: Monitoring charge réseau

#### 4. Established Connections
**Pourquoi**: vs current connections pour capacity planning
**API**: `establishedconn` dans lbvserver stats
**Difficulté**: Facile
**Impact**: Meilleure visibilité charge

### Priorité Moyenne

#### 5. Health Check Latency
**Pourquoi**: Diagnostic perfs backends
**API**: Health monitoring data (parsing requis)
**Difficulté**: Difficile
**Impact**: Troubleshooting lenteurs

#### 6. Interfaces Réseau
**Pourquoi**: Détection erreurs réseau, drops
**API**: `interface` resource
**Difficulté**: Moyenne
**Impact**: Diagnostic problèmes réseau

**Métriques par interface**:
- State (UP/DOWN)
- RX/TX bytes
- RX/TX errors
- RX/TX drops
- Link speed

#### 7. Request Distribution (équilibrage effectif)
**Pourquoi**: Vérifier équilibrage entre backends
**API**: `totalhits` par service member
**Difficulté**: Moyenne
**Impact**: Audit équilibrage

---

## 🚀 Phase 3 - Modules Avancés (Q2 2025 - si utilisés)

### Content Switching (si utilisé)

**Ressources**:
- `csvserver` - Content Switching vServers
- `cspolicy` - Policies et rules

**Métriques**:
- Hits par policy
- Règles matchées vs non matchées
- État CS vServers

**Difficulté**: Moyenne
**Impact**: Optimisation routing applicatif

---

### GSLB (si multi-sites)

**Ressources**:
- `gslbvserver` - GSLB virtual servers
- `gslbsite` - Sites distants
- `gslbservice` - Services GSLB

**Métriques**:
- État des sites distants
- Latence inter-sites (MEP - Metric Exchange Protocol)
- Distribution géographique des requêtes
- Persistence records

**Tags additionnels**:
- `site` - Site name (paris, london, nyc)
- `site_role` - primary, secondary, dr
- `datacenter` - Datacenter location

**Difficulté**: Moyenne-Haute
**Impact**: Monitoring multi-sites, DR

---

### Cache & Compression (si activé)

**Ressources**:
- `cache` - Integrated cache stats
- `cmp` - Compression stats

**Métriques Cache**:
- Cache hit ratio
- Objets en cache
- Mémoire cache utilisée
- Cache misses

**Métriques Compression**:
- Compression ratio
- Bandwidth savings
- Bytes compressés vs originaux

**Difficulté**: Facile
**Impact**: Optimisation performances

---

### AAA/Authentication (si Gateway utilisé)

**Ressources**:
- `aaauser` - Users authentifiés
- `aaagroup` - Groupes
- `authenticationvserver` - Auth vServers

**Métriques**:
- Authentifications réussies/échouées
- Sessions AAA actives
- Latence LDAP/RADIUS
- Failed login attempts (sécurité)

**Difficulté**: Moyenne
**Impact**: Sécurité, troubleshooting auth

---

### Citrix Gateway / VPN (si utilisé)

**Ressources**:
- `vpnvserver` - VPN virtual servers
- `ica` - ICA sessions (Citrix Virtual Apps)

**Métriques**:
- Sessions ICA actives
- Connexions VPN établies
- Bandwidth consommée par VPN
- Session setup time
- Échecs d'authentification

**Tags additionnels**:
- `app_name` - Application Citrix
- `user` - Username (si non-PII)

**Difficulté**: Moyenne
**Impact**: Monitoring Citrix Gateway

---

### Application Firewall / WAF (si activé)

**Ressources**:
- `appfw` - Application Firewall
- `appfwprofile` - WAF profiles
- `appfwpolicy` - WAF policies

**Métriques**:
- Violations détectées par catégorie (XSS, SQLi, etc.)
- Requêtes bloquées vs loguées
- Top signatures déclenchées
- False positives rate

**Difficulté**: Haute
**Impact**: Sécurité applicative

---

## 📊 Matrice de Priorisation

```
┌────────────────────────────┬──────────┬──────────┬────────────┐
│ Fonctionnalité             │ Priorité │ Effort   │ Impact     │
├────────────────────────────┼──────────┼──────────┼────────────┤
│ PHASE 2 - MÉTRIQUES AVANCÉES                                  │
├────────────────────────────┼──────────┼──────────┼────────────┤
│ Spillover count            │ 🔴 Haute │ 1 jour   │ Production │
│ Surge queue                │ 🔴 Haute │ 1 jour   │ Production │
│ PPS (packets/sec)          │ 🟡 Moyen │ 2 jours  │ Monitoring │
│ Established connections    │ 🟡 Moyen │ 0.5 jour │ Monitoring │
│ Health check latency       │ 🟡 Moyen │ 3 jours  │ Diagnostic │
│ Interfaces réseau          │ 🟡 Moyen │ 2 jours  │ Diagnostic │
│ Request distribution       │ 🟢 Basse │ 2 jours  │ Audit      │
├────────────────────────────┼──────────┼──────────┼────────────┤
│ PHASE 3 - MODULES AVANCÉS                                     │
├────────────────────────────┼──────────┼──────────┼────────────┤
│ Content Switching          │ 🟢 Basse │ 2 jours  │ Si utilisé │
│ GSLB                       │ 🟡 Moyen │ 3 jours  │ Si multi-  │
│ Cache & Compression        │ 🟢 Basse │ 1 jour   │ Optimiz.   │
│ AAA/Authentication         │ 🟡 Moyen │ 2 jours  │ Si Gateway │
│ Gateway/VPN                │ 🟡 Moyen │ 2 jours  │ Si Gateway │
│ Application Firewall       │ 🟢 Basse │ 3 jours  │ Si WAF     │
└────────────────────────────┴──────────┴──────────┴────────────┘
```

---

## 🎯 Recommandations

### Pour Production Immédiate

**Phase 1 suffit** pour 90% des besoins :
- ✅ Monitoring santé système
- ✅ État vServers/Services/ServiceGroups
- ✅ Alertes certificats SSL
- ✅ Monitoring HA
- ✅ Surveillance disques
- ✅ Corrélation complète (tags enrichis)

**Action** : Déployer en production, configurer alertes, monitorer.

---

### Pour Q1 2025 (Phase 2)

**Si besoin détecté** :

**Saturation monitoring** :
- Spillover count (débordement vServers)
- Surge queue (saturation backends)
→ **2 jours de dev**

**Capacity planning** :
- PPS (packets per second)
- Established connections
→ **2-3 jours de dev**

**Total Phase 2 priorité haute** : ~4-5 jours

---

### Pour Q2 2025 (Phase 3)

**Selon modules activés** sur votre Netscaler :

**Checklist** :
- [ ] Content Switching utilisé ? → Implémenter CS stats
- [ ] Multi-sites GSLB ? → Implémenter GSLB stats
- [ ] Cache activé ? → Implémenter cache stats
- [ ] Citrix Gateway ? → Implémenter VPN/AAA stats
- [ ] WAF activé ? → Implémenter AppFW stats

**Total Phase 3** : ~5-10 jours selon modules

---

## 📈 Coverage Actuel

```
┌──────────────────────────────────────────────────────────────┐
│ NETSCALER MONITORING COVERAGE                                │
├────────────────────────────────┬─────────────────────────────┤
│ Catégorie                      │ Coverage                    │
├────────────────────────────────┼─────────────────────────────┤
│ Santé Système                  │ ████████████████░░ 85%      │
│ Load Balancing (vServers)      │ ███████████████░░░ 75%      │
│ Services/Backends              │ ████████████████░░ 80%      │
│ SSL/TLS                        │ ███████████░░░░░░░ 55%      │
│ High Availability              │ ████████████████████ 100%   │
│ Disk Usage                     │ ████████████████████ 100%   │
│ Content Switching              │ ░░░░░░░░░░░░░░░░░░ 0%       │
│ GSLB                           │ ░░░░░░░░░░░░░░░░░░ 0%       │
│ Cache & Compression            │ ░░░░░░░░░░░░░░░░░░ 0%       │
│ AAA/Gateway                    │ ░░░░░░░░░░░░░░░░░░ 0%       │
│ Application Firewall           │ ░░░░░░░░░░░░░░░░░░ 0%       │
├────────────────────────────────┼─────────────────────────────┤
│ TOTAL (core features)          │ ███████████████░░░ 75%      │
│ TOTAL (all features)           │ █████████░░░░░░░░░ 45%      │
└────────────────────────────────┴─────────────────────────────┘
```

**Core features** = System, LB, Services, SSL, HA, Disk
**All features** = Core + CS, GSLB, Cache, AAA, WAF

---

## 🔧 Maintenance Continue

### Suivi du Fork SDK

**Tous les 3 mois** (prochaine : 2025-03-11) :
- [ ] Vérifier si [PR #36](https://github.com/citrix/adc-nitro-go/pull/36) mergée
- [ ] Tester SDK officiel si mergé
- [ ] Revert au SDK officiel si OK
- [ ] Mettre à jour doc TEMPORARY-FORK

### Évolutions Générales

**Continue** :
- Ajout métriques selon besoins production
- Optimisation cache (si perf issues)
- Nouvelles corrélations (si demandées)
- Support nouvelles versions Netscaler

---

## 📚 Documentation de Référence

**Analyses complètes** :
- `NETSCALER-METRICS-GAP-ANALYSIS.md` - Détail 58 métriques identifiées
- `NETSCALER-CORRELATION-ANALYSIS.md` - Axes de corrélation
- `NETSCALER-PHASE1-COMPLETE.md` - Phase 1 implémentation

**Maintenance** :
- `TEMPORARY-FORK-citrix-adc-nitro-go.md` - Fork tracking

**Code** :
- `internal/agent/probes/netscaler/` - Probe implementation
- `internal/agent/services/data_store/transformers/definitions/netscaler.yaml` - Metrics

---

## 💡 Décision : Que Développer Ensuite ?

### Option A : Rien (Recommandé pour démarrage)
**Déployer Phase 1 en production** → Monitorer 1-2 mois → Identifier besoins réels

**Avantages** :
- Phase 1 couvre 75% core features
- Évite sur-engineering
- Basé sur usage réel

### Option B : Phase 2 Haute Priorité (si besoin immédiat)
**Spillover + Surge queue** = 2 jours dev

**Si** :
- Historique de saturation vServers
- Problèmes backends surchargés
- Besoin alertes précoces

### Option C : Phase 3 Modules Spécifiques (selon config)
**Développer selon modules activés**

**Si** :
- GSLB configuré → implémenter GSLB stats
- Gateway Citrix → implémenter VPN stats
- WAF activé → implémenter AppFW stats

---

## ❓ Questions pour Prioriser

Pour décider de la suite :

1. **Avez-vous des saturations vServer/backend ?** → Phase 2 Spillover/Surge
2. **Utilisez-vous GSLB multi-sites ?** → Phase 3 GSLB
3. **Citrix Gateway en production ?** → Phase 3 AAA/VPN
4. **WAF activé ?** → Phase 3 AppFW
5. **Problèmes perfs spécifiques ?** → Phase 2 Health check latency

**Sinon** → Déployer Phase 1, monitorer, adapter

---

**Status** : ✅ Phase 1 Complete - Production Ready
**Prochaine révision** : Après 1 mois de production
**Dernière mise à jour** : 2025-12-11
