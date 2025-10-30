# Time Series Key Design - Engineering Rules

**Date:** 2025-10-30
**Version:** 1.0
**Status:** DRAFT - En révision avant implémentation

---

## 🎯 Objectif

Définir les règles d'ingénierie qui garantissent l'**unicité** et la **stabilité** des clés de séries temporelles dans le cache de métriques de l'agent SenHub.

---

## 📚 Concepts fondamentaux

### 1. Qu'est-ce qu'une série temporelle (Time Series) ?

Une **série temporelle** est une séquence de points de données indexés dans le temps pour une métrique spécifique avec un ensemble unique de labels/tags.

**Exemple VictoriaMetrics/Prometheus :**
```
cpu_usage{host="server1",core="0"} → Série 1
cpu_usage{host="server1",core="1"} → Série 2
cpu_usage{host="server2",core="0"} → Série 3
```

**Dans notre système :**
```
cpu:cpu.usage:core=0 → Série 1
cpu:cpu.usage:core=1 → Série 2
network:network.bytes_sent:interface=eth0 → Série 3
```

### 2. Qu'est-ce que la cardinalité ?

La **cardinalité** est le nombre total de séries temporelles uniques dans le système.

**Formule :**
```
Cardinalité = Nombre de métriques × Nombre de combinaisons uniques de labels
```

**Exemple :**
- 1 métrique: `cpu.usage`
- 2 labels: `host` (10 valeurs) × `core` (8 valeurs)
- **Cardinalité = 1 × 10 × 8 = 80 séries temporelles**

### 3. Problème de haute cardinalité

**Cardinalité explosive :**
```
# Mauvais : endpoint dans la clé
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.100,drive_id=0
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.101,drive_id=0  ← Nouvelle série !

# Si l'IP change → nouvelle série → perte d'historique
# Si 1000 équipements → 1000 × 12 drives = 12000 séries
```

**Cardinalité optimale :**
```
# Bon : endpoint dans les métadonnées, pas dans la clé
baie_prod:hardware.storage.drive.health:drive_id=0  # metadata: {endpoint: "https://..."}
baie_prod:hardware.storage.drive.health:drive_id=1

# L'IP peut changer → même série → historique préservé
# Pour 1000 équipements avec noms uniques → 1000 × 12 = 12000 séries (identique mais stable)
```

---

## 🔑 Règle Universelle d'Unicité (RUU)

### Définition

> **Une clé de série temporelle DOIT être unique SI ET SEULEMENT SI les valeurs des métriques collectées à cet instant peuvent être DIFFÉRENTES.**

### Formulation mathématique

```
ts_key = f(probe_name, metric_name, discriminant_tags)

Où discriminant_tags = { tags qui différencient les instances d'une même métrique }
```

### Règle de collision (à éviter)

```
❌ COLLISION si :
   ts_key₁ = ts_key₂  ET  metric_value₁ ≠ metric_value₂
```

### Règle de granularité (à éviter)

```
❌ PERTE DE GRANULARITÉ si :
   ts_key₁ ≠ ts_key₂  MAIS  ils représentent la même ressource physique
```

---

## 📋 Taxonomie des Tags

Notre système classe les tags en 3 catégories :

### 1. Tags Discriminants (dans la clé)

**Critères :**
- Identifient une **instance physique ou logique unique**
- Les valeurs de métriques PEUVENT être différentes entre instances
- Doivent rester **stables dans le temps**

**Exemples :**
- `core`: CPU core 0, 1, 2 → chacun a un usage différent
- `drive_id`: Drive 0, Drive 1 → chacun a des métriques différentes
- `interface`: eth0, wlan0 → chacun a un trafic différent
- `volume_id`: Volume unique dans le système

### 2. Tags Contextuels (dans metadata)

**Critères :**
- Fournissent du **contexte** mais ne discriminent pas
- Peuvent **changer** dans le temps (IP, hostname DNS)
- Utiles pour **filtrage et affichage**

**Exemples :**
- `endpoint`: URL/IP de l'équipement (peut changer)
- `hostname`: Nom DNS (peut changer)
- `manufacturer`: Dell, HPE (informatif)
- `vendor`: storage, server (informatif)

### 3. Tags Redondants (exclus)

**Critères :**
- Déjà présents dans `probe_name` ou `metric_name`
- ID internes techniques sans valeur sémantique

**Exemples :**
- `probe_name`: déjà dans la clé
- `host`: souvent identique au probe
- `prtg_metric_id`: ID interne

---

## 🧪 Tests d'Unicité par Probe

### Test 1: CPU Probe

