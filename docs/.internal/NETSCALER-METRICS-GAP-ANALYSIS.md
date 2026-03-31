# Netscaler Metrics - Gap Analysis

**Date**: 2025-12-11
**Status**: 📊 Current Implementation vs Requirements

---

## Executive Summary

**Current Coverage**: ~45% des métriques souhaitées
**Priorités OK**: 4/5 (manque certificats SSL)
**Métriques collectées**: 23 métriques
**Métriques manquantes**: ~35 métriques identifiées

---

## 1. Santé Système de l'Appliance

### ✅ Implémenté (8 métriques)

| Métrique | Status | API Field | Notes |
|----------|--------|-----------|-------|
| CPU Usage Total | ✅ | `cpuusagepcnt` | System-wide |
| CPU Management Plane | ✅ | `mgmtcpuusagepcnt` | Control plane only |
| Mémoire utilisée | ✅ | `memusagepcnt` | % utilisation |
| Throughput RX | ✅ | `rxmbitsrate` | Mbits/s |
| Throughput TX | ✅ | `txmbitsrate` | Mbits/s |
| HTTP Requests Rate | ✅ | `httprequestsrate` | req/s |
| HTTP Responses Rate | ✅ | `httpresponsesrate` | resp/s |
| TCP Connections (client+server) | ✅ | `tcpcurclientconn`, `tcpcurserverconn` | Current |

### ❌ Manquant

| Métrique | Priorité | API Resource | Difficulté | Notes |
|----------|----------|--------------|------------|-------|
| Température | 🔴 Haute | `system` (hardware) | Moyenne | Physiques uniquement |
| État ventilateurs | 🔴 Haute | `system` (hardware) | Moyenne | Physiques uniquement |
| État disques | 🟡 Moyenne | `systemdiskusage` | Facile | Espace /var/log critique |
| Uptime | 🟢 Basse | `system` (`starttime`) | Facile | Calcul depuis boot |
| État HA | 🔴 Haute | `hanode` | Moyenne | primary/secondary/sync |
| Packets per second | 🟡 Moyenne | `system` (`totalpktsrecvd`) | Facile | Dérivé rate |
| Interfaces réseau | 🟡 Moyenne | `interface` | Moyenne | link state, errors, drops |

**Recommandation**: Ajouter en priorité **HA state** et **disk usage** (logs qui remplissent = panne).

---

## 2. Load Balancing (vServers)

### ✅ Implémenté (5 métriques par vServer)

| Métrique | Status | API Field | Tags Discriminants |
|----------|--------|-----------|-------------------|
| État UP/DOWN | ✅ | `state` | `vserver` |
| Connexions actives | ✅ | `curclntconnections` | `vserver` |
| Requests/sec | ✅ | `requestsrate` | `vserver` |
| Bytes RX/sec | ✅ | `requestbytesrate` | `vserver` |
| Bytes TX/sec | ✅ | `responsebytesrate` | `vserver` |

### ❌ Manquant

| Métrique | Priorité | API Field | Difficulté | Notes |
|----------|----------|-----------|------------|-------|
| Temps de réponse moyen | 🟡 Moyenne | Health monitoring data | Difficile | Nécessite parsing health checks |
| Spillover count | 🟡 Moyenne | `totspillovers` | Facile | Débordement backup |
| Hits totaux | 🟢 Basse | `tothits` | Facile | Compteur cumulé |
| Established connections | 🟢 Basse | `establishedconn` | Facile | vs `curclntconnections` |

**Recommandation**: Ajouter **spillover** (alerte saturation) et **established connections** (capacity planning).

---

## 3. Services et Service Groups

### ✅ Implémenté

**Services** (3 métriques/service):
- État UP/DOWN ✅
- Throughput bytes/sec ✅
- Active transactions ✅

**ServiceGroups** (3 métriques/group):
- État UP/DOWN ✅
- Requests rate ✅
- Active members ✅

### ❌ Manquant

