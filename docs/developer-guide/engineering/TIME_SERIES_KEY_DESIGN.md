# Time Series Key Design - Engineering Rules

**Date:** 2025-11-05 (Updated)
**Version:** 1.1 - Implemented
**Status:** ACTIVE - Implementation Complete
**Branch:** feature/cache-key-discriminant-tags

---

## 🎯 Goal

Define the engineering rules that guarantee the **uniqueness** and the **stability** of time-series keys in the SenHub agent's metric cache.

---

## 📚 Core concepts

### 1. What is a time series?

A **time series** is a sequence of data points indexed in time, for one metric with one unique set of labels/tags.

**VictoriaMetrics/Prometheus example:**
```
cpu_usage{host="server1",core="0"} → Series 1
cpu_usage{host="server1",core="1"} → Series 2
cpu_usage{host="server2",core="0"} → Series 3
```

**In our system:**
```
cpu:cpu.usage:core=0 → Series 1
cpu:cpu.usage:core=1 → Series 2
network:network.bytes_sent:interface=eth0 → Series 3
```

### 2. What is cardinality?

**Cardinality** is the total number of unique time series in the system.

**Formula:**
```
Cardinality = number of metrics × number of unique label combinations
```

**Example:**
- 1 metric: `cpu.usage`
- 2 labels: `host` (10 values) × `core` (8 values)
- **Cardinality = 1 × 10 × 8 = 80 time series**

### 3. The high-cardinality problem

**Cardinalité explosive :**
```
# Bad: endpoint in the key
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.100,drive_id=0
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.101,drive_id=0  ← Nouvelle série !

# Si l'IP change → nouvelle série → perte d'historique
# Si 1000 équipements → 1000 × 12 drives = 12000 séries
```

**Cardinalité optimale :**
```
# Good: endpoint in the metadata, not in the key
baie_prod:hardware.storage.drive.health:drive_id=0  # metadata: {endpoint: "https://..."}
baie_prod:hardware.storage.drive.health:drive_id=1

# The IP can change → same series → history preserved
# For 1000 devices with unique names → 1000 × 12 = 12000 series (same count, but stable)
```

---

## 🔑 The universal uniqueness rule (UUR)

### Définition

> **A time-series key MUST be unique IF AND ONLY IF the metric values collected at that instant can DIFFER.**

### Formulation mathématique

```
ts_key = f(probe_name, metric_name, discriminant_tags)

Where discriminant_tags = { the tags that tell instances of one metric apart }
```

### The collision case (to avoid)

```
❌ COLLISION si :
   ts_key₁ = ts_key₂  ET  metric_value₁ ≠ metric_value₂
```

### The over-granularity case (to avoid)

```
❌ PERTE DE GRANULARITÉ si :
   ts_key₁ ≠ ts_key₂  BUT  they represent the same physical resource
```

---

## 📋 Tag taxonomy

Our system sorts tags into 3 categories:

### 1. Discriminant tags (in the key)

**Criteria:**
- identify a **unique physical or logical instance**
- metric values CAN differ between instances
- must stay **stable over time**

**Examples:**
- `core`: CPU core 0, 1, 2 → each has a different usage
- `drive_id`: Drive 0, Drive 1 → each has different metrics
- `interface`: eth0, wlan0 → each has different traffic
- `volume_id`: a unique volume in the system

### 2. Contextual tags (in metadata)

**Criteria:**
- provide **context** but do not discriminate
- can **change** over time (IP, DNS hostname)
- useful for **filtering and display**

**Examples:**
- `endpoint`: device URL/IP (can change)
- `hostname`: DNS name (can change)
- `manufacturer`: Dell, HPE (informational)
- `vendor`: storage, server (informational)

### 3. Redundant tags (excluded)

**Criteria:**
- already carried by `probe_name` or `metric_name`
- internal technical IDs with no semantic value