**Métriques :**
```
cpu.usage → Mesurée PAR CORE
cpu.frequency → Mesurée PAR CORE
```

**Question d'unicité :**
> "Est-ce que l'usage CPU du core 0 peut être différent du core 1 ?"
> **OUI** → `core` est discriminant

**Clés correctes :**
```
✅ cpu:cpu.usage:core=0
✅ cpu:cpu.usage:core=1
✅ cpu:cpu.usage:core=total

❌ cpu:cpu.usage  ← COLLISION ! Les 4 cores écrasent la même clé
```

**Test de non-régression :**
```go
// 4 cores doivent créer 4 clés différentes
assert len(cache.timeSeries) == 4 // core=0,1,2,total
assert cache.timeSeries["cpu:cpu.usage:core=0"].Value != cache.timeSeries["cpu:cpu.usage:core=1"].Value
```

---

### Test 2: Network Probe

**Métriques :**
```
network.bytes_sent → Mesurée PAR INTERFACE
network.packets_received → Mesurée PAR INTERFACE
```

**Question d'unicité :**
> "Est-ce que le trafic sur eth0 peut être différent de wlan0 ?"
> **OUI** → `interface` est discriminant

**Clés correctes :**
```
✅ network:network.bytes_sent:interface=eth0
✅ network:network.bytes_sent:interface=wlan0

❌ network:network.bytes_sent  ← COLLISION !
```

**Test de non-régression :**
```go
// 2 interfaces doivent créer 2 clés différentes
assert len(cache.timeSeries) == 2
assert cache.timeSeries["network:network.bytes_sent:interface=eth0"].Value !=
       cache.timeSeries["network:network.bytes_sent:interface=wlan0"].Value
```

---

### Test 3: Redfish Probe (complexe)

**Métriques :**
```
hardware.storage.drive.health → Mesurée PAR DRIVE PAR CONTROLLER
hardware.storage.pool.capacity → Mesurée PAR POOL PAR CONTROLLER
hardware.power.health → Mesurée PAR PSU
```

**Questions d'unicité :**

1. **Drives :**
   > "Est-ce que le Drive 0 du contrôleur A peut être différent du Drive 0 du contrôleur B ?"
   > **OUI (physiquement ce sont 2 disques différents)** → `controller` + `drive_id` discriminants

2. **Endpoint :**
   > "Si je change l'IP du contrôleur de 192.168.1.100 à 192.168.1.200, est-ce le même disque ?"
   > **OUI** → `endpoint` N'est PAS discriminant, c'est du contexte

**Clés correctes :**
```
✅ redfish:hardware.storage.drive.health:controller=A:drive_id=0
✅ redfish:hardware.storage.drive.health:controller=B:drive_id=0
✅ redfish:hardware.storage.pool.capacity:controller=A:pool_name=A

❌ redfish:hardware.storage.drive.health:drive_id=0
   ← COLLISION ! Controller A et B écrasent

❌ redfish:hardware.storage.drive.health:endpoint=https://...:drive_id=0
   ← Changement IP = perte historique
```

**Test de non-régression :**
```go
// 2 controllers × 12 drives = 24 clés différentes
assert len(cache.timeSeries) == 24

// Drive 0 du controller A ≠ Drive 0 du controller B
keyA := "redfish:hardware.storage.drive.health:controller=A:drive_id=0"
keyB := "redfish:hardware.storage.drive.health:controller=B:drive_id=0"
assert cache.timeSeries[keyA] exists
assert cache.timeSeries[keyB] exists
assert keyA != keyB

// Endpoint doit être dans metadata, pas dans la clé
assert cache.timeSeries[keyA].Tags["endpoint"] == "https://lb-me5024mgmt1.batistyl.fr"
```

---

### Test 4: Deux probes Redfish vers même endpoint

**Configuration :**
```yaml
probes:
  - name: baie_production    # Probe 1
    type: redfish
    params:
      endpoint: "https://lb-me5024mgmt1.batistyl.fr"

  - name: baie_backup        # Probe 2 (FUTURE - autre équipement)
    type: redfish
    params:
      endpoint: "https://lb-me5024mgmt2.batistyl.fr"  # Endpoint différent
```

**Question d'unicité :**
> "Si 2 probes Redfish surveillent 2 équipements différents, sont-ce des séries différentes ?"
> **OUI** → `probe_name` est discriminant

**Clés correctes :**
```
✅ baie_production:hardware.storage.drive.health:controller=A:drive_id=0
✅ baie_backup:hardware.storage.drive.health:controller=A:drive_id=0

Ces 2 clés sont différentes grâce au probe_name !
```