| Métrique | Priorité | API Resource | Difficulté | Notes |
|----------|----------|--------------|------------|-------|
| Health check latency | 🔴 Haute | Health monitoring | Difficile | Détection lenteur backend |
| Surge queue length | 🟡 Moyenne | `surgecount` | Facile | File d'attente saturée |
| Request distribution | 🟢 Basse | `totalhits` par membre | Moyenne | Vérif équilibrage effectif |
| Inactive members | 🟡 Moyenne | Calcul `totalservers - activemembers` | Facile | ServiceGroups |

**Recommandation**: Ajouter **surge queue** (alerte saturation backend) et **health check latency** (diagnostic perfs).

---

## 4. SSL/TLS

### ✅ Implémenté (2 métriques)

| Métrique | Status | API Field |
|----------|--------|-----------|
| Transactions SSL/sec | ✅ | `ssltransactionsrate` |
| Sessions SSL actives | ✅ | `sslsessiontot` |

### ❌ Manquant (CRITIQUE!)

| Métrique | Priorité | API Resource | Difficulté | Notes |
|----------|----------|--------------|------------|-------|
| **Expiration certificats** | 🔴 **CRITIQUE** | `sslcertkey` | Facile | **Alerte 30/15/7j avant** |
| Handshakes réussis | 🟡 Moyenne | `sslnumhandshakeshits` | Facile | Monitoring erreurs |
| Handshakes échoués | 🟡 Moyenne | `sslnumhandshaketimeouts` | Facile | Erreurs clients |
| Cache sessions hit ratio | 🟢 Basse | `sslsessionhits` / `sslsessionmiss` | Facile | Optimisation perfs |
| Cipher suites | 🟢 Basse | `sslcipher` bindings | Moyenne | Audit sécurité |

**⚠️ PRIORITÉ ABSOLUE**: Implémenter **monitoring expiration certificats** → éviter outages production!

---

## 5. Content Switching & GSLB

### ❌ Tout manquant

| Catégorie | Priorité | API Resources | Difficulté |
|-----------|----------|---------------|------------|
| Content Switching | 🟢 Basse | `csvserver`, `cspolicy` | Moyenne |
| GSLB (multi-sites) | 🟢 Basse | `gslbvserver`, `gslbsite` | Moyenne |

**Recommandation**: Phase 2 (usage moins critique que LB standard).

---

## 6. Caching & Compression

### ❌ Tout manquant

| Métrique | Priorité | API Resource | Difficulté |
|----------|----------|--------------|------------|
| Cache hit ratio | 🟡 Moyenne | `cache` | Facile |
| Compression ratio | 🟢 Basse | `cmp` | Facile |
| Bandwidth savings | 🟢 Basse | Calcul | Moyenne |

**Recommandation**: Phase 2 (optimisation, pas critique).

---

## 7. Sécurité (Modules Avancés)

### ❌ Tout manquant

| Module | Priorité | API Resources | Difficulté | Notes |
|--------|----------|---------------|------------|-------|
| AAA/Auth | 🟡 Moyenne | `aaauser`, `aaagroup` | Moyenne | Si Gateway utilisé |
| Application Firewall | 🟢 Basse | `appfw` | Difficile | Si WAF activé |
| Gateway/VPN | 🟡 Moyenne | `vpnvserver`, `ica` | Moyenne | Citrix Gateway |

**Recommandation**: Phase 3 (si modules utilisés).

---

## 8. Analyse des Priorités Utilisateur

### Priorités définies (5 items)

| Priorité | Métrique | Status | Notes |
|----------|----------|--------|-------|
| 1 | État des vServers (UP/DOWN) | ✅ Implémenté | OK |
| 2 | État des backends | ✅ Implémenté | Services + ServiceGroups |
| 3 | **Certificats SSL** | ❌ **MANQUANT** | **⚠️ CRITIQUE** |
| 4 | CPU/Mémoire appliance | ✅ Implémenté | OK |
| 5 | Connexions actives | ✅ Implémenté | OK |

**✅ 4/5 priorités OK**
**❌ Certificats SSL manquants** → **risque production élevé!**

---

## Roadmap Recommandée

### Phase 1 - Métriques Critiques Manquantes (Sprint actuel)

