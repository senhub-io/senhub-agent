# Discriminant Tags Registry - User Guide

**Date:** 2025-11-05
**Version:** 1.0
**Status:** ACTIVE

---

## Purpose

The **DiscriminantTagsRegistry** is a critical component of the SenHub Agent's cache system. It maps probe types to their **discriminant tags** - tags that identify UNIQUE metric instances and should be included in time series keys.

This ensures:
- ✅ Stable time series keys (metrics don't get recreated when metadata changes)
- ✅ Correct cardinality (one time series per unique metric source)
- ✅ Proper data continuity (infrastructure changes don't break historical data)

---

## Discriminant vs Contextual Tags

### Discriminant Tags
**Definition:** Tags that identify UNIQUE metric instances where values can be DIFFERENT.

**Examples:**
- `core`: Different CPU cores have independent usage values
- `interface`: Different network interfaces have independent traffic
- `drive_id`: Different storage drives have independent I/O metrics

**Behavior:** Included in time series key → Creates separate time series

### Contextual Tags
**Definition:** Tags that provide METADATA but don't identify distinct metric sources.

**Examples:**
- `endpoint`: URL to monitoring system (can change without changing metrics)
- `host`: Hostname or IP (can change for same physical machine)
- `platform`: OS information (metadata about the system)

**Behavior:** NOT included in time series key → Same time series continues

---

## Decision Rule

**Ask yourself:** "If this tag value changes, can the metric value be DIFFERENT?"

- ✅ **YES** → Discriminant tag (add to registry)
- ❌ **NO** → Contextual tag (metadata only)

### Decision Matrix

| Tag Example | Discriminant? | Reason |
|-------------|---------------|--------|
| `core=0` vs `core=1` | ✅ YES | Different CPU cores have different usage percentages |
| `endpoint=https://server1.com` vs `endpoint=https://server2.com` | ❌ NO | Same physical device, just different DNS/IP |
| `drive_id=disk.bay.0` vs `drive_id=disk.bay.1` | ✅ YES | Different physical drives have different I/O metrics |
| `platform=windows` vs `platform=linux` | ❌ NO | Metadata about the system, not the metric source |
| `interface=eth0` vs `interface=eth1` | ✅ YES | Different network interfaces have independent traffic |
| `host=server1` vs `host=server2` | ❌ NO | Infrastructure naming, not physical identity |

---

## Current Registry

The registry is defined in `internal/agent/services/data_store/strategies/http/http_cache.go`:

```go
var DiscriminantTagsRegistry = map[string][]string{
    // System probes - multi-instance metrics
    "cpu":         {"core"},                           // Different CPU cores
    "memory":      {},                                 // System-level only
    "network":     {"interface", "adapter"},           // Network interfaces
    "logicaldisk": {"drive", "mount_point", "device"}, // Storage drives

    // Application probes
    "citrix":  {"metric_type", "failure_category"},
    "webapp":  {"url", "endpoint"},
    "gateway": {"destination", "target"},

    // Infrastructure probes
    "redfish": {
        "controller", "controller_id",
        "drive_id", "drive_name",
        "volume_id", "volume_name",
        "pool_name", "pool_id",
        "psu_name", "psu_id",
        "processor_id",
        "memory_module_id",
        "fan_name", "sensor_name",
    },

    // Event probes
    "syslog": {},
    "event":  {},
}
```

---

## Adding New Probe Types

When creating a new probe type, follow these steps:

### Step 1: Analyze Your Metrics

Identify all tags that your probe generates. For each tag, ask:
- "Can metric values differ for different tag values?"
- "Does this tag identify a physical/logical component?"
- "Is this just metadata about how/where we collect?"

### Step 2: Classify Tags

**Discriminant (include in key):**
- Physical components (drives, CPUs, interfaces)
- Logical instances (containers, VMs, database instances)
- Resource identifiers (pool names, volume IDs, queue names)

**Contextual (metadata only):**
- Collection endpoints (URLs, hostnames, IPs)
- System metadata (OS, version, platform)
- Configuration details (thresholds, intervals)

### Step 3: Update Registry

Add your probe type to `DiscriminantTagsRegistry` in `http_cache.go`:

```go
"myprobe": {"instance_id", "component_name"},
```

### Step 4: Document Your Decision

Add a comment explaining WHY each tag is discriminant:

```go
"myprobe": {
    "instance_id",    // Different instances have independent metrics
    "component_name", // Different components within instance
},
```

### Step 5: Test Time Series Stability

Run tests to verify:
- ✅ Same instance → Same time series key (even if endpoint changes)
- ✅ Different instance → Different time series keys
- ✅ Metadata changes → Time series continues (no new key)

---

## Examples

### Example 1: CPU Probe

**Metrics:** `cpu.usage`, `cpu.system`, `cpu.user`

**Tags:**
- `core="0"` → Discriminant (different cores have different usage)
- `host="server1"` → Contextual (just metadata about the system)
- `platform="linux"` → Contextual (OS information)

**Registry Entry:**
```go
"cpu": {"core"},
```

**Result:**
- Time series: `cpu:cpu.usage:core=0` (includes core)
- Same CPU core with new hostname → Same time series continues ✅

### Example 2: Redfish Storage Probe

**Metrics:** `storage.drive.temperature`, `storage.drive.health`

**Tags:**
- `drive_id="disk.bay.0"` → Discriminant (different drives have different temps)
- `controller="RAID.Integrated.1-1"` → Discriminant (different controllers)
- `endpoint="https://idrac.example.com"` → Contextual (just access URL)

**Registry Entry:**
```go
"redfish": {"drive_id", "controller", ...},
```

**Result:**
- Time series: `storage-me5024:storage.drive.temperature:drive_id=disk.bay.0`
- Same drive with new iDRAC IP → Same time series continues ✅

### Example 3: Network Probe

**Metrics:** `network.bytes_sent`, `network.bytes_received`

**Tags:**
- `interface="eth0"` → Discriminant (different interfaces have different traffic)
- `adapter="Intel I350"` → Discriminant (hardware identification)
- `host="server1"` → Contextual (just hostname)

**Registry Entry:**
```go
"network": {"interface", "adapter"},
```

**Result:**
- Time series: `network:network.bytes_sent:interface=eth0`
- Same interface with new hostname → Same time series continues ✅

---

## Troubleshooting

### Symptom: Metrics overwriting each other

**Cause:** Missing discriminant tag in registry

**Debug:**
1. Check logs for "Replacing existing metric in time series"
2. Identify which tag varies between conflicting metrics
3. Add that tag to the discriminant tags list

**Example:**
```
ERROR: Replacing metric cpu:cpu.usage with different value (old: 45.2, new: 78.9)
Tags: core=0 vs core=1
```
**Fix:** Add `"core"` to cpu discriminant tags

### Symptom: New time series on config change

**Cause:** Contextual tag incorrectly in cache key

**Debug:**
1. Check cache keys contain endpoint/hostname
2. Verify tag doesn't identify physical component
3. Remove tag from discriminant tags list

**Example:**
```
New time series created:
Old key: cpu:cpu.usage:host=server1
New key: cpu:cpu.usage:host=server2
```
**Fix:** Remove `"host"` from cpu discriminant tags (it's contextual)

### Symptom: Metrics not transforming

**Cause:** Using `probe_name` instead of `probe_type` for registry lookup

**Debug:**
1. Check logs for "probe_type tag missing"
2. Verify probe embeds BaseProbe
3. Verify SetProbeType() is called during initialization

**Fix:** Ensure probe properly embeds BaseProbe:
```go
type MyProbe struct {
    *types.BaseProbe  // Required for probe_type tag
    // ... other fields
}
```

---

## Best Practices

### DO:
✅ Use physical/logical component identifiers as discriminant tags
✅ Test time series stability with infrastructure changes
✅ Document your reasoning in code comments
✅ Follow the decision matrix for each tag

### DON'T:
❌ Include collection endpoints in discriminant tags
❌ Use hostnames or IPs as discriminant tags
❌ Add tags "just to be safe" without analysis
❌ Forget to test with multiple probe instances

---

## See Also

- **TIME_SERIES_KEY_DESIGN.md** - Complete design rationale and engineering rules
- **CLAUDE.md** - Probe Architecture: name vs type distinction
- **http_cache.go** - Implementation of discriminant tags system

---

**Questions?** Check `TIME_SERIES_KEY_DESIGN.md` for detailed scenarios and test cases.