**Test de non-régression :**
```go
// 2 probes × 24 drives = 48 clés différentes
assert len(cache.timeSeries) == 48

// Les clés sont distinctes par probe name
keyProd := "baie_production:hardware.storage.drive.health:controller=A:drive_id=0"
keyBackup := "baie_backup:hardware.storage.drive.health:controller=A:drive_id=0"
assert cache.timeSeries[keyProd].Tags["endpoint"] == "https://lb-me5024mgmt1.batistyl.fr"
assert cache.timeSeries[keyBackup].Tags["endpoint"] == "https://lb-me5024mgmt2.batistyl.fr"
```

---

## 🎯 Algorithme de Génération de Clé

### Pseudocode

```python
def generate_ts_key(probe_name, metric_name, all_tags):
    """
    Génère une clé unique pour une série temporelle

    Règle: La clé doit contenir UNIQUEMENT les tags qui discriminent
           les instances multiples d'une même métrique
    """

    # Étape 1: Identifier les tags discriminants pour ce probe
    discriminant_tags = get_discriminant_tags_for_probe(probe_name)

    # Étape 2: Extraire les valeurs présentes
    key_parts = [probe_name, metric_name]

    for tag_name in discriminant_tags:  # Ordre fixe pour cohérence
        if tag_name in all_tags:
            key_parts.append(f"{tag_name}={all_tags[tag_name]}")

    # Étape 3: Joindre avec séparateur
    ts_key = ":".join(key_parts)

    return ts_key
```

### Liste des Tags Discriminants (Registry)

```go
var DiscriminantTagsRegistry = map[string][]string{
    // Probes système
    "cpu":         {"core"},
    "memory":      {},  // Pas de tags discriminants (métrique système globale)
    "network":     {"interface", "adapter"},
    "logicaldisk": {"drive", "mount_point", "device"},

    // Probes applicatifs
    "citrix":      {"metric_type", "failure_category"},
    "webapp":      {"url"},

    // Probes hardware
    "redfish": {
        "controller", "controller_id",
        "drive_id", "drive_name",
        "volume_id", "volume_name",
        "pool_name", "pool_id",
        "psu_name",
        "processor_id",
        "memory_module_id",
    },

    // Probes events
    "winevents": {"event_id", "source"},
    "syslog":    {"event_id", "source"},
}
```

---

## ✅ Checklist de Validation

Avant d'implémenter un changement de clé, vérifier :

### 1. Test d'unicité
```
□ Pour chaque probe, identifier TOUTES les métriques multi-instances
□ Pour chaque métrique, identifier les tags qui la rendent unique
□ Vérifier qu'aucune collision ne peut se produire
```

### 2. Test de stabilité
```
□ Si l'endpoint change, la clé reste-t-elle la même ? (OUI requis)
□ Si le hostname change, la clé reste-t-elle la même ? (OUI requis)
□ Si l'IP change, la clé reste-t-elle la même ? (OUI requis)
```

### 3. Test de cardinalité
```
□ Nombre de séries = attendu ? (pas d'explosion)
□ Nombre de séries × rétention × fréquence = mémoire acceptable ?
```

### 4. Test de filtrage
```
□ Les tags contextuels (endpoint, etc.) sont-ils dans metric.Tags ? (OUI requis)
□ L'interface web peut-elle filtrer par endpoint ? (OUI requis)
□ L'API /info/tags retourne-t-elle tous les tags ? (OUI requis)
```

### 5. Test de migration
```
□ Les anciennes clés sont-elles compatibles ? (Si migration)
□ Y a-t-il une période de transition ? (Si migration)
□ Les dashboards externes continuent-ils de fonctionner ? (OUI requis)
```

---

## 🚨 Cas d'Erreur Fréquents

### Erreur 1: Oubli d'un tag discriminant

**Symptôme :** Métriques qui s'écrasent mutuellement

**Exemple :**
```go
// ❌ MAUVAIS : Oubli de "controller"
tsKey := fmt.Sprintf("%s:%s:drive_id=%s", probe, metric, driveID)
// Résultat: Drive 0 du controller A écrase Drive 0 du controller B

// ✅ BON
tsKey := fmt.Sprintf("%s:%s:controller=%s:drive_id=%s", probe, metric, controller, driveID)
```

**Détection :**
```go
// Test unitaire
func TestNoCollision(t *testing.T) {
    cache := NewCache()

    cache.Add(DataPoint{Name: "metric", Tags: {controller: "A", drive: "0"}, Value: 10})
    cache.Add(DataPoint{Name: "metric", Tags: {controller: "B", drive: "0"}, Value: 20})

    // ❌ Si collision, len == 1 (la 2ème valeur écrase la 1ère)
    // ✅ Si OK, len == 2
    assert.Equal(t, 2, len(cache.timeSeries))
}
```