**Priorité HAUTE** (éviter outages):
1. ✅ **Fork SDK Citrix** (fait)
2. ❌ **Expiration certificats SSL** (30/15/7 jours)
3. ❌ **État HA** (primary/secondary/sync status)
4. ❌ **Disk usage** (/var/log rempli = panne)

**Priorité MOYENNE** (amélioration monitoring):
5. ❌ **Spillover count** (saturation vServers)
6. ❌ **Surge queue** (saturation backends)
7. ❌ **PPS** (packets per second)

### Phase 2 - Métriques Avancées (Q1 2025)

- Health check latency (diagnostic perfs)
- Interfaces réseau (link state, errors)
- Cache & Compression stats
- Inactive members tracking

### Phase 3 - Modules Sécurité (Q2 2025 - si utilisés)

- AAA/Authentication monitoring
- Gateway/VPN sessions
- Application Firewall (si WAF activé)

---

## Actions Immédiates

### 1. Certificats SSL (CRITIQUE)

```go
// Nouvelle fonction à implémenter
func (p *netscalerProbe) collectSSLCertificateStats() {
    certs, _ := p.client.FindAllResources("sslcertkey")
    for _, cert := range certs {
        expiryDate := cert["daystoexpiration"]
        // Alert si < 30 jours
        // Channel: "SSL Certificate Expiration ({certname})"
    }
}
```

**API**: `GET /nitro/v1/config/sslcertkey`
**Fields**: `certkey`, `daystoexpiration`, `expirydate`

### 2. État HA

```go
func (p *netscalerProbe) collectHAStats() {
    hanode, _ := p.client.FindAllStats("hanode")
    // State: PRIMARY, SECONDARY, UNKNOWN
    // syncstatus: SUCCESS, IN PROGRESS, FAILED
}
```

**API**: `GET /nitro/v1/stat/hanode`
**Fields**: `hacurstatus`, `hacurstate`, `hasyncfailures`

### 3. Disk Usage

```go
func (p *netscalerProbe) collectDiskStats() {
    disk, _ := p.client.FindAllStats("systemdiskusage")
    // Alert si /var/log > 80%
}
```

**API**: `GET /nitro/v1/stat/systemdiskusage`
**Fields**: `used`, `avail`, `percentused`

---

## Métriques par Catégorie - Résumé Visuel

```
┌──────────────────────────────────────────────┐
│ SANTÉ SYSTÈME                                │
│ ✅✅✅✅✅✅✅✅ (8/8 basic)               │
│ ❌❌❌❌ (4 advanced manquants)              │
├──────────────────────────────────────────────┤
│ LOAD BALANCING                               │
│ ✅✅✅✅✅ (5/5 basic vServers)              │
│ ❌❌❌ (3 advanced manquants)                │
├──────────────────────────────────────────────┤
│ SERVICES/BACKENDS                            │
│ ✅✅✅✅✅✅ (6/6 basic)                     │
│ ❌❌❌ (3 advanced manquants)                │
├──────────────────────────────────────────────┤
│ SSL/TLS                                      │
│ ✅✅ (2/7 basic)                             │
│ ❌❌❌❌❌ (5 manquants dont 1 CRITIQUE)     │
├──────────────────────────────────────────────┤
│ ADVANCED (CS, GSLB, Cache, Sécurité)        │
│ ❌❌❌❌❌❌❌❌ (tout manquant)              │
└──────────────────────────────────────────────┘

TOTAL: 23/~58 métriques identifiées (~40%)
```

---

## Conclusion

**Points forts actuels**:
- ✅ Monitoring de base solide (vServers, Services, System)
- ✅ Tags discriminants fonctionnels (vserver, service, servicegroup)
- ✅ SDK fix appliqué (singleton resources)

**Risques identifiés**:
- 🔴 **CRITIQUE**: Pas de monitoring expiration certificats SSL
- 🟡 **HAUTE**: Pas de monitoring état HA (risque split-brain)
- 🟡 **HAUTE**: Pas de monitoring disk usage (logs qui saturent)

**Recommandation**: Implémenter Phase 1 (3 métriques critiques) en priorité avant ajout d'autres fonctionnalités.

---

**Dernière mise à jour**: 2025-12-11
**Prochaine révision**: Après implémentation Phase 1