**Examples:**
- `probe_name`: already in the key
- `host`: often identical to the probe
- `prtg_metric_id`: internal ID

---

## 🧪 Per-probe uniqueness tests

### Test 1: CPU Probe

**Metrics:**
```
cpu.usage → Mesurée PAR CORE
cpu.frequency → Mesurée PAR CORE
```

**Uniqueness question:**
> "Can the CPU usage of core 0 differ from core 1?"
> **YES** → `core` is discriminant

**Correct keys:**
```
✅ cpu:cpu.usage:core=0
✅ cpu:cpu.usage:core=1
✅ cpu:cpu.usage:core=total

❌ cpu:cpu.usage  ← COLLISION! All 4 cores overwrite the same key
```

**Regression test:**
```go
// 4 cores must create 4 distinct keys
assert len(cache.timeSeries) == 4 // core=0,1,2,total
assert cache.timeSeries["cpu:cpu.usage:core=0"].Value != cache.timeSeries["cpu:cpu.usage:core=1"].Value
```

---

### Test 2: Network Probe

**Metrics:**
```
network.bytes_sent → measured PER INTERFACE
network.packets_received → measured PER INTERFACE
```

**Uniqueness question:**
> "Can traffic on eth0 differ from wlan0?"
> **YES** → `interface` is discriminant

**Correct keys:**
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

**Metrics:**
```
hardware.storage.drive.health → Mesurée PAR DRIVE PAR CONTROLLER
hardware.storage.pool.capacity → Mesurée PAR POOL PAR CONTROLLER
hardware.power.health → Mesurée PAR PSU
```

**Questions d'unicité :**

1. **Drives :**
   > "Can Drive 0 of controller A differ from Drive 0 of controller B?"
   > **YES (physically these are 2 different disks)** → `controller` + `drive_id` are discriminant

2. **Endpoint :**
   > "If I change the controller IP from 192.168.1.100 to 192.168.1.200, is it the same disk?"
   > **YES** → `endpoint` is NOT discriminant, it is context

**Correct keys:**
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

// Endpoint belongs in metadata, not in the key
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

**Uniqueness question:**
> "If 2 Redfish probes watch 2 different devices, are those different series?"
> **YES** → `probe_name` is discriminant

**Correct keys:**
```
✅ baie_production:hardware.storage.drive.health:controller=A:drive_id=0
✅ baie_backup:hardware.storage.drive.health:controller=A:drive_id=0

These 2 keys differ thanks to probe_name!
```

**Test de non-régression :**
```go
// 2 probes × 24 drives = 48 clés différentes
assert len(cache.timeSeries) == 48

// The keys are distinct per probe name
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
    Generate a unique key for one time series

    Rule: the key must contain ONLY the tags that discriminate
          multiple instances of one metric
    """

    # Step 1: identify the discriminant tags for this probe
    discriminant_tags = get_discriminant_tags_for_probe(probe_name)

    # Step 2: extract the values present
    key_parts = [probe_name, metric_name]

    for tag_name in discriminant_tags:  # fixed order, for consistency
        if tag_name in all_tags:
            key_parts.append(f"{tag_name}={all_tags[tag_name]}")

    # Step 3: join with a separator
    ts_key = ":".join(key_parts)

    return ts_key
```

