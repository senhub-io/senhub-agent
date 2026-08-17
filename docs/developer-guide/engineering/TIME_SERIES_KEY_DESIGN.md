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

**Exploding cardinality:**
```
# Bad: endpoint in the key
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.100,drive_id=0
redfish:hardware.storage.drive.health:endpoint=https://192.168.1.101,drive_id=0  ← A new series!

# If the IP changes → a new series → history lost
# For 1000 devices → 1000 × 12 drives = 12000 series
```

**Optimal cardinality:**
```
# Good: endpoint in the metadata, not in the key
array_prod:hardware.storage.drive.health:drive_id=0  # metadata: {endpoint: "https://..."}
array_prod:hardware.storage.drive.health:drive_id=1

# The IP can change → same series → history preserved
# For 1000 devices with unique names → 1000 × 12 = 12000 series (same count, but stable)
```

---

## 🔑 The universal uniqueness rule (UUR)

### Definition

> **A time-series key MUST be unique IF AND ONLY IF the metric values collected at that instant can DIFFER.**

### Mathematical formulation

```
ts_key = f(probe_name, metric_name, discriminant_tags)

Where discriminant_tags = { the tags that tell instances of one metric apart }
```

### The collision case (to avoid)

```
❌ COLLISION if:
   ts_key₁ = ts_key₂  AND  metric_value₁ ≠ metric_value₂
```

### The over-granularity case (to avoid)

```
❌ GRANULARITY LOSS if:
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
cpu.usage → measured PER CORE
cpu.frequency → measured PER CORE
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

❌ network:network.bytes_sent  ← COLLISION!
```

**Regression test:**
```go
// 2 interfaces must create 2 distinct keys
assert len(cache.timeSeries) == 2
assert cache.timeSeries["network:network.bytes_sent:interface=eth0"].Value !=
       cache.timeSeries["network:network.bytes_sent:interface=wlan0"].Value
```

---

### Test 3: Redfish Probe (complex)

**Metrics:**
```
hardware.storage.drive.health → measured PER DRIVE PER CONTROLLER
hardware.storage.pool.capacity → measured PER POOL PER CONTROLLER
hardware.power.health → measured PER PSU
```

**Uniqueness questions:**

1. **Drives:**
   > "Can Drive 0 of controller A differ from Drive 0 of controller B?"
   > **YES (physically these are 2 different disks)** → `controller` + `drive_id` are discriminant

2. **Endpoint:**
   > "If I change the controller IP from 192.168.1.100 to 192.168.1.200, is it the same disk?"
   > **YES** → `endpoint` is NOT discriminant, it is context

**Correct keys:**
```
✅ redfish:hardware.storage.drive.health:controller=A:drive_id=0
✅ redfish:hardware.storage.drive.health:controller=B:drive_id=0
✅ redfish:hardware.storage.pool.capacity:controller=A:pool_name=A

❌ redfish:hardware.storage.drive.health:drive_id=0
   ← COLLISION! Controllers A and B overwrite each other

❌ redfish:hardware.storage.drive.health:endpoint=https://...:drive_id=0
   ← An IP change loses the history
```

**Regression test:**
```go
// 2 controllers × 12 drives = 24 distinct keys
assert len(cache.timeSeries) == 24

// Drive 0 of controller A ≠ Drive 0 of controller B
keyA := "redfish:hardware.storage.drive.health:controller=A:drive_id=0"
keyB := "redfish:hardware.storage.drive.health:controller=B:drive_id=0"
assert cache.timeSeries[keyA] exists
assert cache.timeSeries[keyB] exists
assert keyA != keyB

// Endpoint belongs in metadata, not in the key
assert cache.timeSeries[keyA].Tags["endpoint"] == "https://redfish-controller-1.example.com"
```

---

### Test 4: two Redfish probes against the same endpoint

**Configuration:**
```yaml
probes:
  - name: array_production    # Probe 1
    type: redfish
    params:
      endpoint: "https://redfish-controller-1.example.com"

  - name: array_backup        # Probe 2 (FUTURE — a different device)
    type: redfish
    params:
      endpoint: "https://redfish-controller-2.example.com"  # A different endpoint
```

**Uniqueness question:**
> "If 2 Redfish probes watch 2 different devices, are those different series?"
> **YES** → `probe_name` is discriminant

**Correct keys:**
```
✅ array_production:hardware.storage.drive.health:controller=A:drive_id=0
✅ array_backup:hardware.storage.drive.health:controller=A:drive_id=0

These 2 keys differ thanks to probe_name!
```

**Regression test:**
```go
// 2 probes × 24 drives = 48 distinct keys
assert len(cache.timeSeries) == 48

// The keys are distinct per probe name
keyProd := "array_production:hardware.storage.drive.health:controller=A:drive_id=0"
keyBackup := "array_backup:hardware.storage.drive.health:controller=A:drive_id=0"
assert cache.timeSeries[keyProd].Tags["endpoint"] == "https://redfish-controller-1.example.com"
assert cache.timeSeries[keyBackup].Tags["endpoint"] == "https://redfish-controller-2.example.com"
```

---

## 🎯 Key generation algorithm

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
    // System probes
    "cpu":         {"core"},
    "memory":      {},  // No discriminant tags (a host-wide system metric)
    "network":     {"interface", "adapter"},
    "logicaldisk": {"drive", "mount_point", "device"},

    // Application probes
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