---

### Erreur 2: Tag contextuel dans la clé

**Symptôme :** Perte d'historique lors d'un changement d'infrastructure

**Exemple :**
```go
// ❌ MAUVAIS : endpoint dans la clé
tsKey := fmt.Sprintf("%s:%s:endpoint=%s:drive_id=%s", probe, metric, endpoint, driveID)
// Résultat: Changement IP = nouvelle série = graphes cassés

// ✅ BON : endpoint dans metadata uniquement
tsKey := fmt.Sprintf("%s:%s:drive_id=%s", probe, metric, driveID)
metadata := CachedMetric{..., Tags: {endpoint: endpoint, drive_id: driveID}}
```

**Détection :**
```go
// Test de stabilité
func TestStability(t *testing.T) {
    cache := NewCache()

    // T0: endpoint = "https://192.168.1.100"
    cache.Add(DataPoint{
        Name: "metric",
        Tags: {endpoint: "https://192.168.1.100", drive: "0"},
        Value: 10
    })

    initialKey := cache.GetKeys()[0]

    // T1: endpoint change → "https://192.168.1.200"
    cache.Add(DataPoint{
        Name: "metric",
        Tags: {endpoint: "https://192.168.1.200", drive: "0"},
        Value: 20
    })

    // ❌ Si endpoint dans clé: 2 clés différentes
    // ✅ Si endpoint dans metadata: même clé, valeur mise à jour
    assert.Equal(t, 1, len(cache.timeSeries))
    assert.Equal(t, initialKey, cache.GetKeys()[0])
    assert.Equal(t, 20, cache.timeSeries[initialKey].Value)  // Valeur mise à jour
    assert.Equal(t, "https://192.168.1.200", cache.timeSeries[initialKey].Tags["endpoint"])
}
```

---

## 📊 Exemples de Cardinalité

### Calcul pour environnement type

**Scénario : 100 serveurs surveillés**

```
Probes actifs:
- CPU (4 cores/serveur)
- Memory (1 métrique globale)
- Network (2 interfaces/serveur)
- LogicalDisk (3 disques/serveur)
- Redfish (50 serveurs avec 12 drives chacun)

Cardinalité par probe:
- CPU:         100 servers × 4 cores × 2 metrics = 800 séries
- Memory:      100 servers × 1 metric = 100 séries
- Network:     100 servers × 2 interfaces × 4 metrics = 800 séries
- LogicalDisk: 100 servers × 3 drives × 3 metrics = 900 séries
- Redfish:     50 servers × 12 drives × 8 metrics = 4800 séries

TOTAL: ~7400 séries temporelles

Mémoire estimée (avec 5min de rétention, 1 point/30s):
- Points/série: 10 points
- Taille/point: ~200 bytes (métadonnées + valeur)
- Mémoire: 7400 séries × 10 points × 200 bytes ≈ 15 MB

✅ Acceptable
```

**Impact du changement de clé :**
```
AVANT (avec endpoint dans clé):
- Si endpoint change → nouvelle série → cardinalité × 2
- 7400 → 14800 séries temporelles = 30 MB

APRÈS (endpoint dans metadata):
- Endpoint change → même série → cardinalité stable
- 7400 séries temporelles = 15 MB
- ✅ 50% de réduction mémoire en cas de changements infrastructure
```

---

## 🎓 Conclusion

### Règle d'Or

> **Une clé de série temporelle doit identifier de manière UNIQUE et STABLE une source de données, indépendamment des changements d'infrastructure.**

### Principes SOLID pour les clés

1. **S**table: La clé ne change pas si l'infrastructure change
2. **U**nique: Pas de collision entre séries différentes
3. **M**inimal: Seulement les tags discriminants
4. **M**etadata: Tags contextuels dans CachedMetric.Tags
5. **A**uditable: Tests automatiques de non-régression
6. **R**eproducible: Même données → même clé
7. **Y**ielding: Cardinalité maîtrisée

---

## 📚 Références

- **VictoriaMetrics:** https://docs.victoriametrics.com/keyConcepts.html#time-series
- **Prometheus Best Practices:** https://prometheus.io/docs/practices/naming/
- **Time Series Database Concepts:** https://en.wikipedia.org/wiki/Time_series_database
- **Cardinality in Monitoring:** https://www.robustperception.io/cardinality-is-key

---

**Document rédigé par:** Claude Code
**Reviewer requis:** Matthieu (User)
**Approbation:** ⏳ En attente