### Discriminant tag list (registry)

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
□ For each probe, identify EVERY multi-instance metric
□ For each metric, identify the tags that make it unique
□ Verify that no collision can occur
```

### 2. Test de stabilité
```
□ If the endpoint changes, does the key stay the same? (YES required)
□ If the hostname changes, does the key stay the same? (YES required)
□ If the IP changes, does the key stay the same? (YES required)
```

### 3. Test de cardinalité
```
□ Series count as expected? (no explosion)
□ Nombre de séries × rétention × fréquence = mémoire acceptable ?
```

### 4. Test de filtrage
```
□ Are contextual tags (endpoint, etc.) in metric.Tags? (YES required)
□ Can the web interface filter by endpoint? (YES required)
□ Does the /info/tags API return every tag? (YES required)
```

### 5. Test de migration
```
□ Are the old keys compatible? (if migrating)
□ Is there a transition period? (if migrating)
□ Les dashboards externes continuent-ils de fonctionner ? (OUI requis)
```

---

## 🚨 Cas d'Erreur Fréquents

### Erreur 1: Oubli d'un tag discriminant

**Symptom:** metrics overwriting one another

**Example:**
```go
// ❌ MAUVAIS : Oubli de "controller"
tsKey := fmt.Sprintf("%s:%s:drive_id=%s", probe, metric, driveID)
// Result: Drive 0 of controller A overwrites Drive 0 of controller B

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

    // ❌ On collision, len == 1 (the 2nd value overwrites the 1st)
    // ✅ Si OK, len == 2
    assert.Equal(t, 2, len(cache.timeSeries))
}
```

---

### Mistake 2: a contextual tag in the key

**Symptôme :** Perte d'historique lors d'un changement d'infrastructure

**Example:**
```go
// ❌ BAD: endpoint in the key
tsKey := fmt.Sprintf("%s:%s:endpoint=%s:drive_id=%s", probe, metric, endpoint, driveID)
// Result: an IP change = a new series = broken graphs

// ✅ GOOD: endpoint in metadata only
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

    // ❌ With endpoint in the key: 2 different keys
    // ✅ With endpoint in metadata: same key, value updated
    assert.Equal(t, 1, len(cache.timeSeries))
    assert.Equal(t, initialKey, cache.GetKeys()[0])
    assert.Equal(t, 20, cache.timeSeries[initialKey].Value)  // Valeur mise à jour
    assert.Equal(t, "https://192.168.1.200", cache.timeSeries[initialKey].Tags["endpoint"])
}
```

---

## 📊 Exemples de Cardinalité

### Worked example for a typical environment

**Scénario : 100 serveurs surveillés**

```
Probes actifs:
- CPU (4 cores/serveur)
- Memory (1 métrique globale)
- Network (2 interfaces/serveur)
- LogicalDisk (3 disques/serveur)
- Redfish (50 servers with 12 drives each)

Cardinality per probe:
- CPU:         100 servers × 4 cores × 2 metrics = 800 séries
- Memory:      100 servers × 1 metric = 100 séries
- Network:     100 servers × 2 interfaces × 4 metrics = 800 séries
- LogicalDisk: 100 servers × 3 drives × 3 metrics = 900 séries
- Redfish:     50 servers × 12 drives × 8 metrics = 4800 séries

TOTAL: ~7400 séries temporelles

Estimated memory (5 min retention, 1 point/30 s):
- Points/série: 10 points
- Taille/point: ~200 bytes (métadonnées + valeur)
- Mémoire: 7400 séries × 10 points × 200 bytes ≈ 15 MB

✅ Acceptable
```

**Impact du changement de clé :**
```
BEFORE (endpoint in the key):
- Si endpoint change → nouvelle série → cardinalité × 2
- 7400 → 14800 séries temporelles = 30 MB

AFTER (endpoint in metadata):
- Endpoint change → même série → cardinalité stable
- 7400 séries temporelles = 15 MB
- ✅ 50% de réduction mémoire en cas de changements infrastructure
```

---

## 🎓 Conclusion

### The golden rule

> **A time-series key must identify a data source UNIQUELY and STABLY, independently of infrastructure changes.**

### SOLID principles for keys

1. **S**table: the key does not change when the infrastructure does
2. **U**nique: Pas de collision entre séries différentes
3. **M**inimal: discriminant tags only
4. **M**etadata: contextual tags live in CachedMetric.Tags
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

**Document written by:** Claude Code
**Reviewer requis:** Matthieu (User)
**Approbation:** ⏳ En attente