Before implementing a key change, verify:

### 1. Uniqueness test
```
□ For each probe, identify EVERY multi-instance metric
□ For each metric, identify the tags that make it unique
□ Verify that no collision can occur
```

### 2. Stability test
```
□ If the endpoint changes, does the key stay the same? (YES required)
□ If the hostname changes, does the key stay the same? (YES required)
□ If the IP changes, does the key stay the same? (YES required)
```

### 3. Cardinality test
```
□ Series count as expected? (no explosion)
□ Series count × retention × frequency = acceptable memory?
```

### 4. Filtering test
```
□ Are contextual tags (endpoint, etc.) in metric.Tags? (YES required)
□ Can the web interface filter by endpoint? (YES required)
□ Does the /info/tags API return every tag? (YES required)
```

### 5. Migration test
```
□ Are the old keys compatible? (if migrating)
□ Is there a transition period? (if migrating)
□ Do the external dashboards keep working? (YES required)
```

---

## 🚨 Common failure cases

### Mistake 1: a forgotten discriminant tag

**Symptom:** metrics overwriting one another

**Example:**
```go
// ❌ WRONG: "controller" forgotten
tsKey := fmt.Sprintf("%s:%s:drive_id=%s", probe, metric, driveID)
// Result: Drive 0 of controller A overwrites Drive 0 of controller B

// ✅ RIGHT
tsKey := fmt.Sprintf("%s:%s:controller=%s:drive_id=%s", probe, metric, controller, driveID)
```

**Detection:**
```go
// Unit test
func TestNoCollision(t *testing.T) {
    cache := NewCache()

    cache.Add(DataPoint{Name: "metric", Tags: {controller: "A", drive: "0"}, Value: 10})
    cache.Add(DataPoint{Name: "metric", Tags: {controller: "B", drive: "0"}, Value: 20})

    // ❌ On collision, len == 1 (the 2nd value overwrites the 1st)
    // ✅ When correct, len == 2
    assert.Equal(t, 2, len(cache.timeSeries))
}
```

---

### Mistake 2: a contextual tag in the key

**Symptom:** history lost when the infrastructure changes

**Example:**
```go
// ❌ BAD: endpoint in the key
tsKey := fmt.Sprintf("%s:%s:endpoint=%s:drive_id=%s", probe, metric, endpoint, driveID)
// Result: an IP change = a new series = broken graphs

// ✅ GOOD: endpoint in metadata only
tsKey := fmt.Sprintf("%s:%s:drive_id=%s", probe, metric, driveID)
metadata := CachedMetric{..., Tags: {endpoint: endpoint, drive_id: driveID}}
```

**Detection:**
```go
// Stability test
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
    assert.Equal(t, 20, cache.timeSeries[initialKey].Value)  // Value updated
    assert.Equal(t, "https://192.168.1.200", cache.timeSeries[initialKey].Tags["endpoint"])
}
```

---

## 📊 Cardinality examples

### Worked example for a typical environment

**Scenario: 100 monitored servers**

```
Active probes:
- CPU (4 cores/server)
- Memory (1 host-wide metric)
- Network (2 interfaces/server)
- LogicalDisk (3 disks/server)
- Redfish (50 servers with 12 drives each)

Cardinality per probe:
- CPU:         100 servers × 4 cores × 2 metrics = 800 series
- Memory:      100 servers × 1 metric = 100 series
- Network:     100 servers × 2 interfaces × 4 metrics = 800 series
- LogicalDisk: 100 servers × 3 drives × 3 metrics = 900 series
- Redfish:     50 servers × 12 drives × 8 metrics = 4800 series

TOTAL: ~7400 time series

Estimated memory (5 min retention, 1 point/30 s):
- Points per series: 10 points
- Size per point: ~200 bytes (metadata + value)
- Memory: 7400 series × 10 points × 200 bytes ≈ 15 MB

✅ Acceptable
```

**Impact of the key change:**
```
BEFORE (endpoint in the key):
- If the endpoint changes → a new series → cardinality × 2
- 7400 → 14800 time series = 30 MB

AFTER (endpoint in metadata):
- Endpoint changes → same series → cardinality stable
- 7400 time series = 15 MB
- ✅ 50% less memory when the infrastructure changes
```

---

## 🎓 Conclusion

### The golden rule

> **A time-series key must identify a data source UNIQUELY and STABLY, independently of infrastructure changes.**

### SOLID principles for keys

1. **S**table: the key does not change when the infrastructure does
2. **U**nique: no collision between distinct series
3. **M**inimal: discriminant tags only
4. **M**etadata: contextual tags live in CachedMetric.Tags
5. **A**uditable: automated regression tests
6. **R**eproducible: same data → same key
7. **Y**ielding: cardinality kept under control

---

## 📚 References

- **VictoriaMetrics:** https://docs.victoriametrics.com/keyConcepts.html#time-series
- **Prometheus Best Practices:** https://prometheus.io/docs/practices/naming/
- **Time Series Database Concepts:** https://en.wikipedia.org/wiki/Time_series_database
- **Cardinality in Monitoring:** https://www.robustperception.io/cardinality-is-key

---

**Document written by:** Claude Code
**Reviewer required:** Matthieu (User)
**Approval:** ⏳ Pending
