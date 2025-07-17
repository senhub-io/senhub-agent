# Citrix Metrics - Complete Reference Guide

This document provides a comprehensive reference for all Citrix metrics collected by the SenHub Agent, including calculation methods, data sources, and official Citrix documentation references.

## Table of Contents

1. [Session Metrics](#session-metrics)
2. [Logon Performance Metrics](#logon-performance-metrics)
3. [Infrastructure Metrics](#infrastructure-metrics)
4. [Connection Failures](#connection-failures)
5. [Multi-Session Machine Faults](#multi-session-machine-faults)
6. [Analytics Metrics](#analytics-metrics)
7. [Data Sources & API Endpoints](#data-sources--api-endpoints)
8. [References](#references)

## Session Metrics

**Metric Type:** `sessions`

### Basic Session Counts

| Metric Name | Description | Calculation | Citrix Director Equivalent |
|-------------|-------------|-------------|---------------------------|
| `sessions_connected` | Active connected sessions | Count of sessions with `ConnectionState = 5` (Active) | "Sessions Connected" |
| `sessions_disconnected` | Disconnected sessions | Count of sessions with `ConnectionState = 2` (Disconnected) | "Sessions Disconnected" |

**Data Source:** `Sessions` OData endpoint with filtered queries  
**Update Frequency:** Every 2 minutes (probe interval)

### Session State Reference

Based on Citrix `ConnectionState` enumeration:

```
0 = Unknown
1 = Connected  
2 = Disconnected
3 = Terminated
4 = PreparingSession
5 = Active
6 = Reconnecting
8 = Other
9 = Pending
```

**Official Reference:** [Citrix Monitor API - Session States](https://docs.citrix.com/en-us/performance-analytics/insights.html)

## Logon Performance Metrics

**Metric Type:** `logon`

### 2-Minute Window Metrics (Complete Previous Minutes)

All logon breakdown metrics use a **complete 2-minute window** calculation:
- Example: At 14:07:30, analyzes connections from 14:05:00 to 14:07:00
- Uses `LogOnStartDate` to filter connections in this window

| Metric Name | Description | Unit | Precision |
|-------------|-------------|------|-----------|
| `logon_brokering` | Average brokering duration | Seconds | 2 decimals |
| `logon_vmstart` | Average VM start duration | Seconds | 2 decimals |
| `logon_hdx` | Average HDX connection duration | Seconds | 2 decimals |
| `logon_authentication` | Average authentication duration | Seconds | 2 decimals |
| `logon_gpo` | Average GPO processing duration | Seconds | 2 decimals |
| `logon_scripts` | Average logon scripts duration | Seconds | 2 decimals |
| `logon_profile` | Average profile load duration | Seconds | 2 decimals |
| `logon_interactive` | Average interactive session start | Seconds | 2 decimals |
| `logon_duration_total` | Average total logon duration | Seconds | 2 decimals |
| `logon_sessions_opened` | Number of sessions started | Count | Whole number |

### 1-Hour Average Metric

| Metric Name | Description | Unit | Precision |
|-------------|-------------|------|-----------|
| `logon_duration_avg_1h` | Average logon time (1 hour window) | Seconds | Whole number |

**Calculation Method:**
1. Get all `Connections` from last 1 hour
2. Calculate `LogOnEndDate - LogOnStartDate` for each connection
3. Average all durations and round to whole seconds

**Data Source:** `Connections` OData endpoint (more reliable than Sessions for timing data)

### Phase Duration Calculations

Each logon phase is calculated using specific date fields from the `Connection` entity:

```go
// Brokering: Provided directly
BrokeringDuration (milliseconds)

// VM Start: Calculated from timestamps  
VMStartEndDate - VMStartStartDate

// HDX Connection: Calculated from timestamps
HdxEndDate - HdxStartDate

// Authentication: Provided directly
AuthenticationDuration (milliseconds)

// GPO Processing: Calculated from timestamps
GpoEndDate - GpoStartDate

// Logon Scripts: Calculated from timestamps
LogOnScriptsEndDate - LogOnScriptsStartDate

// Profile Load: Calculated from timestamps
ProfileLoadEndDate - ProfileLoadStartDate

// Interactive Session: Calculated from timestamps  
InteractiveEndDate - InteractiveStartDate

// Total Logon: Calculated from timestamps
LogOnEndDate - LogOnStartDate
```

**Important Notes:**
- All calculated durations converted from milliseconds to seconds
- Only connections with valid (non-zero) timestamps are included
- Zero metrics returned when no connections in time window

## Infrastructure Metrics

**Metric Type:** `infrastructure`

### Machine State Metrics

| Metric Name | Description | Source Field |
|-------------|-------------|--------------|
| `machines_total` | Total machines in environment | Count of all `Machines` |
| `machines_registered` | Registered machines | `RegistrationState = 1` |
| `unregistered_vda_count` | Unregistered VDA agents | `RegistrationState = 0` or `2` (includes AgentError) |
| `machines_faulty` | Machines with faults | `FaultState != 1` (not None/Healthy) |
| `machines_maintenance` | Machines in maintenance mode | `InMaintenanceMode = true` |

**Data Source:** `Machines` OData endpoint  
**Update Frequency:** Every 2 minutes (probe interval)

### Machine Registration States

```
0 = Unregistered - VDA not communicating with controller
1 = Registered   - VDA successfully registered and available  
2 = AgentError   - VDA communication error
```

### Machine Fault States

Based on Citrix `MachineFaultStateCode` enumeration:

```
0 = Unknown           - Fault state unknown
1 = None              - Healthy machine (no faults)
2 = FailedToStart     - Last power-on operation failed
3 = StuckOnBoot       - Machine might not have booted properly
4 = Unregistered      - Machine unregistered from controller
5 = MaxCapacity       - Machine at maximum session capacity
6 = VMNotFound        - Virtual machine not found in hypervisor
```

## Connection Failures

**Metric Type:** `connection_failures`

### Connection Failure Metrics

| Metric Name | Description | Citrix Director Label |
|-------------|-------------|----------------------|
| `total` | Total connection failures (1 hour) | "Nbre total d'échecs de connexion" |
| `client_connection_failures` | Client-side connection issues | "Échecs de connexion client" |
| `configuration_errors` | Configuration/setup errors | "Erreurs de configuration" |
| `machine_failures` | Machine/VDA failures | "Machines défectueuses" |
| `capacity_unavailable` | Capacity/resource issues | "Capacité non disponible" |
| `licenses_unavailable` | Licensing issues | "Licences non disponibles" |
| `other_failures` | Other/Unknown issues | "Autres échecs" |

**Calculation Method:**
1. Get `ConnectionFailureLogs` from last 1 hour
2. Get `ConnectionFailureCategories` for dynamic mapping from environment
3. Use static local conversion (Category → Failure Type) based on environment analysis
4. Count failures per type

**Data Sources:** 
- `ConnectionFailureLogs` OData endpoint
- `ConnectionFailureCategories` OData endpoint

### Dynamic Category Mapping

The system uses a hybrid approach:
1. **Dynamic mapping** from `ConnectionFailureCategories` (adapts to each environment)
2. **Static conversion** to consistent failure types (no Internet required)

Example mapping (may vary by environment):
- Category 0 → Configuration issues
- Category 1 → Client/network issues
- Category 2 → Machine/VDA failures
- Category 3 → Capacity issues
- Category 4 → License issues
- Category 5 → Other/unknown issues

## Multi-Session Machine Faults

**Metric Type:** `multi_session_faults`

### Multi-Session Fault Metrics

| Metric Name | Description | Fault State | Citrix Director Label |
|-------------|-------------|-------------|----------------------|
| `machines_total` | Total faulty multi-session machines | Any fault state | "Total des machines avec OS multi-session défectueuses" |
| `boot_failure` | Boot failure machines | `FaultState = 2` | "Échec du démarrage" |
| `stuck_at_boot` | Machines stuck at boot | `FaultState = 3` | "Bloquées au démarrage" |
| `unregistered` | Unregistered machines | `FaultState = 4` or `RegistrationState != 1` | "Non enregistrées" |
| `max_capacity` | Max capacity reached | `FaultState = 5` | "Charge maximale" |
| `vm_not_found` | VM not found | `FaultState = 6` | "Machine virtuelle introuvable" |
| `unknown` | Unknown fault state | `FaultState = 0` | "Inconnue" |

**Data Source:** `Machines` OData endpoint  
**Filtering:** 
- Machines with valid `MachineName` and `DesktopGroupId` (excludes phantoms and infrastructure)
- Since `MachineRole` doesn't distinguish multi-session in all environments

## Analytics Metrics

**Metric Type:** `analytics`

### Black Hole Machine Detection

A **Black Hole Machine** is identified when **3 or more unique users** fail to connect to the same machine within a **24-hour period**.

### Analytics Metrics List

| Metric Name | Description | Unit | Calculation |
|-------------|-------------|------|-------------|
| `black_hole_machines_count` | Number of black hole machines detected | Count | Machines with 3+ unique user failures |
| `black_hole_machine_users_affected` | Unique users affected per machine | Count | Number of unique users per machine |
| `sessions_zombie` | Hidden/zombie sessions | Count | Sessions with `Hidden = true` |
| `health_score` | Overall environment health | Score | 100 - (failure_rate * 100) |

**Tags:** 
- `black_hole_machine_users_affected` includes a `machine` tag with the machine name
- All analytics metrics include `metric_type="analytics"` tag

### Official Citrix Definition

> "Black hole VDA failures are detected when **three or more unique users fail to connect** to the same multi-session VDA."

**Reference:** [Citrix Performance Analytics - Insights](https://docs.citrix.com/en-us/performance-analytics/insights.html)

### Time Window Behavior

- **Detection Frequency:** Every 2 minutes (probe interval)
- **Analysis Window:** Exactly 24 hours (rolling window)
- **Citrix Analytics Frequency:** Every 15 minutes (for comparison)

Our implementation provides **faster detection** (2 min vs 15 min) while maintaining the same 24-hour analysis window.

## Data Sources & API Endpoints

### OData Endpoints Used

| Endpoint | Purpose | Fields Used |
|----------|---------|-------------|
| `/Sessions` | Session state and counts | `ConnectionState`, `Hidden`, `LogOnDuration` |
| `/Connections` | Detailed logon timing | All logon phase timestamps, `SessionKey` |
| `/Machines` | Infrastructure state | `RegistrationState`, `FaultState`, `SessionSupport` |
| `/ConnectionFailureLogs` | Failure analysis | `ConnectionFailureEnumValue`, `MachineId`, `UserName` |
| `/ConnectionFailureCategories` | Failure categorization | `ConnectionFailureEnumValue`, `Category` |

### Query Examples

**Active Sessions:**
```
GET /Sessions?$filter=ConnectionState eq 5
```

**Recent Connections (2 minutes):**
```  
GET /Connections?$filter=LogOnStartDate ge 2024-06-27T14:05:00Z and LogOnStartDate lt 2024-06-27T14:07:00Z
```

**Connection Failures (1 hour):**
```
GET /ConnectionFailureLogs?$filter=FailureDate ge 2024-06-27T13:00:00Z
```

**Multi-Session Machines:**
```
GET /Machines?$filter=SessionSupport eq 'MultiSession'
```

### Zombie Session Detection

**Zombie sessions** are identified by the `Hidden` property in the Citrix API:

```go
// From Citrix Monitor API documentation
type Session struct {
    Hidden bool  // true = zombie session (not visible in Director)
    // other fields...
}
```

**Reference:** [Citrix Monitor API Deep Dive - Hidden Sessions](https://www.citrix.com/blogs/2022/09/07/diving-deep-into-the-monitor-api/)

### Health Score Calculation

The health score provides a 0-100 rating of environment health:

```
Health Score = 100 - (Failure Rate × 100)
Failure Rate = Failed Connections / Total Connection Attempts
```

- **100**: Perfect health (no failures)
- **90-99**: Good health (minimal failures)
- **70-89**: Warning state (noticeable failures)
- **0-69**: Critical state (high failure rate)

## Monitoring Integration

### PRTG Integration

All metrics are automatically exposed via PRTG-compatible JSON format:

```json
{
  "prtg": {
    "result": [
      {
        "channel": "Sessions Connected",
        "value": 257,
        "unit": "Count"
      },
      {
        "channel": "Logon Authentication", 
        "value": 0.04,
        "unit": "TimeSeconds",
        "float": 1
      },
      {
        "channel": "Connection Failures - Client",
        "value": 1,
        "unit": "Count"
      },
      {
        "channel": "Multi-Session Faulty - Unregistered",
        "value": 1,
        "unit": "Count"
      }
    ]
  }
}
```

### Grafana/Prometheus Integration

Metrics include structured tags for filtering and aggregation:

```
citrix_sessions_connected{metric_type="sessions"} 257
citrix_logon_authentication{metric_type="logon"} 0.04
citrix_client_connection_failures{metric_type="connection_failures"} 1
citrix_unregistered{metric_type="multi_session_faults"} 1
citrix_sessions_zombie{metric_type="analytics"} 5
citrix_black_hole_machines_count{metric_type="analytics"} 1
citrix_health_score{metric_type="analytics"} 95.5
```

## Configuration

### Probe Configuration

```yaml
probes:
  - name: citrix
    params:
      base_url: "https://your-director.domain.com/Citrix/Monitor/OData/v4/Data"
      environment: "PROD"
      interval: 120  # 2 minutes
      auth:
        method: "ntlm"  # or "basic"
        username: "DOMAIN\\serviceaccount"
        password: "password"
      tls:
        verify_ssl: false  # for development only
```

### No Additional Configuration Required

Black Hole Machine detection uses Citrix standard values:
- **24-hour window** (fixed)
- **3 unique users threshold** (fixed)

These values are based on official Citrix documentation and cannot be overridden.

## Performance Considerations

### API Call Optimization

The probe is optimized to minimize API calls:

1. **Single data fetch per cycle** - All endpoints called once per 2-minute interval
2. **Efficient filtering** - Use OData `$filter` for time-based queries
3. **Cached lookups** - Desktop group names and failure categories cached
4. **Selective processing** - Only multi-session machines for detailed fault states

### Metric Volume

Approximate metrics generated per collection cycle:
- **Session metrics:** 3-4 metrics
- **Logon metrics:** 10-12 metrics (when sessions active)
- **Infrastructure metrics:** 5 + 7 detailed fault state metrics
- **Failure metrics:** 1 + 6 category metrics + black hole metrics

**Total:** ~30-35 metrics per collection (varies based on activity)

## Troubleshooting

### Common Issues

**Zero Logon Metrics:**
- Check if connections exist in the time window
- Verify `LogOnStartDate` and `LogOnEndDate` are populated
- Enable debug logging: `--debug-modules probe.citrix`

**Missing Black Hole Machines:**
- Verify `ConnectionFailureLogs` contains failure data
- Check `MachineId` and `UserName` fields are populated
- Confirm failures span multiple unique users (3+ required)

**Incorrect Failure Categories:**
- Verify `ConnectionFailureCategories` endpoint is accessible
- Check mapping between `ConnectionFailureEnumValue` and `Category`
- Review dynamic category mapping in logs

**Multi-Session Machine Detection Issues:**
- Check `DesktopGroupId` field is populated (excludes infrastructure)
- Verify `MachineName` is not empty (excludes phantom machines)
- Check both `RegistrationState` (0, 2) and `FaultState` (!= 1)

### Debug Logging

Enable detailed logging for troubleshooting:

```bash
./agent run --verbose --debug-modules probe.citrix
```

This provides detailed information about:
- API calls and response data
- Metric calculations and filtering
- Time window calculations
- Category mappings
- Machine filtering logic

## Key Implementation Details

### Metric Naming Convention

1. **Simplified Names**: Removed redundant prefixes for cleaner display
   - ~~`connection_failures_count`~~ → `total`
   - ~~`multi_session_faulty_unregistered`~~ → `unregistered`

2. **Consistent Terminology**:
   - Use "total" instead of "count" for aggregate metrics
   - Use "VDA" (uppercase) for Virtual Delivery Agent references
   - Use snake_case for all metric names

3. **Metric Type Grouping**:
   - `sessions`: Session state metrics
   - `logon`: Logon performance metrics
   - `infrastructure`: Machine and registration metrics
   - `connection_failures`: Failure breakdown by category
   - `multi_session_faults`: Multi-session machine fault states
   - `analytics`: Black hole, zombie, health score

### API Response Field Mapping

The Citrix OData API returns different field names than documented:

| Expected Field | Actual API Field | Usage |
|----------------|------------------|-------|  
| `MachineName` | `Name` | Machine display name |
| `MachineId` | `Id` | Machine unique identifier |
| `SessionSupport` | Not available | Cannot distinguish multi/single-session |
| `DesktopGroupId` | `DesktopGroupId` | Identifies real vs phantom machines |

### Time Window Precision

- **2-minute metrics**: Use complete previous minutes (not sliding window)
- **1-hour metrics**: Use exact 60-minute lookback from current time
- **24-hour metrics**: Use exact 24-hour lookback for Black Hole detection

## References

### Official Citrix Documentation

1. **Citrix Performance Analytics - Insights**  
   https://docs.citrix.com/en-us/performance-analytics/insights.html
   - Black Hole Machine definition and detection criteria

2. **Citrix Monitor API Deep Dive**  
   https://www.citrix.com/blogs/2022/09/07/diving-deep-into-the-monitor-api/
   - Zombie sessions (`Hidden` property)
   - Session properties and states
   - API best practices

3. **Citrix OData API Reference**  
   https://developer-docs.citrix.com/projects/monitor-service-odata-api/en/latest/
   - Complete API endpoint documentation
   - Entity relationships and field definitions

4. **Citrix Virtual Apps and Desktops Monitoring**  
   https://docs.citrix.com/en-us/citrix-virtual-apps-desktops/monitor.html
   - Director interface explanations
   - Metric definitions and calculations

### Internal Implementation References

- **Session States:** `internal/agent/probes/citrix/client_interface.go`
- **Machine Fault States:** `internal/agent/probes/citrix/client_interface.go`
- **Failure Categories:** `internal/agent/probes/citrix/metrics_failures.go`
- **Logon Metrics:** `internal/agent/probes/citrix/metrics_logon.go`
- **Infrastructure Metrics:** `internal/agent/probes/citrix/metrics_infrastructure.go`
- **Analytics Metrics:** `internal/agent/probes/citrix/metrics_sessions.go`, `metrics_health.go`

---

*Document Version: 2.0*  
*Last Updated: December 2024*  
*This document reflects the current implementation with all metric simplifications and organizational improvements.*